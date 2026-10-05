package observability

import (
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/launchdarkly/ldcli/cmd/cliflags"
	resourcescmd "github.com/launchdarkly/ldcli/cmd/resources"
	"github.com/launchdarkly/ldcli/cmd/validators"
	"github.com/launchdarkly/ldcli/internal/errors"
	o11y "github.com/launchdarkly/ldcli/internal/observability"
	"github.com/launchdarkly/ldcli/internal/output"
	"github.com/launchdarkly/ldcli/internal/resources"
)

const (
	backendURLFlag = "backend-url"
	startDateFlag  = "start-date"
	endDateFlag    = "end-date"
	queryFlag      = "query"
	limitFlag      = "limit"
	directionFlag  = "direction"
	pageFlag       = "page"
	searchFlag     = "search"

	defaultStartDate = "24h"
)

var hex24 = regexp.MustCompile(`^[0-9a-fA-F]{24}$`)

// deps holds what every observability subcommand needs to talk to LaunchDarkly.
type deps struct {
	client  resources.Client
	version string
}

// opRunFn is the body of an observability command: given a ready-to-use GraphQL client and the
// resolved observability project ID, it performs the operation and returns the JSON value to
// print plus a function rendering the same value as plaintext.
type opRunFn func(cmd *cobra.Command, gql o11y.Client, projectID string) (interface{}, func() string, error)

// newOpCmd builds a leaf command with the shared --project flag, project resolution, and output
// handling. setupFlags registers any command-specific flags.
func newOpCmd(d deps, use, short, long string, setupFlags func(*cobra.Command), run opRunFn) *cobra.Command {
	cmd := &cobra.Command{
		Args:  validators.Validate(),
		Use:   use,
		Short: short,
		Long:  long,
		// Bind --project here rather than at construction: viper keeps only the most recent
		// binding per key, and many commands define a --project flag.
		PreRun: func(cmd *cobra.Command, args []string) {
			_ = viper.BindPFlag(cliflags.ProjectFlag, cmd.Flags().Lookup(cliflags.ProjectFlag))
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			outputKind := cliflags.GetOutputKind(cmd)
			accessToken := viper.GetString(cliflags.AccessTokenFlag)

			projectID, err := resolveProjectID(d.client, accessToken, viper.GetString(cliflags.ProjectFlag))
			if err != nil {
				return output.NewCmdOutputError(err, outputKind)
			}

			backendURL, _ := cmd.Flags().GetString(backendURLFlag)
			if backendURL == "" {
				backendURL = o11y.DefaultBackendURL
			}
			gql := o11y.Client{
				BackendURL:  backendURL,
				AccessToken: accessToken,
				CLIVersion:  d.version,
			}

			result, plaintext, err := run(cmd, gql, projectID)
			if err != nil {
				return output.NewCmdOutputError(err, outputKind)
			}

			if outputKind == "json" {
				b, err := json.Marshal(result)
				if err != nil {
					return errors.NewError(err.Error())
				}
				fmt.Fprintln(cmd.OutOrStdout(), string(b))
				return nil
			}
			fmt.Fprintln(cmd.OutOrStdout(), plaintext())
			return nil
		},
	}

	cmd.SetUsageTemplate(resourcescmd.SubcommandUsageTemplate())

	cmd.Flags().String(cliflags.ProjectFlag, "", "The project key")
	_ = cmd.MarkFlagRequired(cliflags.ProjectFlag)
	_ = cmd.Flags().SetAnnotation(cliflags.ProjectFlag, "required", []string{"true"})

	if setupFlags != nil {
		setupFlags(cmd)
	}

	return cmd
}

// resolveProjectID maps a LaunchDarkly project key to the project's ID, which is how the
// observability backend identifies projects. A 24-character hex ID is used as-is.
func resolveProjectID(client resources.Client, accessToken, projectKey string) (string, error) {
	if hex24.MatchString(projectKey) {
		return projectKey, nil
	}

	u, _ := url.JoinPath(viper.GetString(cliflags.BaseURIFlag), "api/v2/projects", projectKey)
	res, err := client.MakeRequest(accessToken, "GET", u, "application/json", nil, nil, false)
	if err != nil {
		return "", err
	}

	var project struct {
		ID string `json:"_id"`
	}
	if err := json.Unmarshal(res, &project); err != nil {
		return "", err
	}
	if project.ID == "" {
		return "", fmt.Errorf("project %s not found", projectKey)
	}

	return project.ID, nil
}

// addDateRangeFlags registers --start-date and --end-date.
func addDateRangeFlags(cmd *cobra.Command) {
	cmd.Flags().String(startDateFlag, defaultStartDate, "Start of the time range: an RFC 3339 timestamp (2024-01-15T10:00:00Z), a date (2024-01-15), or a duration before now (30m, 24h, 7d)")
	cmd.Flags().String(endDateFlag, "", "End of the time range, in the same formats as --start-date (default now)")
}

// dateRange returns the {start_date, end_date} input object for the command's date flags.
func dateRange(cmd *cobra.Command) (map[string]interface{}, error) {
	now := time.Now().UTC()
	startRaw, _ := cmd.Flags().GetString(startDateFlag)
	endRaw, _ := cmd.Flags().GetString(endDateFlag)

	start, err := parseTime(startRaw, now)
	if err != nil {
		return nil, fmt.Errorf("invalid --%s: %w", startDateFlag, err)
	}
	end := now
	if endRaw != "" {
		end, err = parseTime(endRaw, now)
		if err != nil {
			return nil, fmt.Errorf("invalid --%s: %w", endDateFlag, err)
		}
	}
	if !start.Before(end) {
		return nil, fmt.Errorf("--%s must be before --%s", startDateFlag, endDateFlag)
	}

	return map[string]interface{}{
		"start_date": start.Format(time.RFC3339),
		"end_date":   end.Format(time.RFC3339),
	}, nil
}

// parseTime accepts an RFC 3339 timestamp, a YYYY-MM-DD date, "now", or a duration before now
// (Go duration syntax plus a "d" suffix for days).
func parseTime(s string, now time.Time) (time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" || s == "now" {
		return now, nil
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t.UTC(), nil
	}
	if t, err := time.Parse(time.DateOnly, s); err == nil {
		return t.UTC(), nil
	}
	if days, ok := strings.CutSuffix(s, "d"); ok {
		if n, err := strconv.Atoi(days); err == nil && n > 0 {
			return now.AddDate(0, 0, -n), nil
		}
	}
	if d, err := time.ParseDuration(s); err == nil && d > 0 {
		return now.Add(-d), nil
	}
	return time.Time{}, fmt.Errorf("%q is not a timestamp, date, or duration", s)
}

// clamp bounds a user-supplied limit to [1, max].
func clamp(v, max int) int {
	if v < 1 {
		return 1
	}
	if v > max {
		return max
	}
	return v
}

func addDirectionFlag(cmd *cobra.Command) {
	cmd.Flags().String(directionFlag, "DESC", "Sort direction by timestamp: ASC or DESC")
}

func direction(cmd *cobra.Command) (string, error) {
	v, _ := cmd.Flags().GetString(directionFlag)
	v = strings.ToUpper(v)
	if v != "ASC" && v != "DESC" {
		return "", fmt.Errorf("--%s must be ASC or DESC", directionFlag)
	}
	return v, nil
}

// oneOf validates a case-insensitive enum flag value and returns its canonical spelling.
func oneOf(flag, value string, allowed []string) (string, error) {
	for _, a := range allowed {
		if strings.EqualFold(a, value) {
			return a, nil
		}
	}
	return "", fmt.Errorf("--%s must be one of: %s", flag, strings.Join(allowed, ", "))
}

// decode unmarshals a GraphQL data payload into v.
func decode(data json.RawMessage, v interface{}) error {
	if err := json.Unmarshal(data, v); err != nil {
		return fmt.Errorf("unable to parse observability API response: %w", err)
	}
	return nil
}

// table renders rows as aligned columns.
func table(headers []string, rows [][]string) string {
	var sb strings.Builder
	w := tabwriter.NewWriter(&sb, 0, 0, 3, ' ', 0)
	fmt.Fprintln(w, strings.Join(headers, "\t"))
	for _, row := range rows {
		cells := make([]string, len(row))
		for i, c := range row {
			cells[i] = strings.NewReplacer("\t", " ", "\n", " ").Replace(c)
		}
		fmt.Fprintln(w, strings.Join(cells, "\t"))
	}
	_ = w.Flush()
	return strings.TrimRight(sb.String(), "\n")
}

// keyValues renders label/value pairs, one per line.
func keyValues(pairs [][2]string) string {
	width := 0
	for _, p := range pairs {
		if len(p[0]) > width {
			width = len(p[0])
		}
	}
	lines := make([]string, 0, len(pairs))
	for _, p := range pairs {
		lines = append(lines, fmt.Sprintf("%s:%s  %s", p[0], strings.Repeat(" ", width-len(p[0])), p[1]))
	}
	return strings.Join(lines, "\n")
}

func truncate(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if len([]rune(s)) <= n {
		return s
	}
	return string([]rune(s)[:n-3]) + "..."
}

// str formats a decoded JSON value for display, rendering nil as empty.
func str(v interface{}) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	case bool, int:
		return fmt.Sprint(t)
	default:
		b, _ := json.Marshal(t)
		return string(b)
	}
}

func emptyHint(what string) string {
	return fmt.Sprintf("No %s found. Try widening --start-date, simplifying --query, or `ldcli observability keys list` to discover filterable keys.", what)
}
