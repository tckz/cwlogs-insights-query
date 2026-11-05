package main

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs"
	"github.com/aymanbagabas/go-udiff"
	"github.com/fatih/color"
)

// cmdDiffDefinition follows diff(1) exit status convention:
// 0 when identical, 1 when different, 2 when trouble.
func cmdDiffDefinition(ctx context.Context, args []string) error {
	fs := newFlagSet("diff-definition")
	optQuery := fs.String("query", "", "path/to/query.yaml, which must have name and query")
	optColor := colorAuto
	fs.Var(&optColor, "color", "colorize diff output: auto, always or never. auto enables color when stdout is a terminal and NO_COLOR is not set")
	optContext := fs.Int("context", -1, "number of context lines around changes, negative shows the whole definition")
	fs.Parse(args)

	if *optQuery == "" {
		return &exitError{code: 2, err: fmt.Errorf("--query must be specified")}
	}
	optColor.apply()

	differ, err := diffDefinition(ctx, *optQuery, *optContext)
	if err != nil {
		return &exitError{code: 2, err: err}
	}
	if differ {
		return &exitError{code: 1}
	}
	return nil
}

func diffDefinition(ctx context.Context, path string, contextLines int) (differ bool, err error) {
	def, err := LoadQueryDefinitionFile(path)
	if err != nil {
		return false, err
	}

	cfg, err := loadAWSConfig(ctx)
	if err != nil {
		return false, err
	}
	clLogs := cloudwatchlogs.NewFromConfig(cfg)

	existing, err := findQueryDefinition(ctx, clLogs, def.Name)
	if err != nil {
		return false, err
	}
	// When the definition does not exist yet, the whole yaml is shown as additions.
	remote := ""
	if existing != nil {
		remote, err = fromRemoteDefinition(*existing).ToYAML()
		if err != nil {
			return false, err
		}
	} else {
		log.Printf("query definition %q not found, treated as empty", def.Name)
	}
	local, err := def.ToYAML()
	if err != nil {
		return false, err
	}

	if contextLines < 0 {
		// Enough context to cover the whole definition in a single hunk.
		contextLines = max(strings.Count(remote, "\n"), strings.Count(local, "\n")) + 1
	}
	u, err := udiff.ToUnifiedDiff("remote:"+def.Name, path, remote, udiff.Lines(remote, local), contextLines)
	if err != nil {
		return false, fmt.Errorf("udiff.ToUnifiedDiff: %w", err)
	}
	if len(u.Hunks) == 0 {
		log.Printf("no difference: %q", def.Name)
		return false, nil
	}
	if err := writeUnified(color.Output, u); err != nil {
		return false, fmt.Errorf("write diff: %w", err)
	}
	return true, nil
}
