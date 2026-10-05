package observability

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	o11y "github.com/launchdarkly/ldcli/internal/observability"
)

const (
	productTypeFlag = "product-type"
	groupByFlag     = "group-by"
	expressionFlag  = "expression"
	bucketCountFlag = "bucket-count"
	sortColumnFlag  = "sort-column"
	sortDirFlag     = "sort-direction"
	countFlag       = "count"
)

// metricAggregators mirrors the backend MetricAggregator enum.
var metricAggregators = []string{
	"Count", "CountDistinct", "CountDistinctKey", "Min", "Avg", "P50", "P90", "P95", "P99", "Max", "Sum",
}

func newLogsCmd(d deps) *cobra.Command {
	return newGroupCmd("logs", "Search logs", newOpCmd(d,
		"query",
		"Query logs",
		"Query a project's logs over a time range using the observability search syntax, for example\n`level=error AND service_name=api`.",
		func(cmd *cobra.Command) {
			addDateRangeFlags(cmd)
			cmd.Flags().String(queryFlag, "", "Search query, for example 'level=error message=\"timeout\"'")
			cmd.Flags().Int(limitFlag, 20, "Number of log lines to return (max 50)")
			addDirectionFlag(cmd)
		},
		func(cmd *cobra.Command, gql o11y.Client, projectID string) (interface{}, func() string, error) {
			dr, err := dateRange(cmd)
			if err != nil {
				return nil, nil, err
			}
			dir, err := direction(cmd)
			if err != nil {
				return nil, nil, err
			}
			query, _ := cmd.Flags().GetString(queryFlag)
			limit, _ := cmd.Flags().GetInt(limitFlag)

			data, err := gql.Do(projectID, o11y.LogsQuery, map[string]interface{}{
				"project_id": projectID,
				"params":     map[string]interface{}{"query": query, "date_range": dr},
				"direction":  dir,
				"limit":      clamp(limit, 50),
			})
			if err != nil {
				return nil, nil, err
			}

			var res struct {
				Logs struct {
					Edges []struct {
						Node struct {
							Timestamp   string `json:"timestamp"`
							Level       string `json:"level"`
							Message     string `json:"message"`
							ServiceName string `json:"serviceName"`
							TraceID     string `json:"traceID"`
						} `json:"node"`
					} `json:"edges"`
				} `json:"logs"`
			}
			if err := decode(data, &res); err != nil {
				return nil, nil, err
			}

			return data, func() string {
				if len(res.Logs.Edges) == 0 {
					return emptyHint("logs")
				}
				rows := make([][]string, 0, len(res.Logs.Edges))
				for _, e := range res.Logs.Edges {
					n := e.Node
					rows = append(rows, []string{n.Timestamp, n.Level, n.ServiceName, truncate(n.Message, 120)})
				}
				return table([]string{"TIMESTAMP", "LEVEL", "SERVICE", "MESSAGE"}, rows)
			}, nil
		},
	))
}

func newTracesCmd(d deps) *cobra.Command {
	return newGroupCmd("traces", "Search traces", newOpCmd(d,
		"query",
		"Query trace spans",
		"Query a project's trace spans over a time range using the observability search syntax, for example\n`service_name=api AND duration>1s`.",
		func(cmd *cobra.Command) {
			addDateRangeFlags(cmd)
			cmd.Flags().String(queryFlag, "", "Search query, for example 'service_name=api has_errors=true'")
			cmd.Flags().Int(limitFlag, 20, "Number of spans to return (max 50)")
			addDirectionFlag(cmd)
			cmd.Flags().String(sortColumnFlag, "", "Column to sort by instead of timestamp, for example 'duration'")
			cmd.Flags().String(sortDirFlag, "DESC", "Direction for --sort-column: ASC or DESC")
		},
		func(cmd *cobra.Command, gql o11y.Client, projectID string) (interface{}, func() string, error) {
			dr, err := dateRange(cmd)
			if err != nil {
				return nil, nil, err
			}
			dir, err := direction(cmd)
			if err != nil {
				return nil, nil, err
			}
			query, _ := cmd.Flags().GetString(queryFlag)
			limit, _ := cmd.Flags().GetInt(limitFlag)
			params := map[string]interface{}{"query": query, "date_range": dr}
			if col, _ := cmd.Flags().GetString(sortColumnFlag); col != "" {
				sortDir, _ := cmd.Flags().GetString(sortDirFlag)
				sortDir, err = oneOf(sortDirFlag, sortDir, []string{"ASC", "DESC"})
				if err != nil {
					return nil, nil, err
				}
				params["sort"] = map[string]interface{}{"column": col, "direction": sortDir}
			}

			data, err := gql.Do(projectID, o11y.TracesQuery, map[string]interface{}{
				"project_id": projectID,
				"params":     params,
				"direction":  dir,
				"limit":      clamp(limit, 50),
			})
			if err != nil {
				return nil, nil, err
			}

			var res struct {
				Traces struct {
					Edges []struct {
						Node struct {
							Timestamp   string  `json:"timestamp"`
							TraceID     string  `json:"traceID"`
							SpanName    string  `json:"spanName"`
							ServiceName string  `json:"serviceName"`
							Duration    float64 `json:"duration"`
							HasErrors   bool    `json:"hasErrors"`
							StatusCode  string  `json:"statusCode"`
						} `json:"node"`
					} `json:"edges"`
				} `json:"traces"`
			}
			if err := decode(data, &res); err != nil {
				return nil, nil, err
			}

			return data, func() string {
				if len(res.Traces.Edges) == 0 {
					return emptyHint("spans")
				}
				rows := make([][]string, 0, len(res.Traces.Edges))
				for _, e := range res.Traces.Edges {
					n := e.Node
					rows = append(rows, []string{
						n.Timestamp, n.ServiceName, truncate(n.SpanName, 60),
						formatNanos(n.Duration), n.StatusCode, n.TraceID,
					})
				}
				return table([]string{"TIMESTAMP", "SERVICE", "SPAN", "DURATION", "STATUS", "TRACE ID"}, rows)
			}, nil
		},
	))
}

// formatNanos renders a span duration, which the backend reports in nanoseconds.
func formatNanos(ns float64) string {
	switch {
	case ns >= 1e9:
		return fmt.Sprintf("%.2fs", ns/1e9)
	case ns >= 1e6:
		return fmt.Sprintf("%.2fms", ns/1e6)
	case ns >= 1e3:
		return fmt.Sprintf("%.2fµs", ns/1e3)
	default:
		return fmt.Sprintf("%.0fns", ns)
	}
}

func newErrorGroupsCmd(d deps) *cobra.Command {
	return newGroupCmd("error-groups", "Search error groups", newOpCmd(d,
		"query",
		"Query error groups",
		"Query a project's error groups (errors aggregated by stack trace) over a time range.",
		func(cmd *cobra.Command) {
			addDateRangeFlags(cmd)
			cmd.Flags().String(queryFlag, "", "Search query, for example 'service_name=api'")
			cmd.Flags().Int(countFlag, 10, "Number of error groups per page (max 50)")
			cmd.Flags().Int(pageFlag, 1, "Page number, starting at 1")
		},
		func(cmd *cobra.Command, gql o11y.Client, projectID string) (interface{}, func() string, error) {
			dr, err := dateRange(cmd)
			if err != nil {
				return nil, nil, err
			}
			query, _ := cmd.Flags().GetString(queryFlag)
			count, _ := cmd.Flags().GetInt(countFlag)
			page, _ := cmd.Flags().GetInt(pageFlag)

			data, err := gql.Do(projectID, o11y.ErrorGroupsQuery, map[string]interface{}{
				"project_id": projectID,
				"params":     map[string]interface{}{"query": query, "date_range": dr},
				"count":      clamp(count, 50),
				"page":       clamp(page, 1<<30),
			})
			if err != nil {
				return nil, nil, err
			}

			var res struct {
				ErrorGroups struct {
					ErrorGroups []struct {
						SecureID    string `json:"secure_id"`
						Event       string `json:"event"`
						State       string `json:"state"`
						Type        string `json:"type"`
						ServiceName string `json:"serviceName"`
						UpdatedAt   string `json:"updated_at"`
					} `json:"error_groups"`
					TotalCount int `json:"totalCount"`
				} `json:"error_groups"`
			}
			if err := decode(data, &res); err != nil {
				return nil, nil, err
			}

			return data, func() string {
				groups := res.ErrorGroups.ErrorGroups
				if len(groups) == 0 {
					return emptyHint("error groups")
				}
				rows := make([][]string, 0, len(groups))
				for _, g := range groups {
					rows = append(rows, []string{g.SecureID, g.State, g.Type, g.ServiceName, g.UpdatedAt, truncate(g.Event, 80)})
				}
				return table([]string{"SECURE ID", "STATE", "TYPE", "SERVICE", "UPDATED", "EVENT"}, rows) +
					fmt.Sprintf("\n\nShowing %d of %d error groups", len(groups), res.ErrorGroups.TotalCount)
			}, nil
		},
	))
}

// aggregationProductTypes maps user-facing product names to the backend ProductType enum.
var aggregationProductTypes = map[string]string{
	"errors":   "Errors",
	"traces":   "Traces",
	"requests": "Traces",
	"logs":     "Logs",
	"sessions": "Sessions",
	"metrics":  "Metrics",
	"events":   "Events",
}

func productTypeNames(m map[string]string) []string {
	names := make([]string, 0, len(m))
	for k := range m {
		names = append(names, k)
	}
	sort.Strings(names)
	return names
}

// parseExpressions parses repeated --expression values of the form AGGREGATOR or
// AGGREGATOR:COLUMN (for example Count, P90:duration).
func parseExpressions(raw []string) ([]map[string]interface{}, error) {
	if len(raw) == 0 {
		raw = []string{"Count"}
	}
	exprs := make([]map[string]interface{}, 0, len(raw))
	for _, r := range raw {
		agg, col, _ := strings.Cut(r, ":")
		canonical, err := oneOf(expressionFlag, agg, metricAggregators)
		if err != nil {
			return nil, err
		}
		exprs = append(exprs, map[string]interface{}{"aggregator": canonical, "column": col})
	}
	return exprs, nil
}

func addExpressionFlag(cmd *cobra.Command) {
	cmd.Flags().StringArray(expressionFlag, nil, fmt.Sprintf(
		"Aggregation as AGGREGATOR or AGGREGATOR:COLUMN, repeatable (default Count). Aggregators: %s",
		strings.Join(metricAggregators, ", "),
	))
}

func newAggregationsCmd(d deps) *cobra.Command {
	return newGroupCmd("aggregations", "Compute aggregated metrics over observability data", newOpCmd(d,
		"query",
		"Query aggregated metrics",
		"Compute time-bucketed aggregates (counts, percentiles, sums, ...) over errors, traces, logs, sessions,\nmetrics, or events, optionally grouped by a key. Examples:\n\n"+
			"  ldcli observability aggregations query --project default --product-type errors --group-by service_name\n"+
			"  ldcli observability aggregations query --project default --product-type traces --expression P90:duration",
		func(cmd *cobra.Command) {
			cmd.Flags().String(productTypeFlag, "", "Data to aggregate: "+strings.Join(productTypeNames(aggregationProductTypes), ", "))
			_ = cmd.MarkFlagRequired(productTypeFlag)
			_ = cmd.Flags().SetAnnotation(productTypeFlag, "required", []string{"true"})
			addDateRangeFlags(cmd)
			cmd.Flags().String(queryFlag, "", "Search query to filter the data")
			cmd.Flags().StringSlice(groupByFlag, nil, "Keys to group by, for example service_name")
			addExpressionFlag(cmd)
			cmd.Flags().Int(bucketCountFlag, 10, "Number of time buckets")
			cmd.Flags().Int(limitFlag, 5, "Maximum number of groups when using --group-by")
		},
		func(cmd *cobra.Command, gql o11y.Client, projectID string) (interface{}, func() string, error) {
			pt, _ := cmd.Flags().GetString(productTypeFlag)
			productType, ok := aggregationProductTypes[strings.ToLower(pt)]
			if !ok {
				return nil, nil, fmt.Errorf("--%s must be one of: %s", productTypeFlag, strings.Join(productTypeNames(aggregationProductTypes), ", "))
			}
			dr, err := dateRange(cmd)
			if err != nil {
				return nil, nil, err
			}
			rawExprs, _ := cmd.Flags().GetStringArray(expressionFlag)
			exprs, err := parseExpressions(rawExprs)
			if err != nil {
				return nil, nil, err
			}
			query, _ := cmd.Flags().GetString(queryFlag)
			groupBy, _ := cmd.Flags().GetStringSlice(groupByFlag)
			if groupBy == nil {
				groupBy = []string{}
			}
			bucketCount, _ := cmd.Flags().GetInt(bucketCountFlag)
			limit, _ := cmd.Flags().GetInt(limitFlag)

			data, err := gql.Do(projectID, o11y.MetricsQuery, map[string]interface{}{
				"product_type":     productType,
				"project_id":       projectID,
				"params":           map[string]interface{}{"query": query, "date_range": dr},
				"bucket_by":        "Timestamp",
				"bucket_count":     clamp(bucketCount, 1000),
				"group_by":         groupBy,
				"expressions":      exprs,
				"limit":            clamp(limit, 100),
				"limit_aggregator": exprs[0]["aggregator"],
			})
			if err != nil {
				return nil, nil, err
			}

			var res struct {
				Metrics struct {
					Buckets []struct {
						BucketMin   float64  `json:"bucket_min"`
						BucketMax   float64  `json:"bucket_max"`
						MetricValue *float64 `json:"metric_value"`
						MetricType  string   `json:"metric_type"`
						Column      string   `json:"column"`
						Group       []string `json:"group"`
					} `json:"buckets"`
				} `json:"metrics"`
			}
			if err := decode(data, &res); err != nil {
				return nil, nil, err
			}

			return data, func() string {
				if len(res.Metrics.Buckets) == 0 {
					return emptyHint("data")
				}
				rows := make([][]string, 0, len(res.Metrics.Buckets))
				for _, b := range res.Metrics.Buckets {
					value := ""
					if b.MetricValue != nil {
						value = str(*b.MetricValue)
					}
					expr := b.MetricType
					if b.Column != "" {
						expr += "(" + b.Column + ")"
					}
					rows = append(rows, []string{
						formatUnix(b.BucketMin), formatUnix(b.BucketMax), strings.Join(b.Group, ", "), expr, value,
					})
				}
				return table([]string{"BUCKET START", "BUCKET END", "GROUP", "EXPRESSION", "VALUE"}, rows)
			}, nil
		},
	))
}

// keysProductTypes maps user-facing product names to the key-discovery query to use. Metrics and
// events have no dedicated resolver and go through the generic keys query.
var keysProductTypes = map[string]struct {
	query         string
	field         string
	supportsCount bool
	productType   string
}{
	"logs":     {o11y.LogsKeysQuery, "logs_keys", true, ""},
	"traces":   {o11y.TracesKeysQuery, "traces_keys", true, ""},
	"sessions": {o11y.SessionsKeysQuery, "sessions_keys", true, ""},
	"errors":   {o11y.ErrorGroupsKeysQuery, "errors_keys", false, ""},
	"metrics":  {o11y.KeysQuery, "keys", true, "Metrics"},
	"events":   {o11y.KeysQuery, "keys", true, "Events"},
}

func newKeysCmd(d deps) *cobra.Command {
	names := make([]string, 0, len(keysProductTypes))
	for k := range keysProductTypes {
		names = append(names, k)
	}
	sort.Strings(names)

	return newGroupCmd("keys", "Discover filterable keys", newOpCmd(d,
		"list",
		"List searchable keys",
		"List the attribute keys that can be used in --query and --group-by for a product type.",
		func(cmd *cobra.Command) {
			cmd.Flags().String(productTypeFlag, "", "Product type: "+strings.Join(names, ", "))
			_ = cmd.MarkFlagRequired(productTypeFlag)
			_ = cmd.Flags().SetAnnotation(productTypeFlag, "required", []string{"true"})
			addDateRangeFlags(cmd)
			cmd.Flags().String(searchFlag, "", "Only return keys matching this substring")
			cmd.Flags().Int(limitFlag, 25, "Number of keys to return (max 100; ignored for errors)")
		},
		func(cmd *cobra.Command, gql o11y.Client, projectID string) (interface{}, func() string, error) {
			pt, _ := cmd.Flags().GetString(productTypeFlag)
			cfg, ok := keysProductTypes[strings.ToLower(strings.TrimSuffix(pt, "s"))+"s"]
			if !ok {
				return nil, nil, fmt.Errorf("--%s must be one of: %s", productTypeFlag, strings.Join(names, ", "))
			}
			dr, err := dateRange(cmd)
			if err != nil {
				return nil, nil, err
			}
			vars := map[string]interface{}{"project_id": projectID, "date_range": dr}
			if cfg.supportsCount {
				limit, _ := cmd.Flags().GetInt(limitFlag)
				vars["count"] = clamp(limit, 100)
			}
			if search, _ := cmd.Flags().GetString(searchFlag); search != "" {
				vars["query"] = search
			}
			if cfg.productType != "" {
				vars["product_type"] = cfg.productType
			}

			data, err := gql.Do(projectID, cfg.query, vars)
			if err != nil {
				return nil, nil, err
			}

			var res map[string][]struct {
				Name string `json:"name"`
				Type string `json:"type"`
			}
			if err := decode(data, &res); err != nil {
				return nil, nil, err
			}
			keys := res[cfg.field]

			return map[string]interface{}{"keys": keys}, func() string {
				if len(keys) == 0 {
					return "No keys found."
				}
				rows := make([][]string, 0, len(keys))
				for _, k := range keys {
					rows = append(rows, []string{k.Name, k.Type})
				}
				return table([]string{"KEY", "TYPE"}, rows)
			}, nil
		},
	))
}

func newServiceMapCmd(d deps) *cobra.Command {
	return newGroupCmd("service-map", "Inspect service dependencies", newOpCmd(d,
		"get",
		"Get the service dependency map",
		"Show which services call which, derived from trace data over a time range.",
		addDateRangeFlags,
		func(cmd *cobra.Command, gql o11y.Client, projectID string) (interface{}, func() string, error) {
			dr, err := dateRange(cmd)
			if err != nil {
				return nil, nil, err
			}
			data, err := gql.Do(projectID, o11y.ServiceMapQuery, map[string]interface{}{
				"project_id": projectID,
				"date_range": dr,
			})
			if err != nil {
				return nil, nil, err
			}

			var res struct {
				ServiceMap []struct {
					Source string  `json:"source"`
					Target *string `json:"target"`
				} `json:"serviceMap"`
			}
			if err := decode(data, &res); err != nil {
				return nil, nil, err
			}

			type dependency struct {
				Source string `json:"source"`
				Target string `json:"target"`
			}
			seen := map[string]bool{}
			services := []string{}
			dependencies := []dependency{}
			addService := func(s string) {
				if !seen[s] {
					seen[s] = true
					services = append(services, s)
				}
			}
			for _, e := range res.ServiceMap {
				if e.Source == "" {
					continue
				}
				addService(e.Source)
				if e.Target != nil && *e.Target != "" {
					addService(*e.Target)
					dependencies = append(dependencies, dependency{e.Source, *e.Target})
				}
			}
			sort.Strings(services)

			result := map[string]interface{}{"services": services, "dependencies": dependencies}
			return result, func() string {
				if len(services) == 0 {
					return "No services found. Check that trace data exists for this time range."
				}
				rows := make([][]string, 0, len(dependencies))
				for _, dep := range dependencies {
					rows = append(rows, []string{dep.Source, dep.Target})
				}
				out := "Services: " + strings.Join(services, ", ")
				if len(rows) > 0 {
					out += "\n\n" + table([]string{"SOURCE", "TARGET"}, rows)
				}
				return out
			}, nil
		},
	))
}

// formatUnix renders a timestamp bucket boundary (unix seconds) as RFC 3339.
func formatUnix(v float64) string {
	if v < 1e9 {
		return str(v)
	}
	return time.Unix(int64(v), 0).UTC().Format(time.RFC3339)
}
