package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs/types"
	"github.com/goccy/go-yaml"
	"github.com/samber/lo"
)

// QueryDefinition is the yaml representation of a query.
//
//	query: |
//	  fields @timestamp, @message
//	  | sort @timestamp desc
//	  | limit 200
//	name: some/my-query
//	logGroups:
//	  - my-log-group
type QueryDefinition struct {
	Name      string   `yaml:"name,omitempty"`
	Query     string   `yaml:"query"`
	LogGroups []string `yaml:"logGroups,omitempty"`
}

func isYAMLPath(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".yaml", ".yml":
		return true
	}
	return false
}

func parseQueryDefinition(b []byte) (*QueryDefinition, error) {
	var def QueryDefinition
	if err := yaml.UnmarshalWithOptions(b, &def, yaml.Strict()); err != nil {
		return nil, fmt.Errorf("yaml.Unmarshal: %w", err)
	}
	if def.Query == "" {
		return nil, fmt.Errorf("query must be specified")
	}
	return &def, nil
}

// LoadQueryFile loads a query from path. The file is parsed as yaml when its extension is .yaml/.yml,
// otherwise the whole content is treated as the query string.
func LoadQueryFile(path string) (*QueryDefinition, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("os.ReadFile: %w", err)
	}

	if isYAMLPath(path) {
		return parseQueryDefinition(b)
	}
	return &QueryDefinition{Query: string(b)}, nil
}

// LoadQueryDefinitionFile loads a yaml query definition which must have name and query.
func LoadQueryDefinitionFile(path string) (*QueryDefinition, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("os.ReadFile: %w", err)
	}
	def, err := parseQueryDefinition(b)
	if err != nil {
		return nil, err
	}
	if def.Name == "" {
		return nil, fmt.Errorf("name must be specified")
	}
	return def, nil
}

func (d *QueryDefinition) ToYAML() (string, error) {
	b, err := yaml.MarshalWithOptions(d, yaml.UseLiteralStyleIfMultiline(true))
	if err != nil {
		return "", fmt.Errorf("yaml.Marshal: %w", err)
	}
	return string(b), nil
}

func fromRemoteDefinition(d types.QueryDefinition) *QueryDefinition {
	return &QueryDefinition{
		Name:      aws.ToString(d.Name),
		Query:     aws.ToString(d.QueryString),
		LogGroups: d.LogGroupNames,
	}
}

// listQueryDefinitions returns all saved query definitions whose name starts with prefix.
// An empty prefix returns every definition.
func listQueryDefinitions(ctx context.Context, cl *cloudwatchlogs.Client, prefix string) ([]types.QueryDefinition, error) {
	var defs []types.QueryDefinition
	in := &cloudwatchlogs.DescribeQueryDefinitionsInput{}
	if prefix != "" {
		in.QueryDefinitionNamePrefix = aws.String(prefix)
	}
	for {
		out, err := cl.DescribeQueryDefinitions(ctx, in)
		if err != nil {
			return nil, fmt.Errorf("DescribeQueryDefinitions: %w", err)
		}
		defs = append(defs, out.QueryDefinitions...)
		if out.NextToken == nil || *out.NextToken == "" {
			return defs, nil
		}
		in.NextToken = out.NextToken
	}
}

// findQueryDefinition looks up a saved query definition whose name exactly matches.
// It returns nil when not found, and an error when the name is ambiguous.
func findQueryDefinition(ctx context.Context, cl *cloudwatchlogs.Client, name string) (*types.QueryDefinition, error) {
	defs, err := listQueryDefinitions(ctx, cl, name)
	if err != nil {
		return nil, err
	}
	found := lo.Filter(defs, func(d types.QueryDefinition, _ int) bool { return aws.ToString(d.Name) == name })

	switch len(found) {
	case 0:
		return nil, nil
	case 1:
		return &found[0], nil
	default:
		ids := lo.Map(found, func(d types.QueryDefinition, _ int) string { return aws.ToString(d.QueryDefinitionId) })
		return nil, fmt.Errorf("query definition %q is ambiguous: %d definitions found: %s", name, len(found), strings.Join(ids, ", "))
	}
}
