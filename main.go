package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"runtime/debug"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs/types"
	"github.com/aws/aws-sdk-go-v2/service/xray"
	"github.com/cenkalti/backoff/v4"
	"github.com/samber/lo"
)

var version string

var (
	optLogGroups StringsFlag
	optVersion   = flag.Bool("version", false, "Show version")
	optQuery     = flag.String("query", "", "path/to/query.txt")
	optLimit     = flag.Int("limit", 0, "limit of results, override limit command in query")
	optStart     = NewTimeFlag()
	optEnd       = NewTimeFlag()
	optDuration  = flag.Duration("duration", 5*time.Minute, "duration of query window")
	optStat      = flag.String("stat", "/dev/stderr", "output last stat")
	optOut       = flag.String("out", "/dev/stdout", "path/to/result/file")
	optTraceID   = flag.String("trace-id", "", "trace-id to query, reflect log groups, start/end time and request ids")
)

func main() {
	flag.Var(&optLogGroups, "log-group", "name of logGroup")
	flag.Var(optStart, "start", "inclusive start time to query, 2006-01-02T15:04:05Z07:00")
	flag.Var(optEnd, "end", "inclusive end time to query, 2006-01-02T15:04:05Z07:00")

	flag.Parse()

	if *optVersion {
		if version != "" {
			fmt.Println(version)
		} else if info, ok := debug.ReadBuildInfo(); ok {
			fmt.Println(info.Main.Version)
		}
		return
	}

	if err := run(); err != nil {
		log.Fatalf("*** %v", err)
	}
}

func run() error {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(os.Getenv("AWS_REGION")))
	if err != nil {
		return fmt.Errorf("config.LoadDefaultConfig: %w", err)
	}

	if *optQuery == "" {
		return fmt.Errorf("--query must be specified")
	}

	b, err := os.ReadFile(*optQuery)
	if err != nil {
		return fmt.Errorf("os.ReadFile: %v", err)
	}

	q := string(b)

	clLogs := cloudwatchlogs.NewFromConfig(cfg)

	if *optTraceID != "" {
		clXray := xray.NewFromConfig(cfg)
		li, err := GatherLogInfo(ctx, clXray, *optTraceID, clLogs)
		if err != nil {
			return err
		}
		if li == nil {
			return fmt.Errorf("no trace found")
		}
		optLogGroups = append(optLogGroups, li.LogGroups...)

		q = q + "\n| filter "
		for i, e := range li.TraceIDs {
			if i > 0 {
				q = q + " or "
			}
			q = q + fmt.Sprintf("@message like %q", e)
		}
		for _, e := range li.RequestIDs {
			q = q + fmt.Sprintf(" or @requestId = %q or @message like %q", e, e)
		}

		lo.Must0(optStart.Set(li.StartTime.Format(time.RFC3339)))
		lo.Must0(optEnd.Set(li.EndTime.Format(time.RFC3339)))
	} else {
		if len(optLogGroups) == 0 {
			return fmt.Errorf("one or more --log-group must be specified")
		}
	}

	fp, err := os.Create(*optOut)
	if err != nil {
		return fmt.Errorf("os.Create out --out: %v", err)
	}
	defer fp.Close()

	st := optStart.Value()
	et := optEnd.Value()

	if st.IsZero() {
		st = time.Now().Add(-*optDuration)
	}
	if et.IsZero() {
		et = st.Add(*optDuration)
	}

	if et.Before(st) {
		return fmt.Errorf("--end must be after --start")
	}

	log.Printf("from %s to %s", st.Format(time.RFC3339), et.Format(time.RFC3339))
	var limit *int32
	if *optLimit > 0 {
		i := int32(*optLimit)
		limit = &i
	}
	out, err := clLogs.StartQuery(ctx, &cloudwatchlogs.StartQueryInput{
		StartTime:     aws.Int64(st.Unix()),
		EndTime:       aws.Int64(et.Unix()),
		Limit:         limit,
		LogGroupNames: optLogGroups,
		QueryString:   aws.String(q),
	})
	if err != nil {
		log.Fatalf("*** StartQueryWithContext: %v", err)
	}

	if err := getResult(ctx, clLogs, out, fp); err != nil {
		return err
	}

	return nil
}

func getResult(ctx context.Context, cl *cloudwatchlogs.Client, stOut *cloudwatchlogs.StartQueryOutput, w io.Writer) error {
	var done bool
	defer func() {
		if !done {
			ctx := context.Background()
			ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
			defer cancel()
			out, err := cl.StopQuery(ctx, &cloudwatchlogs.StopQueryInput{QueryId: stOut.QueryId})
			if err != nil {
				log.Printf("*** StopQuery: %v", err)
			} else {
				b, err := json.Marshal(out)
				if err == nil {
					log.Printf("stopped: %s", string(b))
				}
			}
		}
	}()

	f, err := os.Create(*optStat)
	if err != nil {
		return fmt.Errorf("os.Create stat: %w", err)
	}
	defer f.Close()

	var lastStat *types.QueryStatistics
	defer func() {
		if lastStat != nil {
			enc := json.NewEncoder(f)
			enc.Encode(lastStat)
		}
	}()

	boff := backoff.NewExponentialBackOff()
	boff.MaxInterval = time.Second
	boff.MaxElapsedTime = 0
	boff.Reset()
	enc := json.NewEncoder(w)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		out, err := cl.GetQueryResults(ctx, &cloudwatchlogs.GetQueryResultsInput{
			QueryId: stOut.QueryId,
		})
		if err != nil {
			return fmt.Errorf("GetQueryResultsWithContext: %w", err)
		}
		lastStat = out.Statistics

		b, err := json.Marshal(lastStat)
		if err != nil {
			return fmt.Errorf("json.Marshal Statistics: %v", err)
		}
		log.Printf("status=%s, %s", out.Status, string(b))

		switch out.Status {
		case types.QueryStatusScheduled, types.QueryStatusRunning:
			d := boff.NextBackOff()
			if d == backoff.Stop {
				return fmt.Errorf("reached backoff.Stop")
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(d):
			}
			continue
		case types.QueryStatusComplete:
			for _, r := range out.Results {
				m := map[string]interface{}{}
				for _, e := range r {
					m[*e.Field] = *e.Value
				}
				if err := enc.Encode(m); err != nil {
					return fmt.Errorf("json.Encode rec: %w", err)
				}
			}
			done = true
			return nil
		case types.QueryStatusFailed:
			done = true
			return fmt.Errorf("status=%s", out.Status)
		default:
			return fmt.Errorf("status=%s", out.Status)
		}
	}
}
