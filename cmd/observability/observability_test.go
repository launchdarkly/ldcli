package observability_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/launchdarkly/ldcli/cmd"
	"github.com/launchdarkly/ldcli/internal/analytics"
	"github.com/launchdarkly/ldcli/internal/observability"
	"github.com/launchdarkly/ldcli/internal/resources"
)

const projectID = "0123456789abcdef01234567"

// graphQLRequest is one request that the fake observability API received.
type graphQLRequest struct {
	Authorization string
	ProjectHeader string
	Query         string
	Variables     map[string]any
}

// fakeAPI is an observability API that returns the responses in order and records
// the requests.
type fakeAPI struct {
	mu        sync.Mutex
	responses []string
	requests  []graphQLRequest
}

func (f *fakeAPI) start(t *testing.T) string {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var payload struct {
			Query     string         `json:"query"`
			Variables map[string]any `json:"variables"`
		}
		require.NoError(t, json.Unmarshal(body, &payload))

		f.mu.Lock()
		defer f.mu.Unlock()
		f.requests = append(f.requests, graphQLRequest{
			Authorization: r.Header.Get("Gonfalon-Authorization"),
			ProjectHeader: r.Header.Get("x-project-id"),
			Query:         payload.Query,
			Variables:     payload.Variables,
		})
		response := `{"data":{}}`
		if len(f.responses) > 0 {
			response, f.responses = f.responses[0], f.responses[1:]
		}
		_, _ = w.Write([]byte(response))
	}))
	t.Cleanup(server.Close)

	return server.URL
}

func run(t *testing.T, api *fakeAPI, args ...string) (string, error) {
	t.Helper()
	url := api.start(t)
	resourcesClient := &resources.MockClient{Response: []byte(`{"_id":"` + projectID + `","key":"default"}`)}
	base := []string{"--access-token", "api-test-token", "--observability-url", url, "--project", "default"}
	out, err := cmd.CallCmd(
		t,
		cmd.APIClients{
			ResourcesClient:     resourcesClient,
			ObservabilityClient: observability.NewClient("test"),
		},
		analytics.NoopClientFn{}.Tracker(),
		append(args, base...),
	)

	return string(out), err
}

const logsResponse = `{"data":{"logs":{"edges":[
  {"node":{"timestamp":"2026-10-29T20:01:02.123Z","level":"error","message":"recovered panic\nin handler","serviceName":"production-gonfalon","traceID":"t1",
    "logAttributes":{"error":{"message":"bad metricKey x/y"},"code.stacktrace":"goroutine 1 [running]: very long"}}},
  {"node":{"timestamp":"2026-10-29T20:00:00Z","level":"info","message":"ok","serviceName":"","logAttributes":{}}}
]}}}`

func TestLogsSearch(t *testing.T) {
	t.Run("prints one compact line for each log", func(t *testing.T) {
		api := &fakeAPI{responses: []string{logsResponse}}
		out, err := run(t, api, "observability", "logs", "search", "--query", "level=error", "--start", "15m", "--limit", "5")

		require.NoError(t, err)
		assert.Equal(t,
			"2026-10-29T20:01:02Z  ERROR  production-gonfalon  recovered panic in handler\n"+
				"2026-10-29T20:00:00Z  INFO  -  ok\n",
			out)
		require.Len(t, api.requests, 1)
		req := api.requests[0]
		assert.Equal(t, "api-test-token", req.Authorization)
		assert.Equal(t, projectID, req.ProjectHeader)
		assert.Contains(t, req.Query, "logs(")
		assert.Equal(t, projectID, req.Variables["project_id"])
		assert.Equal(t, "DESC", req.Variables["direction"])
		assert.Equal(t, float64(5), req.Variables["limit"])
		params := req.Variables["params"].(map[string]any)
		assert.Equal(t, "level=error", params["query"])
		dateRange := params["date_range"].(map[string]any)
		assert.NotEmpty(t, dateRange["start_date"])
		assert.NotEmpty(t, dateRange["end_date"])
	})

	t.Run("adds the attributes that --attributes names, nested or flat", func(t *testing.T) {
		api := &fakeAPI{responses: []string{logsResponse}}
		out, err := run(t, api, "o11y", "logs", "search", "--attributes", "error.message,code.stacktrace", "--truncate", "10")

		require.NoError(t, err)
		assert.Contains(t, out, "recovered ...  error.message=bad metric...  code.stacktrace=goroutine ...\n")
	})

	t.Run("prints full records with --json", func(t *testing.T) {
		api := &fakeAPI{responses: []string{logsResponse}}
		out, err := run(t, api, "observability", "logs", "search", "--json")

		require.NoError(t, err)
		var result struct {
			Logs []map[string]any `json:"logs"`
		}
		require.NoError(t, json.Unmarshal([]byte(out), &result))
		require.Len(t, result.Logs, 2)
		assert.Contains(t, result.Logs[0]["logAttributes"], "code.stacktrace")
	})

	t.Run("adds the --deployment-environment filter to the query", func(t *testing.T) {
		api := &fakeAPI{responses: []string{logsResponse}}
		_, err := run(t, api, "observability", "logs", "search", "--query", "level=error", "--deployment-environment", "production")

		require.NoError(t, err)
		params := api.requests[0].Variables["params"].(map[string]any)
		assert.Equal(t, `(level=error) AND environment="production"`, params["query"])
	})

	t.Run("explains an empty result with the query problems", func(t *testing.T) {
		api := &fakeAPI{responses: []string{
			`{"data":{"logs":{"edges":[]}}}`,
			`{"data":{"validate_search_query":["unquoted '=' in value"]}}`,
		}}
		out, err := run(t, api, "observability", "logs", "search", "--query", "url=*a=b*")

		require.NoError(t, err)
		assert.Contains(t, out, "No logs matched.\nQuery problem: unquoted '=' in value\n")
		require.Len(t, api.requests, 2)
		assert.Contains(t, api.requests[1].Query, "validate_search_query")
	})

	t.Run("says when more records can match", func(t *testing.T) {
		api := &fakeAPI{responses: []string{logsResponse}}
		out, err := run(t, api, "observability", "logs", "search", "--limit", "2")

		require.NoError(t, err)
		assert.Contains(t, out, "Showed 2 logs, the --limit value. More records can match.\n")
	})

	t.Run("returns GraphQL errors", func(t *testing.T) {
		api := &fakeAPI{responses: []string{`{"errors":[{"message":"401 - unauthorized"}],"data":null}`}}
		_, err := run(t, api, "observability", "logs", "search")

		require.EqualError(t, err, "observability API error: 401 - unauthorized")
	})

	t.Run("rejects a limit out of range", func(t *testing.T) {
		api := &fakeAPI{}
		_, err := run(t, api, "observability", "logs", "search", "--limit", "500")

		require.EqualError(t, err, "--limit must be from 1 to 100")
		assert.Empty(t, api.requests)
	})

	t.Run("rejects a start after the end", func(t *testing.T) {
		api := &fakeAPI{}
		_, err := run(t, api, "observability", "logs", "search", "--start", "1h", "--end", "2h")

		require.EqualError(t, err, "--start must be before --end")
	})
}

func TestTracesSearch(t *testing.T) {
	response := `{"data":{"traces":{"edges":[
	  {"node":{"timestamp":"2026-10-29T20:01:02Z","duration":1500000000,"serviceName":"api","spanName":"GET /users","hasErrors":true,"traceID":"abc","statusMessage":"boom","traceAttributes":{"http.route":"/users"}}},
	  {"node":{"timestamp":"2026-10-29T20:01:03Z","duration":2500000,"serviceName":"api","spanName":"db.query","hasErrors":false,"traceID":"abc","traceAttributes":{}}}
	]}}}`

	t.Run("prints one compact line for each span", func(t *testing.T) {
		api := &fakeAPI{responses: []string{response}}
		out, err := run(t, api, "observability", "traces", "search", "--attributes", "http.route")

		require.NoError(t, err)
		assert.Equal(t,
			"2026-10-29T20:01:02Z  1.50s  api  GET /users  ERROR  trace=abc  boom  http.route=/users\n"+
				"2026-10-29T20:01:03Z  2.5ms  api  db.query  OK  trace=abc\n",
			out)
		assert.Contains(t, api.requests[0].Query, "traces(")
	})

	t.Run("sends --sort-column as the sort", func(t *testing.T) {
		api := &fakeAPI{responses: []string{response}}
		_, err := run(t, api, "observability", "traces", "search", "--sort-column", "duration", "--direction", "asc")

		require.NoError(t, err)
		params := api.requests[0].Variables["params"].(map[string]any)
		assert.Equal(t, map[string]any{"column": "duration", "direction": "ASC"}, params["sort"])
		assert.Equal(t, "ASC", api.requests[0].Variables["direction"])
	})
}

func TestAggregate(t *testing.T) {
	t.Run("prints the buckets and shows the sample factor", func(t *testing.T) {
		api := &fakeAPI{responses: []string{`{"data":{"metrics":{"bucket_count":2,"sample_factor":93.58,"buckets":[
		  {"bucket_id":0,"bucket_min":1761696000,"bucket_max":1761782400,"metric_value":94,"metric_type":"Count","column":"","group":["bad metricKey x/y"]},
		  {"bucket_id":1,"bucket_min":1761782400,"bucket_max":1761868800,"metric_value":0,"metric_type":"Count","column":"","group":["bad metricKey x/y"]},
		  {"bucket_id":1,"bucket_min":1761782400,"bucket_max":1761868800,"metric_value":12,"metric_type":"Count","column":"","group":["bad metricKey x/../y"]}
		]}}}`}}
		out, err := run(t, api,
			"observability", "aggregate",
			"--product-type", "logs",
			"--query", "request_panic=true",
			"--group-by", "error.message",
			"--bucket-count", "2",
			"--start", "2025-10-29T00:00:00Z",
			"--end", "2025-10-31T00:00:00Z",
		)

		require.NoError(t, err)
		assert.Equal(t, `Logs Count, query "request_panic=true", grouped by error.message, 2025-10-29T00:00:00.000Z to 2025-10-31T00:00:00.000Z, 2 buckets
SAMPLED: sample factor 93.58. The API read a sample of the records, so the values are estimates. For exact counts, use a shorter time range.
BUCKET START          GROUP                 VALUE
2025-10-29T00:00:00Z  bad metricKey x/y     94
2025-10-30T00:00:00Z  bad metricKey x/../y  12
Totals (estimates):
  bad metricKey x/y: 94
  bad metricKey x/../y: 12
`, out)

		vars := api.requests[0].Variables
		assert.Equal(t, "Logs", vars["product_type"])
		assert.Equal(t, "Timestamp", vars["bucket_by"])
		assert.Equal(t, float64(2), vars["bucket_count"])
		assert.Equal(t, []any{"error.message"}, vars["group_by"])
		assert.Equal(t, []any{map[string]any{"aggregator": "Count", "column": ""}}, vars["expressions"])
		assert.Equal(t, "Count", vars["limit_aggregator"])
		assert.NotContains(t, vars, "limit_column")
	})

	t.Run("does not show a sample warning for exact results", func(t *testing.T) {
		api := &fakeAPI{responses: []string{`{"data":{"metrics":{"bucket_count":1,"sample_factor":1,"buckets":[
		  {"bucket_id":0,"bucket_min":1761696000,"bucket_max":1761782400,"metric_value":20,"group":[]}
		]}}}`}}
		out, err := run(t, api, "observability", "aggregate", "--product-type", "logs", "--bucket-count", "1")

		require.NoError(t, err)
		assert.NotContains(t, out, "SAMPLED")
		assert.NotContains(t, out, "Totals")
		assert.Contains(t, out, "20\n")
	})

	t.Run("ranks groups by the function and its column", func(t *testing.T) {
		api := &fakeAPI{responses: []string{`{"data":{"metrics":{"buckets":[],"sample_factor":1}}}`}}
		_, err := run(t, api, "observability", "aggregate", "--product-type", "traces", "--function", "p99", "--column", "duration")

		require.NoError(t, err)
		vars := api.requests[0].Variables
		assert.Equal(t, "Traces", vars["product_type"])
		assert.Equal(t, []any{map[string]any{"aggregator": "P99", "column": "duration"}}, vars["expressions"])
		assert.Equal(t, "P99", vars["limit_aggregator"])
		assert.Equal(t, "duration", vars["limit_column"])
	})

	t.Run("adds sampled to the JSON output", func(t *testing.T) {
		api := &fakeAPI{responses: []string{`{"data":{"metrics":{"buckets":[],"bucket_count":1,"sample_factor":10.4}}}`}}
		out, err := run(t, api, "observability", "aggregate", "--product-type", "logs", "--json")

		require.NoError(t, err)
		var result map[string]any
		require.NoError(t, json.Unmarshal([]byte(out), &result))
		assert.Equal(t, true, result["sampled"])
		assert.Equal(t, 10.4, result["sample_factor"])
	})

	for name, tc := range map[string]struct {
		args []string
		err  string
	}{
		"unknown product type":    {[]string{"--product-type", "spans"}, "--product-type must be one of: logs, traces, errors, sessions, metrics, paevents, events"},
		"function without column": {[]string{"--product-type", "traces", "--function", "Avg"}, "--function Avg needs --column. Only Count works without a column"},
		"column with Count":       {[]string{"--product-type", "traces", "--column", "duration"}, "--column is not used by Count. Set --function, for example Avg or CountDistinct"},
		"too many buckets":        {[]string{"--product-type", "logs", "--bucket-count", "301"}, "--bucket-count must be from 1 to 300"},
	} {
		t.Run("rejects "+name, func(t *testing.T) {
			api := &fakeAPI{}
			_, err := run(t, api, append([]string{"observability", "aggregate"}, tc.args...)...)

			require.EqualError(t, err, tc.err)
			assert.Empty(t, api.requests)
		})
	}
}

func TestKeys(t *testing.T) {
	t.Run("lists keys", func(t *testing.T) {
		api := &fakeAPI{responses: []string{`{"data":{"logs_keys":[{"name":"service_name","type":"String","count":1200},{"name":"serviceName","type":"String","count":3}]}}`}}
		out, err := run(t, api, "observability", "keys", "--product-type", "logs", "--search", "service")

		require.NoError(t, err)
		assert.Equal(t, "KEY           TYPE    COUNT\nservice_name  String  1200\nserviceName   String  3\n", out)
		req := api.requests[0]
		assert.Contains(t, req.Query, "logs_keys(")
		assert.Equal(t, "service", req.Variables["query"])
		assert.Equal(t, float64(25), req.Variables["count"])
		assert.NotContains(t, req.Variables, "product_type")
	})

	t.Run("sends the product type for metrics keys", func(t *testing.T) {
		api := &fakeAPI{responses: []string{`{"data":{"keys":[{"name":"http.server.duration","type":"Numeric","count":5}]}}`}}
		_, err := run(t, api, "observability", "keys", "--product-type", "metrics")

		require.NoError(t, err)
		assert.Contains(t, api.requests[0].Query, "keys(product_type: $product_type")
		assert.Equal(t, "Metrics", api.requests[0].Variables["product_type"])
	})

	t.Run("lists the values of one key", func(t *testing.T) {
		api := &fakeAPI{responses: []string{`{"data":{"value_suggestions":[{"value":"production-gonfalon","count":900,"rank":1},{"value":"staging-gonfalon","count":40,"rank":2}]}}`}}
		out, err := run(t, api, "observability", "keys", "--product-type", "logs", "--key", "service_name", "--search", "gonfalon")

		require.NoError(t, err)
		assert.Equal(t, "VALUE                COUNT\nproduction-gonfalon  900\nstaging-gonfalon     40\n", out)
		req := api.requests[0]
		assert.Contains(t, req.Query, "value_suggestions(")
		assert.Equal(t, "service_name", req.Variables["key_name"])
		assert.Equal(t, "Logs", req.Variables["product_type"])
	})

	t.Run("prints JSON with --json", func(t *testing.T) {
		api := &fakeAPI{responses: []string{`{"data":{"logs_keys":[{"name":"level","type":"String","count":7}]}}`}}
		out, err := run(t, api, "observability", "keys", "--product-type", "logs", "--json")

		require.NoError(t, err)
		assert.JSONEq(t, `{"keys":[{"name":"level","type":"String","count":7}]}`, out)
	})
}

func TestProjectResolution(t *testing.T) {
	t.Run("returns an error if the project has no ID", func(t *testing.T) {
		api := &fakeAPI{}
		url := api.start(t)
		_, err := cmd.CallCmd(
			t,
			cmd.APIClients{
				ResourcesClient:     &resources.MockClient{Response: []byte(`{}`)},
				ObservabilityClient: observability.NewClient("test"),
			},
			analytics.NoopClientFn{}.Tracker(),
			[]string{"observability", "logs", "search", "--access-token", "api-x", "--observability-url", url, "--project", "missing"},
		)

		require.EqualError(t, err, `could not find the ID of project "missing"`)
		assert.Empty(t, api.requests)
	})

	t.Run("uses a project ID as it is", func(t *testing.T) {
		api := &fakeAPI{responses: []string{`{"data":{"logs":{"edges":[]}}}`}}
		url := api.start(t)
		_, err := cmd.CallCmd(
			t,
			cmd.APIClients{
				ResourcesClient:     &resources.MockClient{Response: []byte(`{}`)},
				ObservabilityClient: observability.NewClient("test"),
			},
			analytics.NoopClientFn{}.Tracker(),
			[]string{"observability", "logs", "search", "--access-token", "api-x", "--observability-url", url, "--project", projectID},
		)

		require.NoError(t, err)
		assert.Equal(t, projectID, api.requests[0].ProjectHeader)
	})
}
