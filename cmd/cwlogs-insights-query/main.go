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
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/cloudwatchlogs"
)

var version = "dev"

type stringsFlag []*string

func (f *stringsFlag) String() string {
	if f == nil {
		return "[]"
	}
	return fmt.Sprintf("%v", *f)
}

func (f *stringsFlag) Set(v string) error {
	*f = append(*f, &v)
	return nil
}

var (
	optLogGroups stringsFlag
	optVersion   = flag.Bool("version", false, "Show version")
	optQuery     = flag.String("query", "", "path/to/query.txt")
	optLimit     = flag.Int64("limit", 0, "limit of results, override limit command in query")
	optStart     = flag.String("start", "", "start time to query, 2006-01-02T15:04:05Z07:00")
	optEnd       = flag.String("end", "", "end time to query, inclusive, 2006-01-02T15:04:05Z07:00")
	optDuration  = flag.Duration("duration", 5*time.Minute, "duration of query window")
	optStat      = flag.String("stat", "/dev/stderr", "output last stat")
	optOut       = flag.String("out", "/dev/stdout", "path/to/result/file")
)

func main() {
	flag.Var(&optLogGroups, "log-group", "name of logGroup")
	flag.Parse()

	if *optVersion {
		fmt.Println(version)
		return
	}

	if err := run(); err != nil {
		log.Fatalf("*** %v", err)
	}
}

func run() error {

	if *optQuery == "" {
		return fmt.Errorf("--query must be specified")
	}

	if len(optLogGroups) == 0 {
		return fmt.Errorf("one or more --log-group must be specified")
	}

	if *optStart == "" {
		return fmt.Errorf("--start must be specified")
	}
	st, err := time.Parse(time.RFC3339, *optStart)
	if err != nil {
		return fmt.Errorf("--start must be a valid RFC3339 timestamp: %v", err)
	}

	fp, err := os.Create(*optOut)
	if err != nil {
		return fmt.Errorf("os.Create out --out: %v", err)
	}
	defer fp.Close()

	var et time.Time
	if *optEnd != "" {
		t, err := time.Parse(time.RFC3339, *optEnd)
		if err != nil {
			return fmt.Errorf("--end must be a valid RFC3339 timestamp: %v", err)
		}
		if t.Before(st) {
			return fmt.Errorf("--end must be after --start")
		}
		et = t
	} else {
		et = st.Add(*optDuration)
	}

	b, err := os.ReadFile(*optQuery)
	if err != nil {
		return fmt.Errorf("os.ReadFile: %v", err)
	}

	ctx := context.Background()
	ctx, cancel := signal.NotifyContext(ctx, os.Interrupt)
	defer cancel()

	sess := session.Must(session.NewSessionWithOptions(
		session.Options{SharedConfigState: session.SharedConfigEnable}))

	log.Printf("from %s to %s", st.Format(time.RFC3339), et.Format(time.RFC3339))
	cl := cloudwatchlogs.New(sess)
	var limit *int64
	if *optLimit > 0 {
		limit = optLimit
	}
	out, err := cl.StartQueryWithContext(ctx, &cloudwatchlogs.StartQueryInput{
		StartTime:     aws.Int64(st.Unix()),
		EndTime:       aws.Int64(et.Unix()),
		Limit:         limit,
		LogGroupNames: optLogGroups,
		QueryString:   aws.String(string(b)),
	})
	if err != nil {
		log.Fatalf("*** StartQueryWithContext: %v", err)
	}

	if err := getResult(ctx, cl, out, fp); err != nil {
		return err
	}

	return nil
}

func getResult(ctx context.Context, cl *cloudwatchlogs.CloudWatchLogs, stOut *cloudwatchlogs.StartQueryOutput, w io.Writer) error {
	var done bool
	defer func() {
		if !done {
			cl.StopQuery(&cloudwatchlogs.StopQueryInput{QueryId: stOut.QueryId})
		}
	}()

	f, err := os.Create(*optStat)
	if err != nil {
		return fmt.Errorf("os.Create stat: %w", err)
	}
	defer f.Close()

	var lastStat *cloudwatchlogs.QueryStatistics
	defer func() {
		if lastStat != nil {
			enc := json.NewEncoder(f)
			enc.Encode(lastStat)
		}
	}()

	enc := json.NewEncoder(w)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		out, err := cl.GetQueryResultsWithContext(ctx, &cloudwatchlogs.GetQueryResultsInput{
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
		log.Printf("status=%s, %s", *out.Status, string(b))

		switch *out.Status {
		case cloudwatchlogs.QueryStatusScheduled, cloudwatchlogs.QueryStatusRunning:
			time.Sleep(1 * time.Second)
			continue
		case cloudwatchlogs.QueryStatusComplete:
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
		case cloudwatchlogs.QueryStatusFailed:
			done = true
			return fmt.Errorf("status=%s", *out.Status)
		default:
			return fmt.Errorf("status=%s", *out.Status)
		}
	}
}
