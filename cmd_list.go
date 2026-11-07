package main

import (
	"bufio"
	"cmp"
	"context"
	"fmt"
	"os"
	"slices"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs/types"
)

func cmdList(ctx context.Context, args []string) error {
	fs := newFlagSet("list")
	optPrefix := fs.String("prefix", "", "list only definitions whose name starts with the prefix")
	optLong := fs.Bool("l", false, "long format: id, last modified time and name separated by tab")
	fs.Parse(args)

	cfg, err := loadAWSConfig(ctx)
	if err != nil {
		return err
	}
	clLogs := cloudwatchlogs.NewFromConfig(cfg)

	defs, err := listQueryDefinitions(ctx, clLogs, *optPrefix)
	if err != nil {
		return err
	}
	slices.SortStableFunc(defs, func(a, b types.QueryDefinition) int {
		return cmp.Compare(aws.ToString(a.Name), aws.ToString(b.Name))
	})

	w := bufio.NewWriter(os.Stdout)
	for _, d := range defs {
		if *optLong {
			lastModified := ""
			if d.LastModified != nil {
				lastModified = time.UnixMilli(*d.LastModified).UTC().Format(time.RFC3339)
			}
			fmt.Fprintf(w, "%s\t%s\t%s\n", aws.ToString(d.QueryDefinitionId), lastModified, aws.ToString(d.Name))
		} else {
			fmt.Fprintln(w, aws.ToString(d.Name))
		}
	}
	if err := w.Flush(); err != nil {
		return fmt.Errorf("Flush: %w", err)
	}
	return nil
}
