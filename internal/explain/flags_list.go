package explain

// flagsListExplainer is the curated explainer for `ldcli flags list`. Read
// commands are lower-risk than writes for agents, but list endpoints with
// rich filter/sort grammars (this one in particular) still cause `--help`
// recursion. Exposing the filter argument catalog up front saves several
// turns.
type flagsListExplainer struct{}

func (flagsListExplainer) Explain(_ []string) (CommandExplanation, error) {
	return CommandExplanation{
		Command:     "ldcli flags list",
		Summary:     "List feature flags in a project, optionally filtered and paginated.",
		Description: "Returns the flags in a project. Heavy responses unless you scope with --env, --filter, or --limit. Use --fields to project only the columns you need.",
		Stability:   "stable",
		HTTPMethod:  "GET",
		Path:        "/api/v2/flags/{projectKey}",
		OperationID: "getFeatureFlags",
		Inputs: []InputSpec{
			{Name: "project-key", Location: "flag", Type: "string", Required: true, Description: "The project key."},
			{Name: "env", Location: "flag", Type: "string", Description: "Filter configurations to a single environment. Strongly recommended for agents: cuts payload size by >80% on most accounts."},
			{Name: "tag", Location: "flag", Type: "string", Description: "Filter feature flags by tag."},
			{Name: "limit", Location: "flag", Type: "integer", Default: 20, Description: "Page size."},
			{Name: "offset", Location: "flag", Type: "integer", Default: 0, Description: "Pagination offset; pair with limit."},
			{Name: "summary", Location: "flag", Type: "boolean", Description: "Set to false (\"summary=0\") to include prerequisites, targets, and rules in the response."},
			{
				Name: "filter", Location: "flag", Type: "string",
				Description: "Comma-separated list of filter expressions. Each is `field:value`. Supported fields: query, tags, archived, state (live|deprecated|archived), type (temporary|permanent), maintainerId, maintainerTeamKey, applicationEvaluated, contextKindsEvaluated, codeReferences.min, codeReferences.max, creationDate, evaluated, filterEnv, hasExperiment, sdkAvailability, releasePipeline, guardedRollout. Use `+` to AND tags, `,` to AND filters.",
			},
			{
				Name: "sort", Location: "flag", Type: "string",
				Description: "Sort field. Prefix with `-` for descending. Supported: name, key, creationDate, maintainerId, tags, targetingModifiedDate (requires --env), type.",
			},
			{Name: "compare", Location: "flag", Type: "boolean", Description: "Include before/after comparison metadata for environments that share the same `compareEnv`."},
			{Name: "expand", Location: "flag", Type: "string", Description: "Comma-separated list of fields to expand. Supported: codeReferences, evaluation, migrationSettings."},
		},
		Output: OutputSpec{
			Format:      "json",
			Type:        "object",
			Description: "Paginated list response.",
			Fields: []InputSpec{
				{Name: "items", Type: "array", Description: "The page of flags."},
				{Name: "totalCount", Type: "integer", Description: "Total flags matching the query, across all pages."},
				{Name: "_links", Type: "object", Description: "HATEOAS links: self, first, prev, next, last. Absent links indicate edge of range."},
			},
			Pagination: &PaginationSpec{
				Style:       "offset-limit",
				Description: "Pass --limit and --offset, or follow _links.next.href on each response until absent.",
			},
		},
		Errors: []ErrorSpec{
			{Code: "unauthorized", HTTPStatus: 401, Description: "Access token missing or invalid."},
			{Code: "forbidden", HTTPStatus: 403, Description: "Token lacks `reader` access for this project."},
			{Code: "not_found", HTTPStatus: 404, Description: "Project key not found."},
			{Code: "rate_limited", HTTPStatus: 429, Description: "Slow down; respect `X-Ratelimit-Reset` header.", Remediation: "Reduce concurrent requests or narrow your query."},
		},
		Examples: []ExampleSpec{
			{
				Title:       "Find a flag by partial name match in production",
				Description: "Scope to one environment, project only the keys you need.",
				Args: []string{
					"flags", "list",
					"--project-key", "default",
					"--env", "production",
					"--filter", "query:checkout",
					"--limit", "10",
					"--fields", "key,name,_version",
					"--json",
				},
				Result: "Returns up to 10 flags whose key or name contains \"checkout\", with only the requested fields per item.",
			},
			{
				Title: "List archived flags maintained by a team, sorted by most-recently archived",
				Args: []string{
					"flags", "list",
					"--project-key", "default",
					"--filter", "state:archived,maintainerTeamKey:platform",
					"--sort", "-creationDate",
					"--limit", "50",
					"--json",
				},
			},
			{
				Title:       "Page through all live flags",
				Description: "Use --offset in a loop, or follow _links.next.href.",
				Args: []string{
					"flags", "list",
					"--project-key", "default",
					"--filter", "state:live",
					"--limit", "100",
					"--offset", "0",
					"--json",
				},
				Result: "Increment --offset by 100 until items is shorter than 100 or _links.next is absent.",
			},
		},
		AgentNotes: []string{
			"Always pair --env with --fields when you only need a few columns; the un-scoped response can be megabytes.",
			"Filter syntax is positional and case-sensitive. Quote the entire --filter value when piping through a shell.",
			"This is a read-only command — safe to retry on any 5xx without --dry-run.",
		},
		SeeAlso: []string{
			"ldcli flags get",
			"ldcli flags update",
		},
	}, nil
}
