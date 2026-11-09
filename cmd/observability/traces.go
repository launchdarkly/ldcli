package observability

import (
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

func newTracesSearchCmd(clients Clients) *cobra.Command {
	cmd := newSearchCmd(
		clients,
		"search",
		"Search trace spans",
		`Search trace spans in a time range, newest first.

The compact output prints one line for each span: timestamp, duration, service, span name, status, and trace ID. To add attributes, use --attributes. To get the full spans with all attributes, use --json.

To find the slowest spans, use --sort-column duration. Keep the time range at 24 hours or less. For longer time ranges, use 'ldcli observability aggregate'.`,
		`  ldcli observability traces search --project default --query 'service_name=api' --start 1h
  ldcli observability traces search --project default --query 'span_name="GET /users"' --sort-column duration
  ldcli observability traces search --project default --query 'has_errors=true' --attributes http.route`,
		searchSpec{
			what:            "traces",
			query:           tracesQuery,
			field:           "traces",
			attributesField: "traceAttributes",
			compact: func(node map[string]any) []string {
				status := "OK"
				if hasErrors, _ := node["hasErrors"].(bool); hasErrors {
					status = "ERROR"
				}
				if code := stringField(node, "statusCode"); strings.EqualFold(code, "error") {
					status = "ERROR"
				}
				line := []string{
					shortTime(stringField(node, "timestamp")),
					formatDuration(node["duration"]),
					orDash(stringField(node, "serviceName")),
					orDash(stringField(node, "spanName")),
					status,
					"trace=" + orDash(stringField(node, "traceID")),
				}
				if message := stringField(node, "statusMessage"); message != "" {
					line = append(line, message)
				}
				return line
			},
		},
		func(cmd *cobra.Command, params map[string]any) {
			if column, _ := cmd.Flags().GetString(sortColumnFlag); column != "" {
				direction, _ := cmd.Flags().GetString(directionFlag)
				params["sort"] = map[string]any{"column": column, "direction": strings.ToUpper(direction)}
			}
		},
	)
	cmd.Flags().String(sortColumnFlag, "", "Column to sort by, for example duration. --direction sets the order. Defaults to timestamp")

	return cmd
}

// formatDuration prints a span duration. The API gives it in nanoseconds.
func formatDuration(value any) string {
	ns, ok := value.(float64)
	if !ok {
		return "-"
	}
	d := time.Duration(ns)
	switch {
	case d >= time.Second:
		return fmt.Sprintf("%.2fs", d.Seconds())
	case d >= time.Millisecond:
		return fmt.Sprintf("%.1fms", float64(d)/float64(time.Millisecond))
	default:
		return fmt.Sprintf("%.0fus", float64(d)/float64(time.Microsecond))
	}
}
