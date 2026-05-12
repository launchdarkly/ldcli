// Package explain provides structured, machine-readable schemas for ldcli
// commands. It is consumed by the `ldcli explain` subcommand and is intended
// for LLM agents that need to construct payloads for write commands without
// chained `--help` invocations.
//
// The package defines two layers:
//
//   - A set of plain Go types (CommandExplanation, InputSpec, OutputSpec,
//     ExampleSpec, ErrorSpec) that describe a command's schema in a way that
//     serializes cleanly to JSON.
//
//   - An Explainer interface implemented by per-command "providers". A
//     command resolves an Explainer in one of three ways, in order of
//     preference:
//
//     1. Hand-rolled commands register a curated provider (the highest-fidelity
//     and the source of truth for examples).
//     2. Auto-generated resource commands fall back to an OpenAPIExplainer,
//     which reads the operation schema directly from ld-openapi.json.
//     3. Anything else falls back to a best-effort summary derived from the
//     Cobra flag tree (FlagTreeExplainer).
//
// This first cut wires (1) for `flags update` and `flags list` and stubs (2)
// and (3). See docs/agent-explain.md for the larger design.
package explain

// CommandExplanation is the top-level structured description of a single
// ldcli command. It is intentionally flat and JSON-friendly so that agents can
// pull what they need without traversing nested $ref chains.
type CommandExplanation struct {
	// Command is the full command path, e.g. "ldcli flags update".
	Command string `json:"command"`

	// Summary is a one-line description of what the command does.
	Summary string `json:"summary"`

	// Description is a longer-form description; may include markdown.
	Description string `json:"description,omitempty"`

	// Stability is one of "stable", "beta", "experimental". Defaults to
	// "stable" when omitted.
	Stability string `json:"stability,omitempty"`

	// HTTPMethod and Path describe the underlying REST operation for commands
	// that map 1:1 to the LaunchDarkly API. Omitted for purely local commands.
	HTTPMethod string `json:"httpMethod,omitempty"`
	Path       string `json:"path,omitempty"`

	// OperationID is the OpenAPI operationId for auto-generated commands.
	// Useful when an agent wants to cross-reference the LaunchDarkly REST API
	// docs.
	OperationID string `json:"operationId,omitempty"`

	// Inputs is the list of flags, positional args, and request body fields
	// that the command accepts.
	Inputs []InputSpec `json:"inputs"`

	// Output describes the success-case response shape.
	Output OutputSpec `json:"output"`

	// Errors enumerates the structured error shapes the command can return.
	// This is a curated catalog, not an exhaustive list of every HTTP status.
	Errors []ErrorSpec `json:"errors,omitempty"`

	// Examples is a curated list of agent-friendly examples. Order is
	// significant: the first example should be the most representative.
	Examples []ExampleSpec `json:"examples,omitempty"`

	// AgentNotes contains tips that are specifically useful for LLM agents
	// (e.g. "always set --json", "this is idempotent"). Free-form prose.
	AgentNotes []string `json:"agentNotes,omitempty"`

	// SeeAlso lists related command paths the agent may want to call instead
	// of or alongside this one.
	SeeAlso []string `json:"seeAlso,omitempty"`
}

// InputSpec describes a single input to a command: a flag, a positional
// argument, or a field of the request body.
type InputSpec struct {
	// Name is the user-visible name. For flags this is the long flag name
	// (without the leading "--"). For body fields this is the JSON key, which
	// may include a dotted path (e.g. "instructions[].kind").
	Name string `json:"name"`

	// Location tells the agent where this input lives in the invocation.
	// One of: "flag", "arg", "body", "env".
	Location string `json:"location"`

	// Type is the JSON-schema-flavored type, e.g. "string", "integer",
	// "boolean", "array", "object", "oneOf".
	Type string `json:"type"`

	// Description is human-readable help text.
	Description string `json:"description,omitempty"`

	// Required indicates whether this input must be provided.
	Required bool `json:"required,omitempty"`

	// Default is the default value, if any. Encoded as the value itself
	// (string, number, bool); omit when there is no default.
	Default interface{} `json:"default,omitempty"`

	// Enum is the set of permitted values, if the type is constrained.
	Enum []string `json:"enum,omitempty"`

	// Fields is set when Type is "object" or "array" of objects: it lists the
	// nested fields. This is how we expose semantic-patch instruction shapes.
	Fields []InputSpec `json:"fields,omitempty"`

	// OneOf is set when this input has multiple shapes (e.g. an `addRule`
	// instruction takes either a `variationId` or a percentage rollout).
	OneOf []InputSpec `json:"oneOf,omitempty"`
}

// OutputSpec describes the success-case output of a command.
type OutputSpec struct {
	// Format is the wire format ("json", "plaintext", "table", "markdown").
	// For agents this should typically be "json".
	Format string `json:"format"`

	// Type is the top-level JSON type ("object", "array").
	Type string `json:"type,omitempty"`

	// Description is a human-readable description of the response.
	Description string `json:"description,omitempty"`

	// Fields enumerates the notable top-level fields of the response. This is
	// curated: not every field is listed, just the ones agents typically need.
	Fields []InputSpec `json:"fields,omitempty"`

	// Pagination indicates how the agent should page through results, if any.
	Pagination *PaginationSpec `json:"pagination,omitempty"`
}

// PaginationSpec describes pagination semantics for list endpoints.
type PaginationSpec struct {
	Style       string `json:"style"` // "offset-limit", "cursor", or "links"
	Description string `json:"description,omitempty"`
}

// ExampleSpec is a curated example: an agent should be able to copy the args
// and run it (after substituting placeholders).
type ExampleSpec struct {
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`

	// Args is the argv to pass to ldcli, e.g.
	// ["flags", "update", "--project-key", "default", ...].
	Args []string `json:"args"`

	// Body is the JSON body to pass via --data, if any. Captured as a string
	// (already JSON-encoded) so it round-trips faithfully.
	Body string `json:"body,omitempty"`

	// Result is a short description of what success looks like.
	Result string `json:"result,omitempty"`
}

// ErrorSpec describes one error shape an agent should expect.
type ErrorSpec struct {
	Code        string `json:"code"`
	HTTPStatus  int    `json:"httpStatus,omitempty"`
	Description string `json:"description,omitempty"`
	// Remediation is short, action-oriented guidance for the agent.
	Remediation string `json:"remediation,omitempty"`
}

// Explainer is implemented by anything that can produce a CommandExplanation
// for a single ldcli command. The CommandPath is the full path including the
// leading "ldcli" segment.
type Explainer interface {
	Explain(commandPath []string) (CommandExplanation, error)
}
