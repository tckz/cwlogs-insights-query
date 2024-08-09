package main

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs/types"
	"github.com/aws/aws-sdk-go-v2/service/xray"
	"github.com/deckarep/golang-set/v2"
	"github.com/spyzhov/ajson"
)

type LogInfo struct {
	TraceIDs   []string
	RequestIDs []string
	LogGroups  []string
	StartTime  time.Time
	EndTime    time.Time
}

type resetAPIInfo struct {
	ID    string
	Stage string
}

type traverseTraceInfo struct {
	startTime float64
	endTime   float64
	restAPIs  mapset.Set[resetAPIInfo]
	reqIDs    mapset.Set[string]
	traceIDs  mapset.Set[string]
	logGroups mapset.Set[string]
}

func GatherLogInfo(ctx context.Context, cl *xray.Client, tid string, clLogs *cloudwatchlogs.Client) (*LogInfo, error) {
	ti := traverseTraceInfo{
		restAPIs:  mapset.NewSet[resetAPIInfo](),
		reqIDs:    mapset.NewSet[string](),
		logGroups: mapset.NewSet[string](),
		traceIDs:  mapset.NewSet[string](),
		startTime: math.MaxFloat64,
		endTime:   0,
	}

	if err := traverseTrace(ctx, cl, tid, clLogs, &ti); err != nil {
		return nil, err
	}

	if ti.traceIDs.Cardinality() == 0 {
		return nil, nil
	}

	for _, e := range ti.restAPIs.ToSlice() {
		lg := fmt.Sprintf("API-Gateway-Execution-Logs_%s/%s", e.ID, e.Stage)
		_, err := clLogs.DescribeSubscriptionFilters(ctx, &cloudwatchlogs.DescribeSubscriptionFiltersInput{
			LogGroupName:     aws.String(lg),
			FilterNamePrefix: nil,
			Limit:            aws.Int32(1),
			NextToken:        nil,
		})
		if err != nil {
			var notFound *types.ResourceNotFoundException
			if !errors.As(err, &notFound) {
				return nil, fmt.Errorf("DescribeSubscriptionFilters: %w", err)
			}
		} else {
			ti.logGroups.Add(lg)
		}
	}

	return &LogInfo{
		TraceIDs:   ti.traceIDs.ToSlice(),
		RequestIDs: ti.reqIDs.ToSlice(),
		LogGroups:  ti.logGroups.ToSlice(),
		StartTime:  time.Unix(int64(ti.startTime)-1, 0),
		EndTime:    time.Unix(int64(ti.endTime)+1, 0),
	}, nil

}

func atMostOneValue[T any](node *ajson.Node, jp string, funcName string, f func(*ajson.Node) (T, error)) (found *ajson.Node, ret T, err error) {
	nodes, err := node.JSONPath(jp)
	if err != nil {
		return nil, ret, fmt.Errorf("JSONPath.%s: %w", jp, err)
	}
	if len(nodes) >= 1 {
		found := nodes[0]
		v, err := f(found)
		if err != nil {
			return nil, ret, fmt.Errorf("%s.%s: %w", funcName, jp, err)
		}
		return found, v, nil
	}
	return nil, ret, nil
}

func atMostOneString(node *ajson.Node, jp string) (*ajson.Node, string, error) {
	return atMostOneValue(node, jp, "GetString", func(n *ajson.Node) (string, error) { return n.GetString() })
}

func atMostOneNumeric(node *ajson.Node, jp string) (*ajson.Node, float64, error) {
	return atMostOneValue(node, jp, "GetNumeric", func(n *ajson.Node) (float64, error) { return n.GetNumeric() })
}

func multiString(node *ajson.Node, jp string) ([]*ajson.Node, []string, error) {
	nodes, err := node.JSONPath(jp)
	if err != nil {
		return nil, nil, fmt.Errorf("JSONPath.%s: %w", jp, err)
	}

	ret := make([]string, 0, len(nodes))
	for _, n := range nodes {
		v, err := n.GetString()
		if err != nil {
			return nil, nil, fmt.Errorf("GetString.%s: %w", jp, err)
		}
		ret = append(ret, v)
	}
	return nodes, ret, nil
}

func traverseTrace(ctx context.Context, cl *xray.Client, tid string, clLogs *cloudwatchlogs.Client, ti *traverseTraceInfo) error {
	if ti.traceIDs.Contains(tid) {
		return nil
	}

	out, err := cl.BatchGetTraces(ctx, &xray.BatchGetTracesInput{
		TraceIds: []string{tid},
	})
	if err != nil {
		return fmt.Errorf("BatchGetTraces: %w", err)
	}

	ti.traceIDs.Add(tid)

	if len(out.Traces) == 0 {
		return nil
	}

	for _, t := range out.Traces {
		for _, s := range t.Segments {
			if doc := s.Document; doc != nil {
				root, err := ajson.Unmarshal([]byte(*doc))
				if err != nil {
					return fmt.Errorf("ajson.Unmarshal: %w", err)
				}

				found, v, err := atMostOneString(root, "$.aws.request_id")
				if err != nil {
					return err
				}
				if found != nil {
					ti.reqIDs.Add(v)
				}

				found, n, err := atMostOneNumeric(root, "$.start_time")
				if err != nil {
					return err
				}
				if found != nil {
					ti.startTime = math.Min(n, ti.startTime)
				}

				found, n, err = atMostOneNumeric(root, "$.end_time")
				if err != nil {
					return err
				}
				if found != nil {
					ti.endTime = math.Max(n, ti.endTime)
				}

				found, v, err = atMostOneString(root, "$.aws.api_gateway.rest_api_id")
				if err != nil {
					return err
				}
				if found != nil {
					apiID := v
					found, v, err := atMostOneString(found.Parent(), "@.stage")
					if err != nil {
						return err
					}
					if found != nil {
						ti.restAPIs.Add(resetAPIInfo{
							ID:    apiID,
							Stage: v,
						})
					}
				}

				_, vs, err := multiString(root, "$.aws.cloudwatch_logs..log_group")
				if err != nil {
					return err
				}
				ti.logGroups.Append(vs...)

				_, vs, err = multiString(root, "$.links..trace_id")
				if err != nil {
					return err
				}
				for _, tid := range vs {
					if err := traverseTrace(ctx, cl, tid, clLogs, ti); err != nil {
						return err
					}
				}
			}
		}
	}

	return nil
}
