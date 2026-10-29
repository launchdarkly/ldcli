// Package observability has the hand-written "ldcli observability" commands. They
// query logs, traces, aggregations, and keys from LaunchDarkly observability.
package observability

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	cmdAnalytics "github.com/launchdarkly/ldcli/cmd/analytics"
	"github.com/launchdarkly/ldcli/cmd/cliflags"
	resourcescmd "github.com/launchdarkly/ldcli/cmd/resources"
	"github.com/launchdarkly/ldcli/internal/analytics"
	"github.com/launchdarkly/ldcli/internal/observability"
	"github.com/launchdarkly/ldcli/internal/output"
	"github.com/launchdarkly/ldcli/internal/resources"
)

const (
	directionFlag        = "direction"
	endFlag              = "end"
	limitFlag            = "limit"
	observabilityURLFlag = "observability-url"
	queryFlag            = "query"
	startFlag            = "start"

	defaultStart = "1h"
	timeFormat   = "2006-01-02T15:04:05.000Z"
)

// Clients holds the clients that the observability commands use.
type Clients struct {
	// Resources looks up the project _id from the project key.
	Resources resources.Client
	// Observability sends the GraphQL operations.
	Observability observability.Client
}

func NewObservabilityCmd(clients Clients, analyticsTrackerFn analytics.TrackerFn) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "observability",
		Aliases: []string{"o11y"},
		Short:   "Query logs, traces, and aggregations from LaunchDarkly observability",
		Long: `Query logs, traces, and aggregations from LaunchDarkly observability.

The commands take the LaunchDarkly project key and use your access token. They find the observability project for you.

By default, the commands print compact text, also when stdout is not a terminal. To get the full records as JSON, use --json or --output json.

Time ranges take a relative duration before now, such as 15m, 6h, 7d, or 2w, or an ISO 8601 time, such as 2026-01-15T10:00:00Z.

Queries use the observability search syntax, for example: level=error AND service_name=api "timeout".`,
		Example: `  ldcli observability logs search --project default --query 'level=error' --start 15m
  ldcli observability traces search --project default --query 'service_name=api' --sort-column duration
  ldcli observability aggregate --project default --product-type logs --query 'level=error' --group-by service_name --bucket-count 1
  ldcli observability keys --project default --product-type logs --search service
  ldcli observability keys --project default --product-type logs --key service_name`,
		Args: cobra.NoArgs,
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			tracker := analyticsTrackerFn(
				viper.GetString(cliflags.AccessTokenFlag),
				viper.GetString(cliflags.BaseURIFlag),
				viper.GetBool(cliflags.AnalyticsOptOut),
			)
			tracker.SendCommandRunEvent(cmdAnalytics.CmdRunEventProperties(
				cmd,
				"observability",
				map[string]interface{}{
					"action": strings.TrimPrefix(cmd.CommandPath(), cmd.Root().Name()+" observability "),
				}))
		},
	}

	cmd.PersistentFlags().String(cliflags.ProjectFlag, "", "The LaunchDarkly project key. Required, unless the configuration file sets project")
	_ = viper.BindPFlag(cliflags.ProjectFlag, cmd.PersistentFlags().Lookup(cliflags.ProjectFlag))
	cmd.PersistentFlags().String(
		observabilityURLFlag,
		"",
		fmt.Sprintf("The observability API URL. Defaults to the observability API of the instance that --%s names (%s for the default)", cliflags.BaseURIFlag, observability.DefaultEndpoint),
	)
	_ = viper.BindPFlag(observabilityURLFlag, cmd.PersistentFlags().Lookup(observabilityURLFlag))

	logsCmd := &cobra.Command{
		Use:   "logs",
		Short: "Search logs",
		Args:  cobra.NoArgs,
	}
	logsCmd.AddCommand(newLogsSearchCmd(clients))
	logsCmd.SetUsageTemplate(resourcescmd.SubcommandUsageTemplate())

	tracesCmd := &cobra.Command{
		Use:   "traces",
		Short: "Search traces",
		Args:  cobra.NoArgs,
	}
	tracesCmd.AddCommand(newTracesSearchCmd(clients))
	tracesCmd.SetUsageTemplate(resourcescmd.SubcommandUsageTemplate())

	cmd.AddCommand(logsCmd)
	cmd.AddCommand(tracesCmd)
	cmd.AddCommand(newAggregateCmd(clients))
	cmd.AddCommand(newKeysCmd(clients))
	cmd.SetUsageTemplate(resourcescmd.SubcommandUsageTemplate())

	return cmd
}

// target is the project and API endpoint that one command run queries.
type target struct {
	accessToken string
	endpoint    string
	projectID   string
	projectKey  string
}

// resolveTarget finds the observability project for the --project key and the
// observability API endpoint.
func resolveTarget(cmd *cobra.Command, client resources.Client) (target, error) {
	projectKey := viper.GetString(cliflags.ProjectFlag)
	if projectKey == "" {
		return target{}, fmt.Errorf("required flag \"%s\" not set", cliflags.ProjectFlag)
	}

	endpoint := viper.GetString(observabilityURLFlag)
	if endpoint == "" {
		var err error
		endpoint, err = observability.EndpointFor(viper.GetString(cliflags.BaseURIFlag))
		if err != nil {
			return target{}, err
		}
	}

	accessToken := viper.GetString(cliflags.AccessTokenFlag)
	projectID, err := resolveProjectID(cmd, client, accessToken, projectKey)
	if err != nil {
		return target{}, err
	}

	return target{
		accessToken: accessToken,
		endpoint:    endpoint,
		projectID:   projectID,
		projectKey:  projectKey,
	}, nil
}

// resolveProjectID gets the _id of the LaunchDarkly project. The observability API
// identifies a project by this ID. If the value is already an ID, it is used as it is.
func resolveProjectID(cmd *cobra.Command, client resources.Client, accessToken, projectKey string) (string, error) {
	if isProjectID(projectKey) {
		return projectKey, nil
	}

	path, _ := url.JoinPath(viper.GetString(cliflags.BaseURIFlag), "api/v2/projects", projectKey)
	res, err := client.MakeRequest(accessToken, "GET", path, "application/json", nil, nil, false)
	if err != nil {
		return "", output.NewCmdOutputError(err, cliflags.GetOutputKind(cmd))
	}

	var project struct {
		ID string `json:"_id"`
	}
	if err := json.Unmarshal(res, &project); err != nil || project.ID == "" {
		return "", fmt.Errorf("could not find the ID of project %q", projectKey)
	}

	return project.ID, nil
}

func isProjectID(value string) bool {
	if len(value) != 24 {
		return false
	}
	_, err := hex.DecodeString(value)

	return err == nil
}

// wantsJSON tells if the user asked for JSON output. Other ldcli commands print JSON
// when stdout is not a terminal. These commands print compact text in that case,
// because full log records are large and agents read the output.
func wantsJSON(cmd *cobra.Command) bool {
	if j, err := cmd.Root().PersistentFlags().GetBool(cliflags.JSONFlag); err == nil && j {
		return true
	}
	if f := cmd.Root().PersistentFlags().Lookup(cliflags.OutputFlag); f != nil && f.Changed {
		return f.Value.String() == "json"
	}
	if v, ok := os.LookupEnv("LD_OUTPUT"); ok {
		return v == "json"
	}
	if viper.InConfig(cliflags.OutputFlag) {
		return viper.GetString(cliflags.OutputFlag) == "json"
	}

	return false
}

func writeJSON(cmd *cobra.Command, value any) error {
	b, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	fmt.Fprintln(cmd.OutOrStdout(), string(b))

	return nil
}

// addTimeRangeFlags adds --start and --end.
func addTimeRangeFlags(cmd *cobra.Command) {
	cmd.Flags().String(startFlag, defaultStart, "Start of the time range: a duration before now (15m, 6h, 7d, 2w) or an ISO 8601 time")
	cmd.Flags().String(endFlag, "now", "End of the time range: now, a duration before now, or an ISO 8601 time")
}

// dateRange reads --start and --end and returns the date_range GraphQL input.
func dateRange(cmd *cobra.Command, now time.Time) (map[string]any, error) {
	startValue, _ := cmd.Flags().GetString(startFlag)
	endValue, _ := cmd.Flags().GetString(endFlag)

	start, err := parseTime(startValue, now)
	if err != nil {
		return nil, fmt.Errorf("invalid --%s: %w", startFlag, err)
	}
	end, err := parseTime(endValue, now)
	if err != nil {
		return nil, fmt.Errorf("invalid --%s: %w", endFlag, err)
	}
	if !start.Before(end) {
		return nil, fmt.Errorf("--%s must be before --%s", startFlag, endFlag)
	}

	return map[string]any{
		"start_date": start.UTC().Format(timeFormat),
		"end_date":   end.UTC().Format(timeFormat),
	}, nil
}

// parseTime reads "now", a duration before now (90s, 15m, 6h, 7d, 2w), or an ISO 8601
// time.
func parseTime(value string, now time.Time) (time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" || value == "now" {
		return now, nil
	}
	if t, err := time.Parse(time.RFC3339Nano, value); err == nil {
		return t, nil
	}
	if t, err := time.Parse("2006-01-02T15:04:05", value); err == nil {
		return t, nil
	}
	if t, err := time.Parse("2006-01-02", value); err == nil {
		return t, nil
	}

	duration := strings.TrimPrefix(value, "-")
	if duration == "" {
		return time.Time{}, fmt.Errorf("%q is not a duration or an ISO 8601 time", value)
	}
	units := map[byte]time.Duration{'d': 24 * time.Hour, 'w': 7 * 24 * time.Hour}
	if unit, ok := units[duration[len(duration)-1]]; ok {
		n, err := strconv.ParseFloat(duration[:len(duration)-1], 64)
		if err != nil || n < 0 {
			return time.Time{}, fmt.Errorf("%q is not a duration or an ISO 8601 time", value)
		}
		return now.Add(-time.Duration(n * float64(unit))), nil
	}
	d, err := time.ParseDuration(duration)
	if err != nil || d < 0 {
		return time.Time{}, fmt.Errorf("%q is not a duration or an ISO 8601 time", value)
	}

	return now.Add(-d), nil
}

// queryProblems asks the API why a query can match nothing, for example a value
// with "=" that needs quotes. It returns nil if the check fails.
func queryProblems(cmd *cobra.Command, client observability.Client, t target, query string) []string {
	if strings.TrimSpace(query) == "" {
		return nil
	}
	data, err := client.Query(cmd.Context(), observability.Request{
		AccessToken: t.accessToken,
		Endpoint:    t.endpoint,
		ProjectID:   t.projectID,
		Query:       validateSearchQueryQuery,
		Variables:   map[string]any{"project_id": t.projectID, "query": query},
	})
	if err != nil {
		return nil
	}
	var result struct {
		Problems []string `json:"validate_search_query"`
	}
	if json.Unmarshal(data, &result) != nil {
		return nil
	}

	return result.Problems
}

// printEmpty explains an empty result and gives the next step.
func printEmpty(cmd *cobra.Command, what string, problems []string) {
	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "No %s matched.\n", what)
	for _, p := range problems {
		fmt.Fprintf(out, "Query problem: %s\n", p)
	}
	if len(problems) > 0 {
		fmt.Fprintln(out, "Put quotes around values that contain '=', ':', spaces, or quotes.")
		return
	}
	fmt.Fprintln(out, "Try a longer time range, or use 'ldcli observability keys' to find the keys and values that the data has.")
}

// oneLine removes line breaks so that each record uses one line.
func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func truncate(s string, n int) string {
	r := []rune(s)
	if n <= 0 || len(r) <= n {
		return s
	}

	return string(r[:n]) + "..."
}
