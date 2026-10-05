package observability_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/launchdarkly/ldcli/cmd"
	"github.com/launchdarkly/ldcli/internal/analytics"
	"github.com/launchdarkly/ldcli/internal/resources"
)

const projectID = "0123456789abcdef01234567"

type gqlRequest struct {
	Operation string
	Variables map[string]interface{}
	Header    http.Header
}

var opNameRe = regexp.MustCompile(`^\s*(?:query|mutation)\s+(\w+)`)

// fakeBackend serves canned GraphQL responses keyed by operation name and records requests.
func fakeBackend(t *testing.T, responses map[string]string) (*httptest.Server, *[]gqlRequest) {
	t.Helper()
	var requests []gqlRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Query     string                 `json:"query"`
			Variables map[string]interface{} `json:"variables"`
		}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		m := opNameRe.FindStringSubmatch(body.Query)
		require.Len(t, m, 2, "unparseable operation: %s", body.Query)
		requests = append(requests, gqlRequest{Operation: m[1], Variables: body.Variables, Header: r.Header.Clone()})

		res, ok := responses[m[1]]
		if !ok {
			t.Fatalf("unexpected operation %s", m[1])
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(res))
	}))
	t.Cleanup(server.Close)
	return server, &requests
}

func run(t *testing.T, server *httptest.Server, args ...string) ([]byte, error) {
	t.Helper()
	clients := cmd.APIClients{
		ResourcesClient: &resources.MockClient{Response: []byte(`{"_id": "` + projectID + `", "key": "default"}`)},
	}
	fullArgs := append([]string{"observability"}, args...)
	fullArgs = append(fullArgs, "--access-token", "api-abc123", "--backend-url", server.URL)
	return cmd.CallCmd(t, clients, analytics.NoopClientFn{}.Tracker(), fullArgs)
}

func TestLogsQuery(t *testing.T) {
	server, requests := fakeBackend(t, map[string]string{
		"GetProjectLogs": `{"data": {"logs": {"edges": [{"cursor": "c1", "node": {
			"timestamp": "2024-01-15T10:00:00Z", "level": "error", "message": "connection refused",
			"serviceName": "api"}}], "pageInfo": {"hasNextPage": false}}}}`,
	})

	t.Run("plaintext renders a table", func(t *testing.T) {
		out, err := run(t, server, "logs", "query", "--project", "default", "--query", "level=error", "--limit", "500", "--start-date", "1h")
		require.NoError(t, err)
		assert.Contains(t, string(out), "TIMESTAMP")
		assert.Contains(t, string(out), "connection refused")

		req := (*requests)[len(*requests)-1]
		assert.Equal(t, "api-abc123", req.Header.Get("Gonfalon-Authorization"))
		assert.Equal(t, projectID, req.Header.Get("x-ld-project-id"))
		assert.Equal(t, projectID, req.Variables["project_id"])
		assert.EqualValues(t, 50, req.Variables["limit"], "limit is capped")
		assert.Equal(t, "DESC", req.Variables["direction"])
		params := req.Variables["params"].(map[string]interface{})
		assert.Equal(t, "level=error", params["query"])
		assert.Contains(t, params["date_range"], "start_date")
	})

	t.Run("json passes the GraphQL data through", func(t *testing.T) {
		out, err := run(t, server, "logs", "query", "--project", "default", "--output", "json")
		require.NoError(t, err)
		var got map[string]interface{}
		require.NoError(t, json.Unmarshal(out, &got))
		assert.Contains(t, got, "logs")
	})

	t.Run("invalid direction is rejected", func(t *testing.T) {
		_, err := run(t, server, "logs", "query", "--project", "default", "--direction", "sideways")
		assert.ErrorContains(t, err, "ASC or DESC")
	})
}

func TestGraphQLErrorsAreReturned(t *testing.T) {
	server, _ := fakeBackend(t, map[string]string{
		"GetProjectTraces": `{"data": null, "errors": [{"message": "not authorized"}]}`,
	})

	_, err := run(t, server, "traces", "query", "--project", "default")
	assert.ErrorContains(t, err, "not authorized")
}

func TestOAuthTokenSentAsBearer(t *testing.T) {
	server, requests := fakeBackend(t, map[string]string{
		"GetServiceMap": `{"data": {"serviceMap": []}}`,
	})
	clients := cmd.APIClients{ResourcesClient: &resources.MockClient{}}

	_, err := cmd.CallCmd(t, clients, analytics.NoopClientFn{}.Tracker(), []string{
		"observability", "service-map", "get", "--project", projectID,
		"--access-token", "oauth-token", "--backend-url", server.URL,
	})
	require.NoError(t, err)
	assert.Equal(t, "Bearer oauth-token", (*requests)[0].Header.Get("Gonfalon-Authorization"))
}

func TestServiceMap(t *testing.T) {
	server, _ := fakeBackend(t, map[string]string{
		"GetServiceMap": `{"data": {"serviceMap": [
			{"source": "web", "target": "api"},
			{"source": "api", "target": "db"},
			{"source": "worker", "target": null}]}}`,
	})

	out, err := run(t, server, "service-map", "get", "--project", "default", "--output", "json")
	require.NoError(t, err)
	assert.JSONEq(t, `{
		"services": ["api", "db", "web", "worker"],
		"dependencies": [{"source": "web", "target": "api"}, {"source": "api", "target": "db"}]
	}`, string(out))
}

func TestKeysList(t *testing.T) {
	server, requests := fakeBackend(t, map[string]string{
		"GetErrorGroupsKeys": `{"data": {"errors_keys": [{"name": "service_name", "type": "String"}]}}`,
		"GetKeys":            `{"data": {"keys": [{"name": "cpu", "type": "Numeric"}]}}`,
	})

	out, err := run(t, server, "keys", "list", "--project", "default", "--product-type", "errors")
	require.NoError(t, err)
	assert.Contains(t, string(out), "service_name")
	assert.NotContains(t, (*requests)[0].Variables, "count", "errors_keys takes no count")

	_, err = run(t, server, "keys", "list", "--project", "default", "--product-type", "metric")
	require.NoError(t, err)
	assert.Equal(t, "Metrics", (*requests)[1].Variables["product_type"])

	_, err = run(t, server, "keys", "list", "--project", "default", "--product-type", "widgets")
	assert.ErrorContains(t, err, "--product-type must be one of")
}

func TestAggregationsQuery(t *testing.T) {
	server, requests := fakeBackend(t, map[string]string{
		"GetProjectMetricsBuckets": `{"data": {"metrics": {"buckets": [
			{"bucket_id": 0, "bucket_min": 1705312800, "bucket_max": 1705316400, "metric_value": 42,
			 "metric_type": "P90", "column": "duration", "group": ["api"]}]}}}`,
	})

	out, err := run(t, server, "aggregations", "query", "--project", "default", "--product-type", "requests",
		"--expression", "p90:duration", "--group-by", "service_name")
	require.NoError(t, err)
	assert.Contains(t, string(out), "P90(duration)")
	assert.Contains(t, string(out), "2024-01-15T10:00:00Z")

	vars := (*requests)[0].Variables
	assert.Equal(t, "Traces", vars["product_type"])
	assert.Equal(t, []interface{}{"service_name"}, vars["group_by"])
	assert.Equal(t, []interface{}{map[string]interface{}{"aggregator": "P90", "column": "duration"}}, vars["expressions"])
	assert.Equal(t, "P90", vars["limit_aggregator"])

	_, err = run(t, server, "aggregations", "query", "--project", "default", "--product-type", "logs", "--expression", "P75")
	assert.ErrorContains(t, err, "--expression must be one of")
}

func TestSessionFlagEvaluations(t *testing.T) {
	server, requests := fakeBackend(t, map[string]string{
		"GetFlagEvaluations": `{"data": {"traces": {"edges": [{"node": {"events": [{
			"timestamp": "2024-01-15T10:00:00Z", "name": "feature_flag",
			"attributes": {"feature_flag": {"key": "new-checkout", "result": {"value": "true"}}}}]}}]}}}`,
	})

	out, err := run(t, server, "sessions", "flag-evaluations", "--project", "default", "--session", "abc", "--query", "feature_flag.key=new-checkout")
	require.NoError(t, err)
	assert.Contains(t, string(out), "new-checkout")
	params := (*requests)[0].Variables["params"].(map[string]interface{})
	assert.Equal(t, "events.name=feature_flag AND secure_session_id=abc AND feature_flag.key=new-checkout", params["query"])
}

func TestCreateGraph(t *testing.T) {
	server, requests := fakeBackend(t, map[string]string{
		"UpsertGraph": `{"data": {"upsertGraph": {"id": "77", "title": "Errors by service", "type": "Bar chart / histogram"}}}`,
	})

	out, err := run(t, server, "dashboards", "create-graph", "--project", "default", "--dashboard-id", "12",
		"--title", "Errors by service", "--type", "bar", "--product-type", "errors", "--group-by", "service_name")
	require.NoError(t, err)
	assert.Contains(t, string(out), "ID 77")

	graph := (*requests)[0].Variables["graph"].(map[string]interface{})
	assert.Equal(t, "12", graph["visualizationId"])
	assert.Equal(t, "Bar chart / histogram", graph["type"])
	assert.Equal(t, "Errors", graph["productType"])
	assert.Equal(t, []interface{}{"service_name"}, graph["groupByKeys"])
	assert.EqualValues(t, 10, graph["limit"])
	assert.Equal(t, "Count", graph["limitFunctionType"])
	assert.NotContains(t, graph, "limitMetric")
}

func TestCreateAlert(t *testing.T) {
	server, requests := fakeBackend(t, map[string]string{
		"CreateAlert": `{"data": {"createAlert": {"id": "9", "name": "Slow API"}}}`,
	})

	t.Run("plain alert omits investigation settings", func(t *testing.T) {
		out, err := run(t, server, "alerts", "create", "--project", "default", "--name", "Slow API",
			"--product-type", "traces", "--function-type", "p90", "--function-column", "duration",
			"--threshold-value", "2000", "--threshold-window", "300", "--investigation-prompt", "ignored",
			"--destinations", `[{"destinationType":"slack","typeId":"C01","typeName":"#alerts"}]`)
		require.NoError(t, err)
		assert.Contains(t, string(out), `Created alert "Slow API" (ID 9)`)

		vars := (*requests)[len(*requests)-1].Variables
		assert.Equal(t, "Traces", vars["product_type"])
		assert.Equal(t, "P90", vars["function_type"])
		assert.EqualValues(t, 2000, vars["threshold_value"])
		assert.Equal(t, false, vars["auto_investigation_enabled"])
		assert.NotContains(t, vars, "investigation_prompt")
		assert.NotContains(t, vars, "investigation_cooldown")
		assert.Equal(t, []interface{}{map[string]interface{}{
			"destination_type": "Slack", "type_id": "C01", "type_name": "#alerts",
		}}, vars["destinations"])
	})

	t.Run("auto-investigation gets defaults", func(t *testing.T) {
		_, err := run(t, server, "alerts", "create", "--project", "default", "--name", "Errors",
			"--product-type", "Errors", "--auto-investigate")
		require.NoError(t, err)

		vars := (*requests)[len(*requests)-1].Variables
		assert.Equal(t, "Count", vars["function_type"])
		assert.Equal(t, true, vars["auto_investigation_enabled"])
		assert.EqualValues(t, 86400, vars["investigation_cooldown"])
		assert.Equal(t, "read", vars["permission_mode"])
		assert.Equal(t, []interface{}{}, vars["destinations"])
	})

	t.Run("bad destinations are rejected", func(t *testing.T) {
		_, err := run(t, server, "alerts", "create", "--project", "default", "--name", "x",
			"--product-type", "Errors", "--destinations", `[{"destinationType":"Pager","typeId":"1"}]`)
		assert.ErrorContains(t, err, "destinationType must be one of")
	})
}

func TestUpdateAlertMergesCurrentConfig(t *testing.T) {
	server, requests := fakeBackend(t, map[string]string{
		"GetAlert": `{"data": {"alert": {
			"id": "42", "name": "High errors", "product_type": "Errors", "function_type": "Count",
			"query": "service_name=api", "group_by_key": "legacy", "group_by_keys": ["service_name"],
			"threshold_value": 100, "threshold_window": 300, "threshold_cooldown": 600,
			"threshold_type": "Constant", "threshold_condition": "Above", "no_data_behavior": "Ignore",
			"send_resolved_alert": true, "auto_investigation_enabled": false,
			"destinations": [{"id": "d1", "destination_type": "Slack", "type_id": "C01", "type_name": "#alerts"}]}}}`,
		"UpdateAlert": `{"data": {"updateAlert": {"id": "42", "name": "High errors"}}}`,
	})

	_, err := run(t, server, "alerts", "update", "--project", "default", "--alert-id", "42",
		"--threshold-value", "250", "--group-by", "environment")
	require.NoError(t, err)

	require.Len(t, *requests, 2)
	assert.Equal(t, "GetAlert", (*requests)[0].Operation)
	vars := (*requests)[1].Variables
	assert.Equal(t, "42", vars["alert_id"])
	assert.EqualValues(t, 250, vars["threshold_value"], "changed field is applied")
	assert.EqualValues(t, 300, vars["threshold_window"], "unchanged field is preserved")
	assert.Equal(t, "service_name=api", vars["query"])
	assert.Equal(t, "Ignore", vars["no_data_behavior"], "fields the CLI doesn't expose are echoed back")
	assert.Equal(t, true, vars["send_resolved_alert"])
	assert.Equal(t, []interface{}{"environment"}, vars["group_by_keys"])
	assert.Nil(t, vars["group_by_key"], "legacy grouping is cleared when grouping changes")
	assert.Equal(t, []interface{}{map[string]interface{}{
		"destination_type": "Slack", "type_id": "C01", "type_name": "#alerts",
	}}, vars["destinations"], "destinations are preserved without the id field")
}

func TestUpdateAlertRequiresAChange(t *testing.T) {
	server, requests := fakeBackend(t, map[string]string{})

	_, err := run(t, server, "alerts", "update", "--project", "default", "--alert-id", "42")
	assert.ErrorContains(t, err, "nothing to update")
	assert.Empty(t, *requests)
}

func TestToggleAlert(t *testing.T) {
	server, requests := fakeBackend(t, map[string]string{
		"UpdateAlertDisabled": `{"data": {"updateAlertDisabled": true}}`,
	})

	out, err := run(t, server, "alerts", "disable", "--project", "default", "--alert-id", "42")
	require.NoError(t, err)
	assert.Equal(t, "Alert 42 disabled\n", string(out))
	assert.Equal(t, true, (*requests)[0].Variables["disabled"])

	out, err = run(t, server, "alerts", "enable", "--project", "default", "--alert-id", "42")
	require.NoError(t, err)
	assert.Equal(t, "Alert 42 enabled\n", string(out))
	assert.Equal(t, false, (*requests)[1].Variables["disabled"])
}

func TestAlertFiringHistory(t *testing.T) {
	server, _ := fakeBackend(t, map[string]string{
		"AlertFiringHistory": `{"data": {"alerting_alert_state_changes": {"totalCount": 3, "alertStateChanges": [
			{"id": "1", "timestamp": "2024-01-15T10:00:00Z", "state": "Alerting", "groupByKey": "api"}]}}}`,
		"LastAlertStateChanges": `{"data": {"last_alert_state_changes": [
			{"id": "2", "timestamp": "2024-01-15T11:00:00Z", "state": "Normal", "groupByKey": "api"}]}}`,
	})

	out, err := run(t, server, "alerts", "firing-history", "--project", "default", "--alert-id", "42", "--output", "json")
	require.NoError(t, err)
	var got map[string]interface{}
	require.NoError(t, json.Unmarshal(out, &got))
	assert.EqualValues(t, 3, got["firing_count"])
	assert.Equal(t, "2024-01-15T10:00:00Z", got["last_fired_at"])
	assert.Len(t, got["current_state"], 1)
}

func TestDeleteDashboardFailure(t *testing.T) {
	server, _ := fakeBackend(t, map[string]string{
		"DeleteVisualization": `{"data": {"deleteVisualization": false}}`,
	})

	_, err := run(t, server, "dashboards", "delete", "--project", "default", "--dashboard-id", "5")
	assert.ErrorContains(t, err, "dashboard 5 was not deleted")
}
