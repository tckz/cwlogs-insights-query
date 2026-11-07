package main

import (
	"context"
	"fmt"
	"log"

	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs/types"
)

func cmdDescribe(ctx context.Context, args []string) error {
	fs := newFlagSet("describe")
	optName := fs.String("name", "", "name of the saved query definition")
	fs.Parse(args)

	if *optName == "" {
		return fmt.Errorf("--name must be specified")
	}

	cfg, err := loadAWSConfig(ctx)
	if err != nil {
		return err
	}
	clLogs := cloudwatchlogs.NewFromConfig(cfg)

	existing, err := findQueryDefinition(ctx, clLogs, *optName)
	if err != nil {
		return err
	}
	if existing == nil {
		return fmt.Errorf("query definition %q not found", *optName)
	}

	// The yaml representation covers name, query and logGroups only.
	if existing.QueryLanguage != "" && existing.QueryLanguage != types.QueryLanguageCwli {
		log.Printf("*** warning: query language %q is not preserved by put", existing.QueryLanguage)
	}
	if len(existing.Parameters) > 0 {
		log.Printf("*** warning: %d query parameters are not preserved by put", len(existing.Parameters))
	}

	y, err := fromRemoteDefinition(*existing).ToYAML()
	if err != nil {
		return err
	}
	fmt.Print(y)
	return nil
}
