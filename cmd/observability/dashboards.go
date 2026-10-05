package observability

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/launchdarkly/ldcli/cmd/cliflags"
	o11y "github.com/launchdarkly/ldcli/internal/observability"
)

const (
	dashboardIDFlag   = "dashboard-id"
	nameFlag          = "name"
	timePresetFlag    = "time-preset"
	titleFlag         = "title"
	typeFlag          = "type"
	groupByLimitFlag  = "group-by-limit"
	limitAggFlag      = "limit-aggregator"
	limitColumnFlag   = "limit-column"
	bucketByFlag      = "bucket-by"
	displayFlag       = "display"
	defaultTimePreset = "last_24_hours"
)

// graphTypes maps shorthand chart names to the values the dashboard UI understands.
var graphTypes = map[string]string{
	"line":  "Line chart",
	"bar":   "Bar chart / histogram",
	"table": "Table",
}

// uiURL builds a link into the LaunchDarkly UI.
func uiURL(path ...string) string {
	u, _ := url.JoinPath(viper.GetString(cliflags.BaseURIFlag), path...)
	return u
}

func addRequiredIntFlag(cmd *cobra.Command, name, usage string) {
	cmd.Flags().Int(name, 0, usage)
	_ = cmd.MarkFlagRequired(name)
	_ = cmd.Flags().SetAnnotation(name, "required", []string{"true"})
}

func addRequiredStringFlag(cmd *cobra.Command, name, usage string) {
	cmd.Flags().String(name, "", usage)
	_ = cmd.MarkFlagRequired(name)
	_ = cmd.Flags().SetAnnotation(name, "required", []string{"true"})
}

func newDashboardsCmd(d deps) *cobra.Command {
	return newGroupCmd("dashboards", "List, create, and manage observability dashboards",
		newListDashboardsCmd(d),
		newGetDashboardCmd(d),
		newCreateDashboardCmd(d),
		newDeleteDashboardCmd(d),
		newCreateGraphCmd(d),
	)
}

func newListDashboardsCmd(d deps) *cobra.Command {
	return newOpCmd(d,
		"list",
		"List dashboards",
		"List a project's observability dashboards.",
		func(cmd *cobra.Command) {
			cmd.Flags().String(searchFlag, "", "Only return dashboards whose name matches")
			cmd.Flags().Int(limitFlag, 10, "Number of dashboards to return (max 50)")
		},
		func(cmd *cobra.Command, gql o11y.Client, projectID string) (interface{}, func() string, error) {
			search, _ := cmd.Flags().GetString(searchFlag)
			limit, _ := cmd.Flags().GetInt(limitFlag)
			data, err := gql.Do(projectID, o11y.ListVisualizationsQuery, map[string]interface{}{
				"project_id": projectID,
				"input":      search,
				"count":      clamp(limit, 50),
				"offset":     0,
			})
			if err != nil {
				return nil, nil, err
			}

			var res struct {
				Visualizations struct {
					Count   int `json:"count"`
					Results []struct {
						ID        string        `json:"id"`
						Name      string        `json:"name"`
						UpdatedAt string        `json:"updatedAt"`
						Graphs    []interface{} `json:"graphs"`
					} `json:"results"`
				} `json:"visualizations"`
			}
			if err := decode(data, &res); err != nil {
				return nil, nil, err
			}

			return data, func() string {
				results := res.Visualizations.Results
				if len(results) == 0 {
					return "No dashboards found."
				}
				rows := make([][]string, 0, len(results))
				for _, v := range results {
					rows = append(rows, []string{v.ID, v.Name, fmt.Sprint(len(v.Graphs)), v.UpdatedAt})
				}
				return table([]string{"ID", "NAME", "GRAPHS", "UPDATED"}, rows) +
					fmt.Sprintf("\n\nShowing %d of %d dashboards", len(results), res.Visualizations.Count)
			}, nil
		},
	)
}

func newGetDashboardCmd(d deps) *cobra.Command {
	return newOpCmd(d,
		"get",
		"Get a dashboard",
		"Get a dashboard and the configuration of each of its graphs.",
		func(cmd *cobra.Command) {
			addRequiredIntFlag(cmd, dashboardIDFlag, "The dashboard ID")
		},
		func(cmd *cobra.Command, gql o11y.Client, projectID string) (interface{}, func() string, error) {
			id, _ := cmd.Flags().GetInt(dashboardIDFlag)
			data, err := gql.Do(projectID, o11y.GetVisualizationQuery, map[string]interface{}{"id": fmt.Sprint(id)})
			if err != nil {
				return nil, nil, err
			}

			var res struct {
				Visualization *struct {
					ID         string `json:"id"`
					Name       string `json:"name"`
					TimePreset string `json:"timePreset"`
					Graphs     []struct {
						ID          string   `json:"id"`
						Title       string   `json:"title"`
						Type        string   `json:"type"`
						ProductType string   `json:"productType"`
						Query       string   `json:"query"`
						GroupByKeys []string `json:"groupByKeys"`
						Expressions []struct {
							Aggregator string `json:"aggregator"`
							Column     string `json:"column"`
						} `json:"expressions"`
					} `json:"graphs"`
				} `json:"visualization"`
			}
			if err := decode(data, &res); err != nil {
				return nil, nil, err
			}
			if res.Visualization == nil {
				return nil, nil, fmt.Errorf("dashboard %d not found", id)
			}
			v := res.Visualization

			return data, func() string {
				out := keyValues([][2]string{
					{"ID", v.ID},
					{"Name", v.Name},
					{"Time preset", v.TimePreset},
					{"URL", uiURL("dashboards", v.ID)},
				})
				if len(v.Graphs) == 0 {
					return out + "\n\nNo graphs."
				}
				rows := make([][]string, 0, len(v.Graphs))
				for _, g := range v.Graphs {
					exprs := make([]string, 0, len(g.Expressions))
					for _, e := range g.Expressions {
						if e.Column != "" {
							exprs = append(exprs, e.Aggregator+"("+e.Column+")")
						} else {
							exprs = append(exprs, e.Aggregator)
						}
					}
					rows = append(rows, []string{
						g.ID, g.Title, g.Type, g.ProductType, strings.Join(exprs, ", "),
						strings.Join(g.GroupByKeys, ", "), truncate(g.Query, 60),
					})
				}
				return out + "\n\n" + table([]string{"GRAPH ID", "TITLE", "TYPE", "PRODUCT", "EXPRESSIONS", "GROUP BY", "QUERY"}, rows)
			}, nil
		},
	)
}

func newCreateDashboardCmd(d deps) *cobra.Command {
	return newOpCmd(d,
		"create",
		"Create a dashboard",
		"Create an empty observability dashboard. Add graphs to it with `create-graph`.",
		func(cmd *cobra.Command) {
			addRequiredStringFlag(cmd, nameFlag, "The dashboard name")
			cmd.Flags().String(timePresetFlag, defaultTimePreset, "Default time range, for example last_24_hours, last_7_days, last_30_days")
		},
		func(cmd *cobra.Command, gql o11y.Client, projectID string) (interface{}, func() string, error) {
			name, _ := cmd.Flags().GetString(nameFlag)
			preset, _ := cmd.Flags().GetString(timePresetFlag)
			data, err := gql.Do(projectID, o11y.UpsertVisualizationMutation, map[string]interface{}{
				"visualization": map[string]interface{}{
					"projectId":  projectID,
					"name":       name,
					"timePreset": preset,
				},
			})
			if err != nil {
				return nil, nil, err
			}

			var res struct {
				ID interface{} `json:"upsertVisualization"`
			}
			if err := decode(data, &res); err != nil {
				return nil, nil, err
			}
			id := str(res.ID)

			return map[string]interface{}{"id": id, "name": name, "timePreset": preset}, func() string {
				return fmt.Sprintf("Created dashboard %q (ID %s)\n%s", name, id, uiURL("dashboards", id))
			}, nil
		},
	)
}

func newDeleteDashboardCmd(d deps) *cobra.Command {
	return newOpCmd(d,
		"delete",
		"Delete a dashboard",
		"Permanently delete a dashboard and its graphs.",
		func(cmd *cobra.Command) {
			addRequiredIntFlag(cmd, dashboardIDFlag, "The dashboard ID")
		},
		func(cmd *cobra.Command, gql o11y.Client, projectID string) (interface{}, func() string, error) {
			id, _ := cmd.Flags().GetInt(dashboardIDFlag)
			data, err := gql.Do(projectID, o11y.DeleteVisualizationMutation, map[string]interface{}{"id": fmt.Sprint(id)})
			if err != nil {
				return nil, nil, err
			}

			var res struct {
				Deleted bool `json:"deleteVisualization"`
			}
			if err := decode(data, &res); err != nil {
				return nil, nil, err
			}
			if !res.Deleted {
				return nil, nil, fmt.Errorf("dashboard %d was not deleted", id)
			}

			return map[string]interface{}{"id": fmt.Sprint(id), "deleted": true}, func() string {
				return fmt.Sprintf("Deleted dashboard %d", id)
			}, nil
		},
	)
}

func newCreateGraphCmd(d deps) *cobra.Command {
	return newOpCmd(d,
		"create-graph",
		"Add a graph to a dashboard",
		"Add a graph to an existing dashboard. Preview the data first with `ldcli observability aggregations query`\nusing the same --product-type, --query, --group-by, and --expression values. Example:\n\n"+
			"  ldcli observability dashboards create-graph --project default --dashboard-id 123 \\\n"+
			"    --title \"Errors by service\" --type bar --product-type errors --group-by service_name",
		func(cmd *cobra.Command) {
			addRequiredIntFlag(cmd, dashboardIDFlag, "The dashboard ID")
			addRequiredStringFlag(cmd, titleFlag, "The graph title")
			cmd.Flags().String(typeFlag, "line", "Chart type: line, bar, or table")
			addRequiredStringFlag(cmd, productTypeFlag, "Data to chart: "+strings.Join(productTypeNames(aggregationProductTypes), ", "))
			addExpressionFlag(cmd)
			cmd.Flags().String(queryFlag, "", "Search query to filter the data")
			cmd.Flags().StringSlice(groupByFlag, nil, "Keys to group by, for example service_name")
			cmd.Flags().Int(groupByLimitFlag, 10, "Maximum number of series when using --group-by")
			cmd.Flags().String(limitAggFlag, "", "Aggregator used to rank groups for --group-by-limit (default: the first expression's)")
			cmd.Flags().String(limitColumnFlag, "", "Column used with --limit-aggregator (default: the first expression's)")
			cmd.Flags().String(bucketByFlag, "Timestamp", "Key to bucket by")
			cmd.Flags().Int(bucketCountFlag, 12, "Number of buckets")
			cmd.Flags().String(displayFlag, "", "Display style, for example Line, Stacked, or Stacked area")
		},
		func(cmd *cobra.Command, gql o11y.Client, projectID string) (interface{}, func() string, error) {
			id, _ := cmd.Flags().GetInt(dashboardIDFlag)
			title, _ := cmd.Flags().GetString(titleFlag)
			rawType, _ := cmd.Flags().GetString(typeFlag)
			graphType, ok := graphTypes[strings.ToLower(rawType)]
			if !ok {
				return nil, nil, fmt.Errorf("--%s must be one of: line, bar, table", typeFlag)
			}
			pt, _ := cmd.Flags().GetString(productTypeFlag)
			productType, ok := aggregationProductTypes[strings.ToLower(pt)]
			if !ok {
				return nil, nil, fmt.Errorf("--%s must be one of: %s", productTypeFlag, strings.Join(productTypeNames(aggregationProductTypes), ", "))
			}
			rawExprs, _ := cmd.Flags().GetStringArray(expressionFlag)
			exprs, err := parseExpressions(rawExprs)
			if err != nil {
				return nil, nil, err
			}
			query, _ := cmd.Flags().GetString(queryFlag)
			bucketBy, _ := cmd.Flags().GetString(bucketByFlag)
			bucketCount, _ := cmd.Flags().GetInt(bucketCountFlag)

			graph := map[string]interface{}{
				"visualizationId": fmt.Sprint(id),
				"title":           title,
				"type":            graphType,
				"productType":     productType,
				"query":           query,
				"expressions":     exprs,
				"bucketByKey":     bucketBy,
				"bucketCount":     clamp(bucketCount, 1000),
			}
			if groupBy, _ := cmd.Flags().GetStringSlice(groupByFlag); len(groupBy) > 0 {
				limit, _ := cmd.Flags().GetInt(groupByLimitFlag)
				limitAgg, _ := cmd.Flags().GetString(limitAggFlag)
				if limitAgg == "" {
					limitAgg = exprs[0]["aggregator"].(string)
				} else if limitAgg, err = oneOf(limitAggFlag, limitAgg, metricAggregators); err != nil {
					return nil, nil, err
				}
				limitCol, _ := cmd.Flags().GetString(limitColumnFlag)
				if !cmd.Flags().Changed(limitColumnFlag) {
					limitCol = exprs[0]["column"].(string)
				}
				graph["groupByKeys"] = groupBy
				graph["limit"] = clamp(limit, 100)
				graph["limitFunctionType"] = limitAgg
				if limitCol != "" {
					graph["limitMetric"] = limitCol
				}
			}
			if display, _ := cmd.Flags().GetString(displayFlag); display != "" {
				graph["display"] = display
			}

			data, err := gql.Do(projectID, o11y.UpsertGraphMutation, map[string]interface{}{"graph": graph})
			if err != nil {
				return nil, nil, err
			}

			var res struct {
				Graph struct {
					ID    interface{} `json:"id"`
					Title string      `json:"title"`
					Type  string      `json:"type"`
				} `json:"upsertGraph"`
			}
			if err := decode(data, &res); err != nil {
				return nil, nil, err
			}

			return data, func() string {
				return fmt.Sprintf("Created graph %q (ID %s) on dashboard %d\n%s",
					title, str(res.Graph.ID), id, uiURL("dashboards", fmt.Sprint(id)))
			}, nil
		},
	)
}
