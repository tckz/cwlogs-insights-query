cwlogs-insights-query
===

Execute query on CloudWatch Logs Insights, then wait and output results as JSON lines.

# Usage

```
Usage of cwlogs-insights-query:
  -duration duration
        duration of query window (default 5m0s)
  -end string
        end time to query, inclusive, 2006-01-02T15:04:05Z07:00
  -limit int
        limit of results, override limit command in query
  -log-group value
        name of logGroup
  -query string
        path/to/query.txt
  -start string
        start time to query, 2006-01-02T15:04:05Z07:00
  -stat string
        output last stat (default "/dev/stderr")
  -trace-id string
    	trace-id to query, reflect log groups, start/end time and request ids
  -version
        Show version
```

## Example

```
# q.txt
fields @timestamp, @message
| sort @timestamp desc
| limit 200

$ path/to/cwlogs-insights-query -log-group my-log-group -query q.txt \
    -start 2022-07-01T00:00:00+09:00 -duration 1h
```

# Installation

https://github.com/tckz/cwlogs-insights-query/releases or
```
go install github.com/tckz/cwlogs-insights-query@latest
```

# Development

## Requirements

* go 1.22


# LICENSE

See LICENCE
