package observability

// These GraphQL operations are the same ones that the hosted LaunchDarkly MCP server
// sends to the observability API.

const logsQuery = `
query GetProjectLogs($project_id: StringID!, $params: QueryInput!, $direction: SortDirection!, $limit: Int) {
  logs(project_id: $project_id, params: $params, direction: $direction, limit: $limit) {
    edges {
      node {
        timestamp
        level
        message
        logAttributes
        source
        environment
        traceID
        spanID
        secureSessionID
        serviceName
        serviceVersion
      }
    }
  }
}`

const tracesQuery = `
query GetProjectTraces($project_id: StringID!, $params: QueryInput!, $direction: SortDirection!, $limit: Int) {
  traces(project_id: $project_id, params: $params, direction: $direction, limit: $limit) {
    edges {
      node {
        timestamp
        traceID
        spanID
        parentSpanID
        secureSessionID
        spanName
        spanKind
        duration
        serviceName
        serviceVersion
        environment
        hasErrors
        traceAttributes
        statusCode
        statusMessage
      }
    }
  }
}`

const aggregationsQuery = `
query GetProjectMetricsBuckets(
  $product_type: ProductType!
  $project_id: StringID!
  $params: QueryInput!
  $bucket_by: String!
  $bucket_count: Int
  $group_by: [String!]!
  $expressions: [MetricExpressionInput!]!
  $limit: Int
  $limit_aggregator: MetricAggregator
  $limit_column: String
) {
  metrics(
    product_type: $product_type
    project_id: $project_id
    params: $params
    bucket_by: $bucket_by
    bucket_count: $bucket_count
    group_by: $group_by
    expressions: $expressions
    limit: $limit
    limit_aggregator: $limit_aggregator
    limit_column: $limit_column
  ) {
    buckets {
      bucket_id
      bucket_min
      bucket_max
      metric_value
      metric_type
      column
      group
    }
    bucket_count
    sample_factor
  }
}`

// keysQueries are the key list operations for each product type. The data field of
// each response has the same name as the operation field.
var keysQueries = map[string]struct{ field, query string }{
	"Logs":     {"logs_keys", keysOperation("GetLogKeys", "logs_keys", false)},
	"Traces":   {"traces_keys", keysOperation("GetTracesKeys", "traces_keys", false)},
	"Sessions": {"sessions_keys", keysOperation("GetSessionsKeys", "sessions_keys", false)},
	"Errors":   {"errors_keys", keysOperation("GetErrorGroupsKeys", "errors_keys", false)},
	"Metrics":  {"keys", keysOperation("GetKeys", "keys", true)},
	"PAEvents": {"keys", keysOperation("GetKeys", "keys", true)},
	"Events":   {"keys", keysOperation("GetKeys", "keys", true)},
}

func keysOperation(name, field string, withProductType bool) string {
	productTypeVariable, productTypeArgument := "", ""
	if withProductType {
		productTypeVariable = "$product_type: ProductType, "
		productTypeArgument = "product_type: $product_type, "
	}

	return `
query ` + name + `(` + productTypeVariable + `$project_id: StringID!, $date_range: DateRangeRequiredInput!, $query: String, $type: KeyType, $count: Int) {
  ` + field + `(` + productTypeArgument + `project_id: $project_id, date_range: $date_range, query: $query, type: $type, count: $count) {
    name
    type
    count
  }
}`
}

const valueSuggestionsQuery = `
query GetValueSuggestions(
  $product_type: ProductType
  $project_id: StringID!
  $key_name: String!
  $date_range: DateRangeRequiredInput!
  $query: String
  $count: Int
) {
  value_suggestions(
    product_type: $product_type
    project_id: $project_id
    key_name: $key_name
    date_range: $date_range
    query: $query
    count: $count
  ) {
    value
    count
    rank
  }
}`

const validateSearchQueryQuery = `
query ValidateSearchQuery($project_id: StringID!, $query: String!) {
  validate_search_query(project_id: $project_id, query: $query)
}`

// productTypes maps the names that users type to the ProductType values of the API.
var productTypes = map[string]string{
	"errors": "Errors", "error": "Errors",
	"traces": "Traces", "trace": "Traces", "requests": "Traces",
	"logs": "Logs", "log": "Logs",
	"sessions": "Sessions", "session": "Sessions",
	"metrics": "Metrics", "metric": "Metrics",
	"paevents": "PAEvents", "paevent": "PAEvents", "pa-events": "PAEvents", "pa_events": "PAEvents",
	"events": "Events", "event": "Events",
}

const productTypeNames = "logs, traces, errors, sessions, metrics, paevents, events"
