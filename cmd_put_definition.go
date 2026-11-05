package main

import (
	"context"
	"fmt"
	"log"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs"
)

func cmdPutDefinition(ctx context.Context, args []string) error {
	fs := newFlagSet("put-definition")
	optQuery := fs.String("query", "", "path/to/query.yaml, which must have name and query")
	fs.Parse(args)

	if *optQuery == "" {
		return fmt.Errorf("--query must be specified")
	}

	def, err := LoadQueryDefinitionFile(*optQuery)
	if err != nil {
		return err
	}

	cfg, err := loadAWSConfig(ctx)
	if err != nil {
		return err
	}
	clLogs := cloudwatchlogs.NewFromConfig(cfg)

	existing, err := findQueryDefinition(ctx, clLogs, def.Name)
	if err != nil {
		return err
	}

	in := &cloudwatchlogs.PutQueryDefinitionInput{
		Name:          aws.String(def.Name),
		QueryString:   aws.String(def.Query),
		LogGroupNames: def.LogGroups,
	}
	action := "created"
	if existing != nil {
		in.QueryDefinitionId = existing.QueryDefinitionId
		action = "updated"
	}

	out, err := clLogs.PutQueryDefinition(ctx, in)
	if err != nil {
		return fmt.Errorf("PutQueryDefinition: %w", err)
	}

	log.Printf("%s query definition %q", action, def.Name)
	fmt.Println(aws.ToString(out.QueryDefinitionId))
	return nil
}
