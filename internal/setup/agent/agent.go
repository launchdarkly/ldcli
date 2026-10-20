// Package agent runs the setup agent: an OpenCode session that adds LaunchDarkly
// SDK initialization code to the user's project.
//
// The CLI owns the OpenCode process. It starts OpenCode in the project
// directory, sends one task, turns the session events into Events, and stops
// OpenCode when the run ends. OpenCode edits the files. The CLI never gives
// OpenCode a LaunchDarkly credential.
package agent

import "context"

// Task describes the project and what the agent must do in it.
type Task struct {
	// Dir is the project directory that OpenCode edits.
	Dir            string
	SDKID          string
	Language       string
	Framework      string
	EntryPoint     string
	PackageManager string
	// FlagKey is the flag that the new code evaluates.
	FlagKey string
	// SDKInstalled reports that the install step added the SDK package, so the
	// agent does not need to install it.
	SDKInstalled bool
}

// Config is how the agent behaves for one run: the model and the instructions.
type Config struct {
	// ProviderID and ModelID select the OpenCode model. If both are empty,
	// OpenCode uses its own default model.
	ProviderID   string
	ModelID      string
	Instructions string
	// Task is the first user message. If it is empty, the run sends the
	// CLI's own task message from BuildPrompt.
	Task string
}

// ConfigSource gives the agent configuration for a task.
//
// The setup-agent AI Config will be the source when the Go AI SDK has an
// OpenCode handler. Until then, DefaultConfigSource supplies built-in
// instructions.
type ConfigSource interface {
	Resolve(ctx context.Context, task Task) (Config, error)
}

// DefaultConfigSource returns the built-in instructions and no model, so
// OpenCode uses the model that the user configured for it.
type DefaultConfigSource struct{}

func (DefaultConfigSource) Resolve(context.Context, Task) (Config, error) {
	return Config{Instructions: DefaultInstructions}, nil
}

// EventKind identifies what an Event reports.
type EventKind int

const (
	// EventText carries a part of the assistant's reply in Text.
	EventText EventKind = iota
	// EventTool reports a tool call that started, finished, or failed.
	EventTool
	// EventPermission asks the user to allow or reject a tool call.
	EventPermission
	// EventQuestion asks the user to choose an option for each question in
	// Question.
	EventQuestion
	// EventFileEdited reports a file that OpenCode changed, in File.
	EventFileEdited
	// EventUsage reports the model and the tokens of one finished reply.
	EventUsage
	// EventDone reports that the session finished. It is the last event.
	EventDone
	// EventError reports that the run failed, with the reason in Text. It is
	// the last event.
	EventError
)

// Event is one thing that happened in the agent run.
type Event struct {
	Kind       EventKind
	Text       string
	Tool       ToolCall
	Permission PermissionRequest
	Question   QuestionRequest
	File       string
	Usage      Usage
}

// ToolCall is the state of one tool call.
type ToolCall struct {
	ID string
	// Name is the tool, for example "edit" or "bash".
	Name string
	// Status is "running", "completed", or "error".
	Status string
	// Title is OpenCode's short description of the call, if it has one.
	Title string
}

// PermissionRequest is a tool call that waits for the user's answer.
type PermissionRequest struct {
	ID        string
	SessionID string
	// Permission is the permission type, for example "bash" or "edit".
	Permission string
	// Patterns are what the call applies to, for example the command to run.
	Patterns []string
	// legacy marks a request from an OpenCode version that uses the
	// session-scoped reply endpoint.
	legacy bool
}

// QuestionRequest is a set of questions from the agent that waits for the
// user's answers. The agent asks with OpenCode's question tool.
type QuestionRequest struct {
	ID        string
	SessionID string
	Questions []Question
}

// Question is one question with its choices.
type Question struct {
	// Header is a short label, at most 30 characters.
	Header  string
	Text    string
	Options []QuestionOption
	// Multiple reports that the user can choose more than one option.
	Multiple bool
}

// QuestionOption is one choice for a question.
type QuestionOption struct {
	Label       string
	Description string
}

// Usage is the model and the token counts of one finished reply.
type Usage struct {
	ProviderID   string
	ModelID      string
	InputTokens  int
	OutputTokens int
	Cost         float64
}

// Runner starts agent runs.
type Runner interface {
	// Check returns an error that tells the user why the agent cannot run, or
	// nil if it can.
	Check(ctx context.Context) error
	// Start begins a run for the task. The caller must Close the run.
	Start(ctx context.Context, task Task) (Run, error)
}

// Run is one agent run in progress.
type Run interface {
	// Events delivers the run's events. The channel closes after EventDone or
	// EventError.
	Events() <-chan Event
	// Reply answers a permission request.
	Reply(ctx context.Context, req PermissionRequest, allow bool) error
	// Answer replies to a question request with the chosen option labels, one
	// list for each question, in order. A nil answers value dismisses the
	// questions, and the agent continues without answers.
	Answer(ctx context.Context, req QuestionRequest, answers [][]string) error
	// Close stops OpenCode. It is safe to call more than once.
	Close() error
}
