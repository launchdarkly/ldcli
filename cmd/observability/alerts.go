package observability

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	o11y "github.com/launchdarkly/ldcli/internal/observability"
)

const (
	alertIDFlag  = "alert-id"
	lookbackFlag = "lookback-days"

	defaultInvestigationCooldown       = 86400
	defaultInvestigationPermissionMode = "read"
)

var (
	alertProductTypes        = []string{"Logs", "Traces", "Errors", "Sessions", "Metrics", "Events"}
	thresholdTypes           = []string{"Constant", "Anomaly"}
	thresholdConditions      = []string{"Above", "Below", "Outside"}
	alertDestinationTypes    = []string{"Slack", "Discord", "MicrosoftTeams", "Webhook", "Email"}
	investigationPermissions = []string{"read", "read-write"}
)

type fieldKind int

const (
	kindString fieldKind = iota
	kindInt
	kindFloat
	kindBool
	kindStrings
	kindDestinations
)

// alertField ties a CLI flag to the GraphQL argument it sets on createAlert/updateAlert.
type alertField struct {
	flag     string
	variable string
	kind     fieldKind
	usage    string
	allowed  []string
}

var alertFields = []alertField{
	{flag: "name", variable: "name", kind: kindString, usage: "The alert name"},
	{flag: "product-type", variable: "product_type", kind: kindString, usage: "Data to monitor: " + strings.Join(alertProductTypes, ", "), allowed: alertProductTypes},
	{flag: "function-type", variable: "function_type", kind: kindString, usage: "Aggregator to monitor (create defaults to Count): " + strings.Join(metricAggregators, ", "), allowed: metricAggregators},
	{flag: "function-column", variable: "function_column", kind: kindString, usage: "Field to aggregate, for example duration (omit for Count)"},
	{flag: "query", variable: "query", kind: kindString, usage: "Search query to filter the monitored data"},
	{flag: "group-by", variable: "group_by_keys", kind: kindStrings, usage: "Keys to alert on separately, for example service_name"},
	{flag: "threshold-value", variable: "threshold_value", kind: kindFloat, usage: "Value to compare against (required for Constant thresholds)"},
	{flag: "threshold-window", variable: "threshold_window", kind: kindInt, usage: "Evaluation window in seconds"},
	{flag: "threshold-cooldown", variable: "threshold_cooldown", kind: kindInt, usage: "Seconds to wait before notifying again while still alerting"},
	{flag: "threshold-type", variable: "threshold_type", kind: kindString, usage: "Constant or Anomaly", allowed: thresholdTypes},
	{flag: "threshold-condition", variable: "threshold_condition", kind: kindString, usage: "Above, Below, or Outside (Anomaly only)", allowed: thresholdConditions},
	{flag: "message", variable: "message_content", kind: kindString, usage: "Custom text included in notifications"},
	{flag: "destinations", variable: "destinations", kind: kindDestinations, usage: `Notification destinations as a JSON array, for example '[{"destinationType":"Slack","typeId":"C0123","typeName":"#alerts"}]'. Types: ` + strings.Join(alertDestinationTypes, ", ")},
	{flag: "auto-investigate", variable: "auto_investigation_enabled", kind: kindBool, usage: "Start an automatic AI investigation each time the alert fires"},
	{flag: "investigation-cooldown", variable: "investigation_cooldown", kind: kindInt, usage: "Minimum seconds between automatic investigations (default 86400)"},
	{flag: "investigation-permission-mode", variable: "permission_mode", kind: kindString, usage: "read investigates and reports; read-write may also open a pull request (default read)", allowed: investigationPermissions},
	{flag: "investigation-repository", variable: "investigation_repositories", kind: kindStrings, usage: "Repository the investigation may read, as owner/name (repeatable)"},
	{flag: "investigation-prompt", variable: "investigation_prompt", kind: kindString, usage: "Extra instructions for each investigation"},
}

func (f alertField) register(cmd *cobra.Command) {
	switch f.kind {
	case kindInt:
		cmd.Flags().Int(f.flag, 0, f.usage)
	case kindFloat:
		cmd.Flags().Float64(f.flag, 0, f.usage)
	case kindBool:
		cmd.Flags().Bool(f.flag, false, f.usage)
	case kindStrings:
		cmd.Flags().StringSlice(f.flag, nil, f.usage)
	default:
		cmd.Flags().String(f.flag, "", f.usage)
	}
}

// value reads and validates the flag as the GraphQL argument value.
func (f alertField) value(cmd *cobra.Command) (interface{}, error) {
	switch f.kind {
	case kindInt:
		return cmd.Flags().GetInt(f.flag)
	case kindFloat:
		return cmd.Flags().GetFloat64(f.flag)
	case kindBool:
		return cmd.Flags().GetBool(f.flag)
	case kindStrings:
		v, err := cmd.Flags().GetStringSlice(f.flag)
		if v == nil {
			v = []string{}
		}
		return v, err
	case kindDestinations:
		raw, _ := cmd.Flags().GetString(f.flag)
		return parseDestinations(raw)
	default:
		v, _ := cmd.Flags().GetString(f.flag)
		if f.allowed != nil {
			return oneOf(f.flag, v, f.allowed)
		}
		return v, nil
	}
}

func parseDestinations(raw string) ([]map[string]interface{}, error) {
	var in []struct {
		DestinationType string `json:"destinationType"`
		TypeID          string `json:"typeId"`
		TypeName        string `json:"typeName"`
	}
	if err := json.Unmarshal([]byte(raw), &in); err != nil {
		return nil, fmt.Errorf("--destinations must be a JSON array of {destinationType, typeId, typeName}: %w", err)
	}
	out := make([]map[string]interface{}, 0, len(in))
	for _, d := range in {
		t, err := oneOf("destinations destinationType", d.DestinationType, alertDestinationTypes)
		if err != nil {
			return nil, err
		}
		if d.TypeID == "" {
			return nil, fmt.Errorf("--destinations entries require a typeId")
		}
		out = append(out, map[string]interface{}{"destination_type": t, "type_id": d.TypeID, "type_name": d.TypeName})
	}
	return out, nil
}

func newAlertsCmd(d deps) *cobra.Command {
	return newGroupCmd("alerts", "List, create, and manage observability alerts",
		newListAlertsCmd(d),
		newGetAlertCmd(d),
		newAlertFiringHistoryCmd(d),
		newCreateAlertCmd(d),
		newUpdateAlertCmd(d),
		newToggleAlertCmd(d, "enable", false),
		newToggleAlertCmd(d, "disable", true),
		newDeleteAlertCmd(d),
	)
}

type alertSummary struct {
	ID                 interface{} `json:"id"`
	Name               string      `json:"name"`
	ProductType        string      `json:"product_type"`
	FunctionType       string      `json:"function_type"`
	FunctionColumn     string      `json:"function_column"`
	Query              string      `json:"query"`
	Disabled           bool        `json:"disabled"`
	ThresholdValue     *float64    `json:"threshold_value"`
	ThresholdWindow    *int        `json:"threshold_window"`
	ThresholdType      string      `json:"threshold_type"`
	ThresholdCondition string      `json:"threshold_condition"`
	AutoInvestigate    bool        `json:"auto_investigation_enabled"`
	Destinations       []struct {
		DestinationType string `json:"destination_type"`
		TypeName        string `json:"type_name"`
	} `json:"destinations"`
}

func (a alertSummary) threshold() string {
	if a.ThresholdType == "Anomaly" {
		return "anomaly"
	}
	if a.ThresholdValue == nil {
		return ""
	}
	cond := map[string]string{"Above": ">", "Below": "<"}[a.ThresholdCondition]
	if cond == "" {
		cond = ">"
	}
	out := cond + " " + str(*a.ThresholdValue)
	if a.ThresholdWindow != nil {
		out += fmt.Sprintf(" over %ds", *a.ThresholdWindow)
	}
	return out
}

func (a alertSummary) metric() string {
	if a.FunctionColumn != "" {
		return fmt.Sprintf("%s %s(%s)", a.ProductType, a.FunctionType, a.FunctionColumn)
	}
	return a.ProductType + " " + a.FunctionType
}

func newListAlertsCmd(d deps) *cobra.Command {
	return newOpCmd(d,
		"list",
		"List alerts",
		"List a project's observability alerts. Use `firing-history` to see whether an alert actually fires.",
		func(cmd *cobra.Command) {
			cmd.Flags().String(searchFlag, "", "Only return alerts whose name contains this text (case-insensitive)")
		},
		func(cmd *cobra.Command, gql o11y.Client, projectID string) (interface{}, func() string, error) {
			data, err := gql.Do(projectID, o11y.ListAlertsQuery, map[string]interface{}{"project_id": projectID})
			if err != nil {
				return nil, nil, err
			}

			var raw struct {
				Alerts []json.RawMessage `json:"alerts"`
			}
			if err := decode(data, &raw); err != nil {
				return nil, nil, err
			}
			search, _ := cmd.Flags().GetString(searchFlag)
			matched := []json.RawMessage{}
			summaries := []alertSummary{}
			for _, r := range raw.Alerts {
				var a alertSummary
				if err := decode(r, &a); err != nil {
					return nil, nil, err
				}
				if search != "" && !strings.Contains(strings.ToLower(a.Name), strings.ToLower(search)) {
					continue
				}
				matched = append(matched, r)
				summaries = append(summaries, a)
			}

			return map[string]interface{}{"alerts": matched}, func() string {
				if len(summaries) == 0 {
					return "No alerts found."
				}
				rows := make([][]string, 0, len(summaries))
				for _, a := range summaries {
					status := "enabled"
					if a.Disabled {
						status = "disabled"
					}
					rows = append(rows, []string{str(a.ID), a.Name, a.metric(), a.threshold(), status})
				}
				return table([]string{"ID", "NAME", "METRIC", "THRESHOLD", "STATUS"}, rows)
			}, nil
		},
	)
}

func newGetAlertCmd(d deps) *cobra.Command {
	return newOpCmd(d,
		"get",
		"Get an alert",
		"Get the full configuration of an observability alert.",
		func(cmd *cobra.Command) {
			addRequiredIntFlag(cmd, alertIDFlag, "The alert ID")
		},
		func(cmd *cobra.Command, gql o11y.Client, projectID string) (interface{}, func() string, error) {
			id, _ := cmd.Flags().GetInt(alertIDFlag)
			alert, raw, err := fetchAlert(gql, projectID, id)
			if err != nil {
				return nil, nil, err
			}
			var a alertSummary
			if err := decode(raw, &a); err != nil {
				return nil, nil, err
			}

			return map[string]interface{}{"alert": raw}, func() string {
				dests := make([]string, 0, len(a.Destinations))
				for _, dst := range a.Destinations {
					dests = append(dests, dst.DestinationType+" "+dst.TypeName)
				}
				status := "enabled"
				if a.Disabled {
					status = "disabled"
				}
				return keyValues([][2]string{
					{"ID", str(a.ID)},
					{"Name", a.Name},
					{"Status", status},
					{"Metric", a.metric()},
					{"Query", a.Query},
					{"Group by", strings.Join(toStrings(alert["group_by_keys"]), ", ")},
					{"Threshold", a.threshold()},
					{"Cooldown", str(alert["threshold_cooldown"])},
					{"Destinations", strings.Join(dests, ", ")},
					{"Auto-investigate", fmt.Sprint(a.AutoInvestigate)},
					{"URL", uiURL("alerts", str(a.ID))},
				})
			}, nil
		},
	)
}

func toStrings(v interface{}) []string {
	items, _ := v.([]interface{})
	out := make([]string, 0, len(items))
	for _, i := range items {
		out = append(out, str(i))
	}
	return out
}

// fetchAlert returns the alert both decoded generically and as raw JSON.
func fetchAlert(gql o11y.Client, projectID string, id int) (map[string]interface{}, json.RawMessage, error) {
	data, err := gql.Do(projectID, o11y.GetAlertQuery, map[string]interface{}{"id": fmt.Sprint(id)})
	if err != nil {
		return nil, nil, err
	}
	var res struct {
		Alert json.RawMessage `json:"alert"`
	}
	if err := decode(data, &res); err != nil {
		return nil, nil, err
	}
	if len(res.Alert) == 0 || string(res.Alert) == "null" {
		return nil, nil, fmt.Errorf("alert %d not found", id)
	}
	var alert map[string]interface{}
	if err := decode(res.Alert, &alert); err != nil {
		return nil, nil, err
	}
	return alert, res.Alert, nil
}

type alertStateChange struct {
	Timestamp                   string `json:"timestamp"`
	State                       string `json:"state"`
	GroupByKey                  string `json:"groupByKey"`
	InvestigationConversationID string `json:"investigationConversationId,omitempty"`
}

func newAlertFiringHistoryCmd(d deps) *cobra.Command {
	return newOpCmd(d,
		"firing-history",
		"Show when an alert fired",
		"Show when an alert fired within a lookback window, and its current state per group.\nAn alert that is enabled but never fires, or is stuck in NoData, is usually stale.",
		func(cmd *cobra.Command) {
			addRequiredIntFlag(cmd, alertIDFlag, "The alert ID")
			cmd.Flags().Int(lookbackFlag, 30, "How many days back to look")
			cmd.Flags().Int(limitFlag, 20, "Number of firings to return, newest first (max 100)")
		},
		func(cmd *cobra.Command, gql o11y.Client, projectID string) (interface{}, func() string, error) {
			id, _ := cmd.Flags().GetInt(alertIDFlag)
			days, _ := cmd.Flags().GetInt(lookbackFlag)
			if days <= 0 {
				return nil, nil, fmt.Errorf("--%s must be positive", lookbackFlag)
			}
			limit, _ := cmd.Flags().GetInt(limitFlag)
			end := time.Now().UTC()
			start := end.AddDate(0, 0, -days)

			data, err := gql.Do(projectID, o11y.AlertFiringHistoryQuery, map[string]interface{}{
				"alert_id":   fmt.Sprint(id),
				"start_date": start.Format(time.RFC3339),
				"end_date":   end.Format(time.RFC3339),
				"page":       1,
				"count":      clamp(limit, 100),
			})
			if err != nil {
				return nil, nil, err
			}
			var history struct {
				Changes struct {
					TotalCount int                `json:"totalCount"`
					Changes    []alertStateChange `json:"alertStateChanges"`
				} `json:"alerting_alert_state_changes"`
			}
			if err := decode(data, &history); err != nil {
				return nil, nil, err
			}

			currentData, err := gql.Do(projectID, o11y.LastAlertStateChangesQuery, map[string]interface{}{"alert_id": fmt.Sprint(id)})
			if err != nil {
				return nil, nil, err
			}
			var current struct {
				Changes []alertStateChange `json:"last_alert_state_changes"`
			}
			if err := decode(currentData, &current); err != nil {
				return nil, nil, err
			}

			firings := history.Changes.Changes
			if firings == nil {
				firings = []alertStateChange{}
			}
			if current.Changes == nil {
				current.Changes = []alertStateChange{}
			}
			lastFired := ""
			if len(firings) > 0 {
				lastFired = firings[0].Timestamp
			}
			result := map[string]interface{}{
				"alert_id":      id,
				"lookback_days": days,
				"firing_count":  history.Changes.TotalCount,
				"last_fired_at": lastFired,
				"firings":       firings,
				"current_state": current.Changes,
			}

			return result, func() string {
				states := make([]string, 0, len(current.Changes))
				for _, c := range current.Changes {
					if c.GroupByKey != "" {
						states = append(states, c.GroupByKey+": "+c.State)
					} else {
						states = append(states, c.State)
					}
				}
				if lastFired == "" {
					lastFired = "never"
				}
				out := keyValues([][2]string{
					{"Firings", fmt.Sprintf("%d in the last %d days", history.Changes.TotalCount, days)},
					{"Last fired", lastFired},
					{"Current state", strings.Join(states, ", ")},
				})
				if len(firings) == 0 {
					return out
				}
				rows := make([][]string, 0, len(firings))
				for _, f := range firings {
					rows = append(rows, []string{f.Timestamp, f.State, f.GroupByKey, f.InvestigationConversationID})
				}
				return out + "\n\n" + table([]string{"TIMESTAMP", "STATE", "GROUP", "INVESTIGATION"}, rows)
			}, nil
		},
	)
}

func newCreateAlertCmd(d deps) *cobra.Command {
	return newOpCmd(d,
		"create",
		"Create an alert",
		"Create an observability alert that fires when a metric crosses a threshold. Check existing alerts with\n`list` first, and pick a threshold by previewing the metric with `ldcli observability aggregations query`. Example:\n\n"+
			"  ldcli observability alerts create --project default --name \"Slow API\" --product-type Traces \\\n"+
			"    --function-type P90 --function-column duration --query service_name=api \\\n"+
			"    --threshold-value 2000 --threshold-window 300",
		func(cmd *cobra.Command) {
			for _, f := range alertFields {
				f.register(cmd)
			}
			for _, required := range []string{"name", "product-type"} {
				_ = cmd.MarkFlagRequired(required)
				_ = cmd.Flags().SetAnnotation(required, "required", []string{"true"})
			}
		},
		func(cmd *cobra.Command, gql o11y.Client, projectID string) (interface{}, func() string, error) {
			vars := map[string]interface{}{
				"project_id":   projectID,
				"query":        "",
				"destinations": []map[string]interface{}{},
			}
			autoInvestigate, _ := cmd.Flags().GetBool("auto-investigate")
			for _, f := range alertFields {
				if !cmd.Flags().Changed(f.flag) {
					continue
				}
				// Investigation settings only apply when auto-investigation is on, so a plain alert
				// isn't created carrying settings the UI would render as configured.
				if strings.HasPrefix(f.flag, "investigation-") && !autoInvestigate {
					continue
				}
				v, err := f.value(cmd)
				if err != nil {
					return nil, nil, err
				}
				vars[f.variable] = v
			}
			if _, ok := vars["function_type"]; !ok {
				vars["function_type"] = "Count"
			}
			vars["auto_investigation_enabled"] = autoInvestigate
			if autoInvestigate {
				if _, ok := vars["investigation_cooldown"]; !ok {
					vars["investigation_cooldown"] = defaultInvestigationCooldown
				}
				if _, ok := vars["permission_mode"]; !ok {
					vars["permission_mode"] = defaultInvestigationPermissionMode
				}
			}

			data, err := gql.Do(projectID, o11y.CreateAlertMutation, vars)
			if err != nil {
				return nil, nil, err
			}
			var res struct {
				Alert json.RawMessage `json:"createAlert"`
			}
			if err := decode(data, &res); err != nil {
				return nil, nil, err
			}
			var a alertSummary
			if err := decode(res.Alert, &a); err != nil {
				return nil, nil, err
			}

			return map[string]interface{}{"alert": res.Alert}, func() string {
				return fmt.Sprintf("Created alert %q (ID %s)\n%s", a.Name, str(a.ID), uiURL("alerts", str(a.ID)))
			}, nil
		},
	)
}

// updateEchoedFields are updateAlert arguments the CLI doesn't expose but must send back
// unchanged, because the backend writes every omitted argument as NULL.
var updateEchoedFields = []string{
	"population_function_type", "population_query", "population_window", "group_by_key",
	"warn_threshold_value", "no_data_behavior", "send_resolved_alert", "hide_graph", "sql",
	"evaluation_delay_seconds",
}

func newUpdateAlertCmd(d deps) *cobra.Command {
	return newOpCmd(d,
		"update",
		"Update an alert",
		"Update an observability alert. Only the flags you pass change; everything else is preserved.\n--destinations replaces the whole destination list. Use `enable` or `disable` to toggle an alert.",
		func(cmd *cobra.Command) {
			addRequiredIntFlag(cmd, alertIDFlag, "The alert ID")
			for _, f := range alertFields {
				f.register(cmd)
			}
		},
		func(cmd *cobra.Command, gql o11y.Client, projectID string) (interface{}, func() string, error) {
			id, _ := cmd.Flags().GetInt(alertIDFlag)
			changed := false
			for _, f := range alertFields {
				changed = changed || cmd.Flags().Changed(f.flag)
			}
			if !changed {
				return nil, nil, fmt.Errorf("nothing to update: pass at least one field flag, for example --threshold-value")
			}

			// The backend's updateAlert writes every omitted argument as NULL and replaces the
			// destination list, so start from the current config and merge the changes over it.
			alert, _, err := fetchAlert(gql, projectID, id)
			if err != nil {
				return nil, nil, err
			}

			vars := map[string]interface{}{
				"project_id": projectID,
				"alert_id":   fmt.Sprint(id),
			}
			for _, name := range updateEchoedFields {
				vars[name] = alert[name]
			}
			for _, f := range alertFields {
				if f.kind == kindDestinations {
					vars[f.variable] = currentDestinations(alert)
				} else {
					vars[f.variable] = alert[f.variable]
				}
				if !cmd.Flags().Changed(f.flag) {
					continue
				}
				v, err := f.value(cmd)
				if err != nil {
					return nil, nil, err
				}
				vars[f.variable] = v
			}
			// Clear the legacy singular grouping key so it can't linger alongside new group_by_keys.
			if cmd.Flags().Changed("group-by") {
				vars["group_by_key"] = nil
			}
			if vars["auto_investigation_enabled"] == true {
				if vars["investigation_cooldown"] == nil {
					vars["investigation_cooldown"] = defaultInvestigationCooldown
				}
				if vars["permission_mode"] == nil {
					vars["permission_mode"] = defaultInvestigationPermissionMode
				}
			}

			data, err := gql.Do(projectID, o11y.UpdateAlertMutation, vars)
			if err != nil {
				return nil, nil, err
			}
			var res struct {
				Alert json.RawMessage `json:"updateAlert"`
			}
			if err := decode(data, &res); err != nil {
				return nil, nil, err
			}

			return map[string]interface{}{"alert": res.Alert}, func() string {
				return fmt.Sprintf("Updated alert %d\n%s", id, uiURL("alerts", fmt.Sprint(id)))
			}, nil
		},
	)
}

func currentDestinations(alert map[string]interface{}) []map[string]interface{} {
	items, _ := alert["destinations"].([]interface{})
	out := make([]map[string]interface{}, 0, len(items))
	for _, item := range items {
		dst, _ := item.(map[string]interface{})
		out = append(out, map[string]interface{}{
			"destination_type": dst["destination_type"],
			"type_id":          dst["type_id"],
			"type_name":        dst["type_name"],
		})
	}
	return out
}

func newToggleAlertCmd(d deps, use string, disabled bool) *cobra.Command {
	return newOpCmd(d,
		use,
		strings.ToUpper(use[:1])+use[1:]+" an alert",
		strings.ToUpper(use[:1])+use[1:]+" an observability alert without changing its configuration.",
		func(cmd *cobra.Command) {
			addRequiredIntFlag(cmd, alertIDFlag, "The alert ID")
		},
		func(cmd *cobra.Command, gql o11y.Client, projectID string) (interface{}, func() string, error) {
			id, _ := cmd.Flags().GetInt(alertIDFlag)
			data, err := gql.Do(projectID, o11y.UpdateAlertDisabledMutation, map[string]interface{}{
				"project_id": projectID,
				"alert_id":   fmt.Sprint(id),
				"disabled":   disabled,
			})
			if err != nil {
				return nil, nil, err
			}
			var res struct {
				Updated bool `json:"updateAlertDisabled"`
			}
			if err := decode(data, &res); err != nil {
				return nil, nil, err
			}
			if !res.Updated {
				return nil, nil, fmt.Errorf("alert %d was not updated", id)
			}

			return map[string]interface{}{"id": fmt.Sprint(id), "disabled": disabled}, func() string {
				return fmt.Sprintf("Alert %d %sd", id, use)
			}, nil
		},
	)
}

func newDeleteAlertCmd(d deps) *cobra.Command {
	return newOpCmd(d,
		"delete",
		"Delete an alert",
		"Permanently delete an observability alert.",
		func(cmd *cobra.Command) {
			addRequiredIntFlag(cmd, alertIDFlag, "The alert ID")
		},
		func(cmd *cobra.Command, gql o11y.Client, projectID string) (interface{}, func() string, error) {
			id, _ := cmd.Flags().GetInt(alertIDFlag)
			data, err := gql.Do(projectID, o11y.DeleteAlertMutation, map[string]interface{}{
				"project_id": projectID,
				"alert_id":   fmt.Sprint(id),
			})
			if err != nil {
				return nil, nil, err
			}
			var res struct {
				Deleted bool `json:"deleteAlert"`
			}
			if err := decode(data, &res); err != nil {
				return nil, nil, err
			}
			if !res.Deleted {
				return nil, nil, fmt.Errorf("alert %d was not deleted", id)
			}

			return map[string]interface{}{"id": fmt.Sprint(id), "deleted": true}, func() string {
				return fmt.Sprintf("Deleted alert %d", id)
			}, nil
		},
	)
}
