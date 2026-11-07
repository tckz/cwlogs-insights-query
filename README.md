cwlogs-insights-query
===

Execute query on CloudWatch Logs Insights, then wait and output results as JSON lines.
Also manage saved query definitions from yaml.

# Usage

```
Usage: cwlogs-insights-query <command> [flags]

Commands:
  run        execute a query, then wait and output results as JSON lines
  put        create or update a saved query definition from yaml
  diff       show diff between yaml and the saved query definition
  describe   show the saved query definition as yaml
  list       list saved query definitions
  version    show version
```

## Query yaml

```yaml
query: |
  fields @timestamp, @message
  | sort @timestamp desc
  | limit 200
name: some/my-query
# optional
logGroups:
  - my-log-group
  - my-log-group2
```

* `name` is the name of the saved query definition. Required for `put` and `diff`.
* `logGroups` is optional. `--log-group` given to `run` overrides it.

## run

```
Usage: cwlogs-insights-query run [flags]

Flags:
  -duration duration
    	duration of query window (default 5m0s)
  -end value
    	inclusive end time to query, 2006-01-02T15:04:05Z07:00
  -limit int
    	limit of results, override limit command in query
  -log-group value
    	name of logGroup, override logGroups in query yaml
  -out string
    	path/to/result/file (default "/dev/stdout")
  -query string
    	path/to/query.yaml or path/to/query.txt (yaml when extension is .yaml/.yml)
  -start value
    	inclusive start time to query, 2006-01-02T15:04:05Z07:00
  -stat string
    	output last stat (default "/dev/stderr")
  -trace-id string
    	trace-id to query, reflect log groups, start/end time and request ids
```

`--query` accepts either a yaml file (`.yaml`/`.yml`) or a plain text file which contains the query string only.

```
# q.txt
fields @timestamp, @message
| sort @timestamp desc
| limit 200

$ path/to/cwlogs-insights-query run -log-group my-log-group -query q.txt \
    -start 2022-07-01T00:00:00+09:00 -duration 1h

$ path/to/cwlogs-insights-query run -query q.yaml -start 2022-07-01T00:00:00+09:00 -duration 1h
```

## put

Create or update the saved query definition named by `name` in the yaml.
When a definition with the same name exists, it is updated; otherwise a new one is created.
The query definition ID is written to stdout.

```
$ path/to/cwlogs-insights-query put -query q.yaml
```

## diff

Show unified diff between the saved query definition and the yaml.
When no definition with the same name exists, the whole yaml is shown as additions.

Exit status follows diff(1): 0 when identical, 1 when different, 2 when trouble.

By default the whole definition is shown as context. Use `-context N` to limit context lines around changes like `diff -U N`.

The diff is colorized when stdout is a terminal and `NO_COLOR` is not set. Use `-color always` or `-color never` to force it.

```
$ path/to/cwlogs-insights-query diff -query q.yaml
$ path/to/cwlogs-insights-query diff -query q.yaml -color always | less -R
```

## describe

Show the saved query definition specified by `-name` as yaml.
The output can be saved and used as the input of `put`, `diff` and `run`.
Only `name`, `query` and `logGroups` are supported; a warning is printed when the definition
has a query language other than CWLI or has parameters, since they are not preserved by `put`.

```
$ path/to/cwlogs-insights-query describe -name some/my-query
name: some/my-query
query: |-
  fields @timestamp, @message
  | sort @timestamp desc
  | limit 200
logGroups:
- my-log-group

$ path/to/cwlogs-insights-query describe -name some/my-query > q.yaml
```

## list

List names of saved query definitions, one per line, sorted by name.
`-prefix` limits the result to names starting with the prefix.
`-l` shows the query definition ID, last modified time (UTC) and name separated by tab.

```
$ path/to/cwlogs-insights-query list
$ path/to/cwlogs-insights-query list -prefix some/
$ path/to/cwlogs-insights-query list -l
```

# Installation

https://github.com/tckz/cwlogs-insights-query/releases or
```
go install github.com/tckz/cwlogs-insights-query/v2@latest
```

# Development

## Requirements

* go 1.27


# LICENSE

See LICENCE
