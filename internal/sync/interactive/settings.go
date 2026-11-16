package interactive

import (
	"context"
	"fmt"
	"io"
	"strings"

	syncconsole "github.com/launchdarkly/ldcli/internal/sync/console"
)

// SettingsUnavailable describes a command that cannot record ownership
// because some projects have no sync settings.
type SettingsUnavailable struct {
	Context context.Context
	Input   io.Reader
	// Prompt is the stream that the question goes to and that must be a
	// terminal. If it is nil, ErrorOutput is used.
	Prompt io.Writer
	// Output gets the cancel line.
	Output      io.Writer
	ErrorOutput io.Writer
	NoInput     bool
	Yes         bool
	// IsTerminal reports whether the streams are a terminal. If it is nil,
	// StreamsAreTerminal is used.
	IsTerminal func(io.Reader, io.Writer) bool
	// ProjectKeys are the projects without sync settings.
	ProjectKeys []string
	// Unrecorded names what LaunchDarkly cannot record, for example
	// "ownership of the detached variations".
	Unrecorded string
	// Question asks whether to go on without recording ownership.
	Question string
	// Verb names the command in the cancel line, for example "Detach".
	Verb string
}

// SettingsUnavailableWarning tells the user that LaunchDarkly could not
// record what unrecorded names in the projects, because they have no sync
// settings.
func SettingsUnavailableWarning(unrecorded string, projectKeys []string) string {
	return fmt.Sprintf(
		"Warning: LaunchDarkly could not record %s in project %s, because the project has no sync settings.",
		unrecorded, strings.Join(projectKeys, ", "),
	)
}

// ConfirmWithoutSettings warns on the error output that LaunchDarkly cannot
// record ownership in the projects, and reports whether to go on. With Yes,
// it goes on. When the prompt stream is a terminal, it asks the question
// there and writes "<Verb> canceled." to the output on a no. Without a
// terminal, it returns an error that suggests --yes.
func ConfirmWithoutSettings(settings SettingsUnavailable) (bool, error) {
	errorOutput := settings.ErrorOutput
	if errorOutput == nil {
		errorOutput = io.Discard
	}
	_ = syncconsole.New(errorOutput).Line(SettingsUnavailableWarning(settings.Unrecorded, settings.ProjectKeys))
	if settings.Yes {
		return true, nil
	}
	isTerminal := settings.IsTerminal
	if isTerminal == nil {
		isTerminal = StreamsAreTerminal
	}
	prompt := settings.Prompt
	if prompt == nil {
		prompt = errorOutput
	}
	interactive := !settings.NoInput && isTerminal(settings.Input, prompt)
	confirmed, err := Confirm(settings.Context, settings.Input, prompt, interactive, settings.Question)
	if err == nil && !confirmed {
		_ = syncconsole.New(settings.Output).Printf("%s canceled.\n", settings.Verb)
	}
	return confirmed, err
}
