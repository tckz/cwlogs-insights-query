package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs/types"
	"github.com/aws/aws-sdk-go-v2/service/xray"
	"github.com/cenkalti/backoff/v7"
	"github.com/samber/lo"
)

func cmdRun(ctx context.Context, args []string) error {
	fs := newFlagSet("run")
	var optLogGroups StringsFlag
	optQuery := fs.String("query", "", "path/to/query.yaml or path/to/query.txt (yaml when extension is .yaml/.yml)")
	optLimit := fs.Int("limit", 0, "limit of results, override limit command in query")
	optStart := NewTimeFlag()
	optEnd := NewTimeFlag()
	optDuration := fs.Duration("duration", 5*time.Minute, "duration of query window")
	optStat := fs.String("stat", "/dev/stderr", "output last stat")
	optOut := fs.String("out", "/dev/stdout", "path/to/result/file")
	optTraceID := fs.String("trace-id", "", "trace-id to query, reflect log groups, start/end time and request ids")
	// If the query contains a SOURCE command, the --log-group option cannot be specified. These options are mutually exclusive.
	fs.Var(&optLogGroups, "log-group", "name of logGroup, override logGroups in query yaml")
	fs.Var(optStart, "start", "inclusive start time to query, 2006-01-02T15:04:05Z07:00")
	fs.Var(optEnd, "end", "inclusive end time to query, 2006-01-02T15:04:05Z07:00")
	fs.Parse(args)

	if *optQuery == "" {
		return fmt.Errorf("--query must be specified")
	}

	def, err := LoadQueryFile(*optQuery)
	if err != nil {
		return err
	}

	q := def.Query
	logGroups := def.LogGroups
	if len(optLogGroups) > 0 {
		logGroups = optLogGroups
	}

	cfg, err := loadAWSConfig(ctx)
	if err != nil {
		return err
	}

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
		logGroups = append(logGroups, li.LogGroups...)

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
	}

	fp, err := os.Create(*optOut)
	if err != nil {
		return fmt.Errorf("os.Create out --out: %w", err)
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
		LogGroupNames: logGroups,
		QueryString:   aws.String(q),
	})
	if err != nil {
		return fmt.Errorf("StartQuery: %w", err)
	}

	return getResult(ctx, clLogs, out, fp, *optStat)
}

func getResult(ctx context.Context, cl *cloudwatchlogs.Client, stOut *cloudwatchlogs.StartQueryOutput, w io.Writer, statPath string) error {
	var queryDone bool
	defer func(ctx context.Context) {
		if !queryDone {
			ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
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
	}(ctx)

	f, err := os.Create(statPath)
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
	enc := json.NewEncoder(w)
	op := func() error {
		ctx := ctx
		out, err := cl.GetQueryResults(ctx, &cloudwatchlogs.GetQueryResultsInput{
			QueryId: stOut.QueryId,
		})
		if err != nil {
			return backoff.Permanent(fmt.Errorf("GetQueryResults: %w", err))
		}
		lastStat = out.Statistics

		b, err := json.Marshal(lastStat)
		if err != nil {
			return backoff.Permanent(fmt.Errorf("json.Marshal Statistics: %w", err))
		}
		log.Printf("status=%s, %s", out.Status, string(b))

		switch out.Status {
		case types.QueryStatusScheduled, types.QueryStatusRunning:
			return fmt.Errorf("query still running: %s", out.Status)
		case types.QueryStatusComplete:
			for _, r := range out.Results {
				m := map[string]any{}
				for _, e := range r {
					m[*e.Field] = *e.Value
				}
				if err := enc.Encode(m); err != nil {
					return backoff.Permanent(fmt.Errorf("json.Encode rec: %w", err))
				}
			}
			queryDone = true
			return nil
		case types.QueryStatusFailed:
			queryDone = true
			return backoff.Permanent(fmt.Errorf("status=%s", out.Status))
		default:
			return backoff.Permanent(fmt.Errorf("status=%s", out.Status))
		}
	}

	boffFunc := func() (any, error) { return nil, op() }

	_, err = backoff.Retry(ctx, boffFunc, backoff.WithBackOff(boff), backoff.WithMaxElapsedTime(0))
	return err
}
