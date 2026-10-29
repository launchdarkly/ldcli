package observability

import (
	"strings"

	"github.com/spf13/cobra"
)

func newLogsSearchCmd(clients Clients) *cobra.Command {
	return newSearchCmd(
		clients,
		"search",
		"Search log records",
		`Search log records in a time range, newest first.

The compact output prints one line for each log: timestamp, level, service, and message. To add attributes, use --attributes. To get the full records with all attributes, use --json.

For counts and trends, use 'ldcli observability aggregate'. It is faster than a search over a long time range.`,
		`  ldcli observability logs search --project default --query 'level=error' --start 15m
  ldcli observability logs search --project default --query 'service_name=api "timeout"' --start 2026-01-15T10:00:00Z --end 2026-01-15T11:00:00Z
  ldcli observability logs search --project default --query 'level=error' --attributes error.message,code.function`,
		searchSpec{
			what:            "logs",
			query:           logsQuery,
			field:           "logs",
			attributesField: "logAttributes",
			compact: func(node map[string]any) []string {
				return []string{
					shortTime(stringField(node, "timestamp")),
					orDash(strings.ToUpper(stringField(node, "level"))),
					orDash(stringField(node, "serviceName")),
					stringField(node, "message"),
				}
			},
		},
		nil,
	)
}
