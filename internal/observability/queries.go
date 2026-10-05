// Code in this file mirrors the observability GraphQL operations used by the LaunchDarkly MCP
// server (launchdarkly/gram-functions src/lib/o11y/queries.ts). Keep them in sync.

package observability

const (
	LogsQuery = `query GetProjectLogs(
  $project_id: StringID!
  $params: QueryInput!
  $direction: SortDirection!
  $limit: Int
) {
  logs(
    project_id: $project_id
    params: $params
    direction: $direction
    limit: $limit
  ) {
    edges {
      cursor
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
    pageInfo {
      startCursor
      endCursor
      hasNextPage
      hasPreviousPage
    }
  }
}`

	MetricsQuery = `query GetProjectMetricsBuckets(
  $product_type: ProductType!
  $project_id: StringID!
  $params: QueryInput!
  $bucket_by: String!
  $bucket_count: Int
  $group_by: [String!]!
  $expressions: [MetricExpressionInput!]!
  $limit: Int
  $limit_aggregator: MetricAggregator
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

	TracesQuery = `query GetProjectTraces(
  $project_id: StringID!
  $params: QueryInput!
  $direction: SortDirection!
  $limit: Int
) {
  traces(
    project_id: $project_id
    params: $params
    direction: $direction
    limit: $limit
  ) {
    edges {
      cursor
      node {
        timestamp
        traceID
        spanID
        parentSpanID
        projectID
        secureSessionID
        traceState
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
    pageInfo {
      startCursor
      endCursor
      hasNextPage
      hasPreviousPage
    }
  }
}`

	ErrorGroupsQuery = `query GetProjectErrorGroups(
  $project_id: StringID!
  $params: QueryInput!
  $count: Int!
  $page: Int
) {
  error_groups(
    project_id: $project_id
    params: $params
    count: $count
    page: $page
  ) {
    error_groups {
      created_at
      updated_at
      id
      event
      state
      snoozed_until
      structured_stack_trace {
        fileName
        lineNumber
        functionName
        columnNumber
      }
      mapped_stack_trace
      stack_trace
      type
      environments
      project_id
      is_public
      serviceName
      secure_id
    }
    totalCount
  }
}`

	SessionsQuery = `query GetProjectSessions(
  $project_id: StringID!
  $params: QueryInput!
  $count: Int!
  $sort_field: String
  $sort_desc: Boolean!
  $page: Int
) {
  sessions(
    project_id: $project_id
    params: $params
    count: $count
    sort_field: $sort_field
    sort_desc: $sort_desc
    page: $page
  ) {
    sessions {
      id
      created_at
      client_id
      identifier
      fingerprint
      user_properties
      user_object
      fields {
        name
        value
      }
      has_rage_clicks
      has_errors
      length
      city
      state
      country
      postal
      os_name
      os_version
      browser_name
      browser_version
      environment
      app_version
      first_time
      viewed
      starred
      processed
      excluded
      excluded_reason
      payload_updated_at
      direct_download_url
      enable_strict_privacy
      privacy_setting
      enable_recording_network_contents
      within_billing_quota
      event_counts
      chunked
      object_storage_enabled
      payload_size
      last_user_interaction_time
      active_length
      secure_id
    }
    totalCount
  }
}`

	TimelineIndicatorEventsQuery = `query GetTimelineIndicatorEvents($session_secure_id: String!) {
  timeline_indicator_events(session_secure_id: $session_secure_id) {
    session_secure_id
    timestamp
    sid
    data
    type
  }
}`

	FlagEvaluationsQuery = `query GetFlagEvaluations(
  $project_id: StringID!
  $params: QueryInput!
  $direction: SortDirection!
  $limit: Int
) {
  traces(
    project_id: $project_id
    params: $params
    direction: $direction
    limit: $limit
  ) {
    edges {
      cursor
      node {
        events {
          timestamp
          name
          attributes
        }
      }
    }
    pageInfo {
      startCursor
      endCursor
      hasNextPage
      hasPreviousPage
    }
  }
}`

	EventChunksQuery = `query GetEventChunks($secure_id: String!) {
  event_chunks(secure_id: $secure_id) {
    session_id
    chunk_index
    timestamp
  }
}`

	EventChunkURLQuery = `query GetEventChunkURL($secure_id: String!, $index: Int!) {
  event_chunk_url(secure_id: $secure_id, index: $index)
}`

	TimestampEventChunkURLQuery = `query GetTimestampEventChunkURL($secure_id: String!, $timestamp: Timestamp!) {
  timestamp_event_chunk_url(secure_id: $secure_id, timestamp: $timestamp)
}`

	ListVisualizationsQuery = `query GetVisualizations(
  $project_id: StringID!
  $input: String!
  $count: Int!
  $offset: Int!
) {
  visualizations(
    project_id: $project_id
    input: $input
    count: $count
    offset: $offset
  ) {
    count
    results {
      id
      name
      updatedAt
      graphs {
        id
        title
        type
        productType
      }
    }
  }
}`

	GetVisualizationQuery = `query GetVisualization($id: ID!) {
  visualization(id: $id) {
    id
    name
    projectId
    timePreset
    graphs {
      id
      type
      title
      description
      productType
      query
      groupByKeys
      bucketByKey
      bucketCount
      display
      expressions {
        aggregator
        column
      }
    }
  }
}`

	UpsertVisualizationMutation = `mutation UpsertVisualization($visualization: VisualizationInput!) {
  upsertVisualization(visualization: $visualization)
}`

	UpsertGraphMutation = `mutation UpsertGraph($graph: GraphInput!) {
  upsertGraph(graph: $graph) {
    id
    title
    type
  }
}`

	DeleteVisualizationMutation = `mutation DeleteVisualization($id: ID!) {
  deleteVisualization(id: $id)
}`

	ListAlertsQuery = `query GetAlerts($project_id: StringID!) {
  alerts(project_id: $project_id) {
    id
    updated_at
    name
    product_type
    function_type
    function_column
    query
    group_by_keys
    disabled
    threshold_value
    threshold_window
    threshold_cooldown
    threshold_type
    threshold_condition
    auto_investigation_enabled
    investigation_cooldown
    permission_mode
    investigation_repositories
    destinations {
      id
      destination_type
      type_id
      type_name
    }
  }
}`

	GetAlertQuery = `query GetAlert($id: ID!) {
  alert(id: $id) {
    id
    project_id
    updated_at
    metric_id
    name
    message_content
    product_type
    function_type
    population_function_type
    population_query
    population_window
    function_column
    query
    group_by_key
    group_by_keys
    disabled
    high_cardinality
    sql
    evaluation_delay_seconds
    threshold_value
    warn_threshold_value
    threshold_window
    threshold_cooldown
    threshold_type
    threshold_condition
    no_data_behavior
    send_resolved_alert
    hide_graph
    auto_investigation_enabled
    investigation_cooldown
    permission_mode
    investigation_repositories
    investigation_prompt
    investigation_conversation_id
    investigation_started_at
    destinations {
      id
      destination_type
      type_id
      type_name
    }
  }
}`

	CreateAlertMutation = `mutation CreateAlert(
  $project_id: StringID!
  $name: String!
  $message_content: String
  $product_type: ProductType!
  $function_type: MetricAggregator!
  $function_column: String
  $query: String
  $group_by_keys: [String!]
  $threshold_value: Float
  $threshold_window: Int
  $threshold_cooldown: Int
  $threshold_type: ThresholdType
  $threshold_condition: ThresholdCondition
  $destinations: [AlertDestinationInput!]!
  $sql: String
  $evaluation_delay_seconds: Int
  $auto_investigation_enabled: Boolean
  $investigation_cooldown: Int
  $permission_mode: String
  $investigation_repositories: StringArray
  $investigation_prompt: String
) {
  createAlert(
    project_id: $project_id
    name: $name
    message_content: $message_content
    product_type: $product_type
    function_type: $function_type
    function_column: $function_column
    query: $query
    group_by_keys: $group_by_keys
    threshold_value: $threshold_value
    threshold_window: $threshold_window
    threshold_cooldown: $threshold_cooldown
    threshold_type: $threshold_type
    threshold_condition: $threshold_condition
    destinations: $destinations
    sql: $sql
    evaluation_delay_seconds: $evaluation_delay_seconds
    auto_investigation_enabled: $auto_investigation_enabled
    investigation_cooldown: $investigation_cooldown
    permission_mode: $permission_mode
    investigation_repositories: $investigation_repositories
    investigation_prompt: $investigation_prompt
  ) {
    id
    name
    product_type
    function_type
    threshold_value
    threshold_window
    threshold_type
    threshold_condition
    auto_investigation_enabled
    investigation_cooldown
    permission_mode
    investigation_repositories
  }
}`

	UpdateAlertMutation = `mutation UpdateAlert(
  $project_id: StringID!
  $alert_id: ID!
  $name: String
  $message_content: String
  $product_type: ProductType
  $function_type: MetricAggregator
  $population_function_type: MetricAggregator
  $population_query: String
  $population_window: Int
  $function_column: String
  $query: String
  $group_by_key: String
  $group_by_keys: [String!]
  $threshold_value: Float
  $warn_threshold_value: Float
  $threshold_window: Int
  $threshold_cooldown: Int
  $threshold_type: ThresholdType
  $threshold_condition: ThresholdCondition
  $no_data_behavior: NoDataBehavior
  $send_resolved_alert: Boolean
  $hide_graph: Boolean
  $destinations: [AlertDestinationInput!]
  $sql: String
  $auto_investigation_enabled: Boolean
  $investigation_cooldown: Int
  $permission_mode: String
  $investigation_repositories: StringArray
  $investigation_prompt: String
  $evaluation_delay_seconds: Int
) {
  updateAlert(
    project_id: $project_id
    alert_id: $alert_id
    name: $name
    message_content: $message_content
    product_type: $product_type
    function_type: $function_type
    population_function_type: $population_function_type
    population_query: $population_query
    population_window: $population_window
    function_column: $function_column
    query: $query
    group_by_key: $group_by_key
    group_by_keys: $group_by_keys
    threshold_value: $threshold_value
    warn_threshold_value: $warn_threshold_value
    threshold_window: $threshold_window
    threshold_cooldown: $threshold_cooldown
    threshold_type: $threshold_type
    threshold_condition: $threshold_condition
    no_data_behavior: $no_data_behavior
    send_resolved_alert: $send_resolved_alert
    hide_graph: $hide_graph
    destinations: $destinations
    sql: $sql
    auto_investigation_enabled: $auto_investigation_enabled
    investigation_cooldown: $investigation_cooldown
    permission_mode: $permission_mode
    investigation_repositories: $investigation_repositories
    investigation_prompt: $investigation_prompt
    evaluation_delay_seconds: $evaluation_delay_seconds
  ) {
    id
    name
    product_type
    function_type
    query
    threshold_value
    threshold_window
    threshold_type
    threshold_condition
    disabled
    auto_investigation_enabled
    investigation_cooldown
    permission_mode
    investigation_repositories
    investigation_prompt
  }
}`

	UpdateAlertDisabledMutation = `mutation UpdateAlertDisabled(
  $project_id: StringID!
  $alert_id: ID!
  $disabled: Boolean!
) {
  updateAlertDisabled(
    project_id: $project_id
    alert_id: $alert_id
    disabled: $disabled
  )
}`

	DeleteAlertMutation = `mutation DeleteAlert($project_id: StringID!, $alert_id: ID!) {
  deleteAlert(project_id: $project_id, alert_id: $alert_id)
}`

	AlertFiringHistoryQuery = `query AlertFiringHistory(
  $alert_id: ID!
  $start_date: Timestamp!
  $end_date: Timestamp!
  $page: Int
  $count: Int
) {
  alerting_alert_state_changes(
    alert_id: $alert_id
    start_date: $start_date
    end_date: $end_date
    page: $page
    count: $count
  ) {
    totalCount
    alertStateChanges {
      id
      timestamp
      state
      groupByKey
      investigationConversationId
    }
  }
}`

	LastAlertStateChangesQuery = `query LastAlertStateChanges($alert_id: ID!) {
  last_alert_state_changes(alert_id: $alert_id) {
    id
    timestamp
    state
    groupByKey
    investigationConversationId
  }
}`

	LogsKeysQuery = `query GetLogKeys(
  $project_id: StringID!
  $date_range: DateRangeRequiredInput!
  $query: String
  $type: KeyType
  $count: Int
) {
  logs_keys(
    project_id: $project_id
    date_range: $date_range
    query: $query
    type: $type
    count: $count
  ) {
    name
    type
  }
}`

	TracesKeysQuery = `query GetTracesKeys(
  $project_id: StringID!
  $date_range: DateRangeRequiredInput!
  $query: String
  $type: KeyType
  $count: Int
) {
  traces_keys(
    project_id: $project_id
    date_range: $date_range
    query: $query
    type: $type
    count: $count
  ) {
    name
    type
  }
}`

	SessionsKeysQuery = `query GetSessionsKeys(
  $project_id: StringID!
  $date_range: DateRangeRequiredInput!
  $query: String
  $type: KeyType
  $count: Int
) {
  sessions_keys(
    project_id: $project_id
    date_range: $date_range
    query: $query
    type: $type
    count: $count
  ) {
    name
    type
  }
}`

	ErrorGroupsKeysQuery = `query GetErrorGroupsKeys(
  $project_id: StringID!
  $date_range: DateRangeRequiredInput!
  $query: String
  $type: KeyType
) {
  errors_keys(
    project_id: $project_id
    date_range: $date_range
    query: $query
    type: $type
  ) {
    name
    type
  }
}`

	KeysQuery = `query GetKeys(
  $product_type: ProductType
  $project_id: StringID!
  $date_range: DateRangeRequiredInput!
  $query: String
  $type: KeyType
  $count: Int
) {
  keys(
    product_type: $product_type
    project_id: $project_id
    date_range: $date_range
    query: $query
    type: $type
    count: $count
  ) {
    name
    type
  }
}`

	ServiceMapQuery = `query GetServiceMap(
  $project_id: StringID!
  $date_range: DateRangeRequiredInput!
) {
  serviceMap(
    project_id: $project_id
    date_range: $date_range
  ) {
    source
    target
  }
}`
)
