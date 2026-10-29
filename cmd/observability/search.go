package observability

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/launchdarkly/ldcli/cmd/validators"
	"github.com/launchdarkly/ldcli/internal/observability"
)

const (
	attributesFlag  = "attributes"
	environmentFlag = "deployment-environment"
	sortColumnFlag  = "sort-column"
	truncateFlag    = "truncate"

	defaultLimit    = 20
	maxLimit        = 100
	defaultTruncate = 300
)

// searchSpec is what differs between "logs search" and "traces search".
type searchSpec struct {
	what            string
	query           string
	field           string
	attributesField string
	compact         func(node map[string]any) []string
}

func addSearchFlags(cmd *cobra.Command) {
	addTimeRangeFlags(cmd)
	cmd.Flags().String(queryFlag, "", "Search query, for example: level=error AND service_name=api. Empty matches all records")
	cmd.Flags().Int(limitFlag, defaultLimit, fmt.Sprintf("Number of records to return, at most %d", maxLimit))
	cmd.Flags().String(directionFlag, "DESC", "Sort direction of the timestamp: DESC (newest first) or ASC")
	cmd.Flags().String(environmentFlag, "", "Only return records with this environment attribute, for example production. This is the environment that the observability SDK reports, not a LaunchDarkly environment key")
	cmd.Flags().StringSlice(attributesFlag, nil, "Comma-separated attribute keys to add to each line of the compact output, for example error.message,code.function")
	cmd.Flags().Int(truncateFlag, defaultTruncate, "Maximum characters for each value in the compact output. 0 shows full values")
}

// searchQuery adds the --environment filter to the --query value.
func searchQuery(cmd *cobra.Command) string {
	query, _ := cmd.Flags().GetString(queryFlag)
	env, _ := cmd.Flags().GetString(environmentFlag)
	if env == "" {
		return query
	}
	filter := fmt.Sprintf("environment=%q", env)
	if strings.TrimSpace(query) == "" {
		return filter
	}

	return fmt.Sprintf("(%s) AND %s", query, filter)
}

func newSearchCmd(clients Clients, use, short, long, example string, spec searchSpec, extraParams func(*cobra.Command, map[string]any)) *cobra.Command {
	cmd := &cobra.Command{
		Use:     use,
		Short:   short,
		Long:    long,
		Example: example,
		Args:    validators.Validate(),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSearch(cmd, clients, spec, extraParams)
		},
	}
	addSearchFlags(cmd)

	return cmd
}

func runSearch(cmd *cobra.Command, clients Clients, spec searchSpec, extraParams func(*cobra.Command, map[string]any)) error {
	dr, err := dateRange(cmd, time.Now())
	if err != nil {
		return err
	}
	limit, _ := cmd.Flags().GetInt(limitFlag)
	if limit < 1 || limit > maxLimit {
		return fmt.Errorf("--%s must be from 1 to %d", limitFlag, maxLimit)
	}
	direction, _ := cmd.Flags().GetString(directionFlag)
	direction = strings.ToUpper(direction)
	if direction != "ASC" && direction != "DESC" {
		return fmt.Errorf("--%s must be ASC or DESC", directionFlag)
	}

	t, err := resolveTarget(cmd, clients.Resources)
	if err != nil {
		return err
	}

	query := searchQuery(cmd)
	params := map[string]any{"query": query, "date_range": dr}
	if extraParams != nil {
		extraParams(cmd, params)
	}
	data, err := clients.Observability.Query(cmd.Context(), observability.Request{
		AccessToken: t.accessToken,
		Endpoint:    t.endpoint,
		ProjectID:   t.projectID,
		Query:       spec.query,
		Variables: map[string]any{
			"project_id": t.projectID,
			"params":     params,
			"direction":  direction,
			"limit":      limit,
		},
	})
	if err != nil {
		return err
	}

	var result map[string]struct {
		Edges []struct {
			Node map[string]any `json:"node"`
		} `json:"edges"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return fmt.Errorf("decoding %s: %w", spec.what, err)
	}
	nodes := make([]map[string]any, 0, len(result[spec.field].Edges))
	for _, e := range result[spec.field].Edges {
		if e.Node != nil {
			nodes = append(nodes, e.Node)
		}
	}

	if wantsJSON(cmd) {
		return writeJSON(cmd, map[string]any{spec.field: nodes})
	}
	if len(nodes) == 0 {
		printEmpty(cmd, spec.what, queryProblems(cmd, clients.Observability, t, query))
		return nil
	}

	attributeKeys, _ := cmd.Flags().GetStringSlice(attributesFlag)
	maxLength, _ := cmd.Flags().GetInt(truncateFlag)
	out := cmd.OutOrStdout()
	for _, node := range nodes {
		attributes, _ := node[spec.attributesField].(map[string]any)
		line := spec.compact(node)
		for i := range line {
			line[i] = truncate(oneLine(line[i]), maxLength)
		}
		for _, key := range attributeKeys {
			if value, ok := lookupAttribute(attributes, key); ok {
				line = append(line, key+"="+truncate(oneLine(formatValue(value)), maxLength))
			}
		}
		fmt.Fprintln(out, strings.Join(line, "  "))
	}
	if len(nodes) == limit {
		fmt.Fprintf(out, "Showed %d %s, the --%s value. More records can match.\n", len(nodes), spec.what, limitFlag)
	}

	return nil
}

// lookupAttribute finds a dotted key such as "code.function". It tries the full key
// first, then the nested path, because the API can return both forms.
func lookupAttribute(attributes map[string]any, key string) (any, bool) {
	if attributes == nil {
		return nil, false
	}
	if value, ok := attributes[key]; ok {
		return value, true
	}
	parts := strings.SplitN(key, ".", 2)
	if len(parts) < 2 {
		return nil, false
	}
	child, ok := attributes[parts[0]].(map[string]any)
	if !ok {
		return nil, false
	}

	return lookupAttribute(child, parts[1])
}

func formatValue(value any) string {
	switch v := value.(type) {
	case nil:
		return ""
	case string:
		return v
	case float64:
		return fmt.Sprintf("%v", v)
	default:
		b, err := json.Marshal(v)
		if err != nil {
			return fmt.Sprintf("%v", v)
		}
		return string(b)
	}
}

func stringField(node map[string]any, key string) string {
	return formatValue(node[key])
}

// shortTime prints an API timestamp in UTC with second precision.
func shortTime(value string) string {
	t, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return value
	}

	return t.UTC().Format("2006-01-02T15:04:05Z")
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}

	return s
}
