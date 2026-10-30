package observability

import (
	"encoding/json"
	"fmt"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/launchdarkly/ldcli/cmd/validators"
	"github.com/launchdarkly/ldcli/internal/observability"
)

const (
	bucketCountFlag = "bucket-count"
	columnFlag      = "column"
	functionFlag    = "function"
	groupByFlag     = "group-by"
	groupLimitFlag  = "group-limit"
	productTypeFlag = "product-type"

	maxBucketCount = 300
	maxGroupLimit  = 50
)

// functions are the aggregate functions of the API. Only Count works without a column.
var functions = []string{"Count", "CountDistinct", "CountDistinctKey", "Min", "Avg", "P50", "P75", "P90", "P95", "P99", "Max", "Sum"}

type metricsResult struct {
	Buckets []struct {
		BucketID    float64  `json:"bucket_id"`
		BucketMin   float64  `json:"bucket_min"`
		BucketMax   float64  `json:"bucket_max"`
		MetricValue *float64 `json:"metric_value"`
		MetricType  string   `json:"metric_type"`
		Column      string   `json:"column"`
		Group       []string `json:"group"`
	} `json:"buckets"`
	BucketCount  int     `json:"bucket_count"`
	SampleFactor float64 `json:"sample_factor"`
}

func newAggregateCmd(clients Clients) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "aggregate",
		Short: "Count or aggregate records in time buckets",
		Long: `Count or aggregate logs, traces, errors, sessions, metrics, or events in time buckets.

Use this command for counts, trends, and group comparisons, and for time ranges longer than 24 hours.

If the API read only a sample of the data, the output shows the sample factor. The values are then estimates. A sample factor of 90 can mean that one sampled record became a count of 90. For an exact count of rare events, use a shorter time range.

To get one total for each group over the full time range, use --bucket-count 1.`,
		Example: `  ldcli observability aggregate --project default --product-type logs --query 'level=error' --group-by service_name --bucket-count 1
  ldcli observability aggregate --project default --product-type traces --function P99 --column duration --group-by span_name --start 6h
  ldcli observability aggregate --project default --product-type logs --query 'request_panic=true' --group-by error.message --start 7d --bucket-count 7`,
		Args: validators.Validate(),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAggregate(cmd, clients)
		},
	}

	addTimeRangeFlags(cmd)
	cmd.Flags().String(productTypeFlag, "", "Data to aggregate: "+productTypeNames)
	_ = cmd.MarkFlagRequired(productTypeFlag)
	_ = cmd.Flags().SetAnnotation(productTypeFlag, "required", []string{"true"})
	cmd.Flags().String(queryFlag, "", "Search query that selects the records. Empty selects all records")
	cmd.Flags().String(functionFlag, "Count", "Aggregate function: "+strings.Join(functions, ", "))
	cmd.Flags().String(columnFlag, "", "Column for the function, for example duration for traces or value for metrics. Required for each function except Count")
	cmd.Flags().StringSlice(groupByFlag, nil, "Comma-separated keys to group by, for example service_name,level")
	cmd.Flags().Int(groupLimitFlag, 5, fmt.Sprintf("Maximum number of groups, at most %d", maxGroupLimit))
	cmd.Flags().Int(bucketCountFlag, 10, fmt.Sprintf("Number of time buckets, at most %d. Use 1 for one total for each group", maxBucketCount))

	return cmd
}

func runAggregate(cmd *cobra.Command, clients Clients) error {
	productTypeValue, _ := cmd.Flags().GetString(productTypeFlag)
	productType, ok := productTypes[strings.ToLower(productTypeValue)]
	if !ok {
		return fmt.Errorf("--%s must be one of: %s", productTypeFlag, productTypeNames)
	}
	functionValue, _ := cmd.Flags().GetString(functionFlag)
	function := ""
	for _, f := range functions {
		if strings.EqualFold(f, functionValue) {
			function = f
		}
	}
	if function == "" {
		return fmt.Errorf("--%s must be one of: %s", functionFlag, strings.Join(functions, ", "))
	}
	column, _ := cmd.Flags().GetString(columnFlag)
	column = strings.TrimSpace(column)
	if function != "Count" && column == "" {
		return fmt.Errorf("--%s %s needs --%s. Only Count works without a column", functionFlag, function, columnFlag)
	}
	if function == "Count" && column != "" {
		return fmt.Errorf("--%s is not used by Count. Set --%s, for example Avg or CountDistinct", columnFlag, functionFlag)
	}
	bucketCount, _ := cmd.Flags().GetInt(bucketCountFlag)
	if bucketCount < 1 || bucketCount > maxBucketCount {
		return fmt.Errorf("--%s must be from 1 to %d", bucketCountFlag, maxBucketCount)
	}
	groupLimit, _ := cmd.Flags().GetInt(groupLimitFlag)
	if groupLimit < 1 || groupLimit > maxGroupLimit {
		return fmt.Errorf("--%s must be from 1 to %d", groupLimitFlag, maxGroupLimit)
	}
	groupBy, _ := cmd.Flags().GetStringSlice(groupByFlag)
	groups := make([]string, 0, len(groupBy))
	for _, g := range groupBy {
		if g = strings.TrimSpace(g); g != "" {
			groups = append(groups, g)
		}
	}
	dr, err := dateRange(cmd, time.Now())
	if err != nil {
		return err
	}

	t, err := resolveTarget(cmd, clients.Resources)
	if err != nil {
		return err
	}

	query, _ := cmd.Flags().GetString(queryFlag)
	variables := map[string]any{
		"product_type": productType,
		"project_id":   t.projectID,
		"params":       map[string]any{"query": query, "date_range": dr},
		"bucket_by":    "Timestamp",
		"bucket_count": bucketCount,
		"group_by":     groups,
		"expressions":  []any{map[string]any{"aggregator": function, "column": column}},
		"limit":        groupLimit,
	}
	// The API ranks groups by limit_aggregator. Rank by the function, so that the top
	// groups for P99 are the slowest. Count and CountDistinctKey rank by record count.
	if function == "Count" || function == "CountDistinctKey" {
		variables["limit_aggregator"] = "Count"
	} else {
		variables["limit_aggregator"] = function
		variables["limit_column"] = column
	}

	data, err := clients.Observability.Query(cmd.Context(), observability.Request{
		AccessToken: t.accessToken,
		Endpoint:    t.endpoint,
		ProjectID:   t.projectID,
		Query:       aggregationsQuery,
		Variables:   variables,
	})
	if err != nil {
		return err
	}
	var result struct {
		Metrics *metricsResult `json:"metrics"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return fmt.Errorf("decoding aggregation: %w", err)
	}
	metrics := result.Metrics
	if metrics == nil {
		metrics = &metricsResult{}
	}
	sampled := metrics.SampleFactor > 1

	if wantsJSON(cmd) {
		var raw struct {
			Metrics map[string]any `json:"metrics"`
		}
		_ = json.Unmarshal(data, &raw)
		if raw.Metrics == nil {
			raw.Metrics = map[string]any{}
		}
		raw.Metrics["sampled"] = sampled
		return writeJSON(cmd, raw.Metrics)
	}

	out := cmd.OutOrStdout()
	description := fmt.Sprintf("%s %s", productType, function)
	if column != "" {
		description += "(" + column + ")"
	}
	if query != "" {
		description += fmt.Sprintf(", query %q", query)
	}
	if len(groups) > 0 {
		description += ", grouped by " + strings.Join(groups, ",")
	}
	fmt.Fprintf(out, "%s, %s to %s, %d buckets\n", description, dr["start_date"], dr["end_date"], bucketCount)
	if sampled {
		fmt.Fprintf(out, "SAMPLED: sample factor %.2f. The API read a sample of the records, so the values are estimates. For exact counts, use a shorter time range.\n", metrics.SampleFactor)
	}

	// Count and Sum buckets with no records come back as 0, so 0 means "no data" there.
	// For other functions (Min, Avg, percentiles, ...) 0 is a real value; only a
	// missing value means the bucket had no records.
	countLike := function == "Count" || function == "Sum" || strings.HasPrefix(function, "CountDistinct")
	hasValue := func(v *float64) bool {
		return v != nil && (*v != 0 || !countLike)
	}
	empty := true
	for _, b := range metrics.Buckets {
		if hasValue(b.MetricValue) {
			empty = false
		}
	}
	if empty {
		printEmpty(cmd, strings.ToLower(productType), queryProblems(cmd, clients.Observability, t, query))
		return nil
	}

	w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	header := "BUCKET START\t"
	if len(groups) > 0 {
		header += "GROUP\t"
	}
	fmt.Fprintln(w, header+"VALUE")
	totals := map[string]float64{}
	var groupOrder []string
	for _, b := range metrics.Buckets {
		if !hasValue(b.MetricValue) {
			continue
		}
		row := bucketTime(b.BucketMin) + "\t"
		group := strings.Join(b.Group, ", ")
		if len(groups) > 0 {
			row += orDash(group) + "\t"
		}
		if _, seen := totals[group]; !seen {
			groupOrder = append(groupOrder, group)
		}
		totals[group] += *b.MetricValue
		fmt.Fprintln(w, row+formatNumber(*b.MetricValue))
	}
	_ = w.Flush()

	// A total is a sum of the buckets, so it has a meaning only for Count and Sum.
	if bucketCount > 1 && (function == "Count" || function == "Sum") {
		if sampled {
			fmt.Fprintln(out, "Totals (estimates):")
		} else {
			fmt.Fprintln(out, "Totals:")
		}
		for _, g := range groupOrder {
			label := "all"
			if len(groups) > 0 {
				label = orDash(g)
			}
			fmt.Fprintf(out, "  %s: %s\n", label, formatNumber(totals[g]))
		}
	}

	return nil
}

// bucketTime prints the start of a time bucket. The API gives it in Unix seconds.
func bucketTime(seconds float64) string {
	return time.Unix(int64(seconds), 0).UTC().Format("2006-01-02T15:04:05Z")
}

func formatNumber(v float64) string {
	if v == float64(int64(v)) {
		return fmt.Sprintf("%d", int64(v))
	}

	return fmt.Sprintf("%.2f", v)
}
