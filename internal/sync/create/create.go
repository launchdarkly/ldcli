// Package create collects and checks a resource that an add command creates
// from flags. It checks the flags before any read, and asks in a terminal for
// each value that the flags do not give.
package create

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/charmbracelet/huh"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
	syncinteractive "github.com/launchdarkly/ldcli/internal/sync/interactive"
)

// Input is the content that the create flags of an add give. A nil field is a
// flag that the command line did not set.
type Input struct {
	// Content is the Markdown of a skill.
	Content *string
	// JSONSchema is the input schema of a tool.
	JSONSchema *string
	// Name is the name of a skill or a variation.
	Name *string
	// Description is the description of a tool or a skill.
	Description *string
	// ModelConfigKey is the model config of a variation.
	ModelConfigKey *string
}

// Check applies the content rules of a create of kind. It reads nothing, so
// an add runs it before any request.
func (input Input) Check(kind syncdomain.Kind) error {
	if err := input.checkContentFlag(kind); err != nil {
		return err
	}
	if kind == syncdomain.KindTool {
		_, err := parseSchema(*input.JSONSchema)
		return err
	}
	switch {
	case strings.TrimSpace(*input.Content) == "":
		return errors.New("--content cannot be blank")
	case input.Name != nil && strings.TrimSpace(*input.Name) == "":
		return errors.New("--name cannot be blank")
	case input.ModelConfigKey != nil && strings.TrimSpace(*input.ModelConfigKey) == "":
		return errors.New("--model-config-key cannot be blank")
	}
	return nil
}

// checkContentFlag makes sure that the flag that starts a create is set when
// another create flag is.
func (input Input) checkContentFlag(kind syncdomain.Kind) error {
	flag, content := "--content", input.Content
	if kind == syncdomain.KindTool {
		flag, content = "--json-schema", input.JSONSchema
	}
	if content != nil {
		return nil
	}

	var passed []string
	for _, option := range []struct {
		flag  string
		value *string
	}{{"--name", input.Name}, {"--description", input.Description}, {"--model-config-key", input.ModelConfigKey}} {
		if option.value != nil {
			passed = append(passed, option.flag)
		}
	}
	switch len(passed) {
	case 0:
		return fmt.Errorf("add creates a %s only when %s is set", kind, flag)
	case 1:
		return fmt.Errorf("%s needs %s. add creates a %s only when %s is set.", passed[0], flag, kind, flag)
	default:
		return fmt.Errorf("%s need %s. add creates a %s only when %s is set.", joinFlags(passed), flag, kind, flag)
	}
}

// Questions are the streams that ask the user for the values that the flags
// do not give. Allowed is false when the command cannot ask, because of
// --no-input or because the streams are not a terminal.
type Questions struct {
	Allowed  bool
	Input    io.Reader
	Output   io.Writer
	Projects syncinteractive.ProjectSearcher
}

// Target returns the identity of the new resource. An empty projectKey or key
// comes from a question. Without questions, the error names every missing
// flag. The bool result is true when the user cancels.
func Target(kind syncdomain.Kind, projectKey, key string, questions Questions) (syncdomain.ResourceID, bool, error) {
	id := syncdomain.ResourceID{Kind: kind, ProjectKey: projectKey, LookupKey: key}
	var missing []string
	if projectKey == "" {
		missing = append(missing, "--project")
	}
	if key == "" {
		missing = append(missing, "--key")
	}
	switch {
	case len(missing) == 0:
		return id, false, nil
	case !questions.Allowed:
		return id, false, fmt.Errorf("%s creation requires %s without interactive input", kind, joinFlags(missing))
	}

	if id.ProjectKey == "" {
		project, canceled, err := syncinteractive.SelectProject(questions.Input, questions.Output, questions.Projects)
		if err != nil || canceled {
			return id, canceled, err
		}
		id.ProjectKey = project.Key
	}
	if id.LookupKey == "" {
		field := huh.NewInput().Title(capitalize(string(kind)) + " key").Value(&id.LookupKey).Validate(keyValidator(kind))
		if canceled, err := syncinteractive.RunForm(questions.Input, questions.Output, field); err != nil || canceled {
			return id, canceled, err
		}
	}
	return id, false, nil
}

// keyValidator returns a form validator that applies the create rules of
// LaunchDarkly to a new key of kind.
func keyValidator(kind syncdomain.Kind) func(string) error {
	return func(key string) error {
		if err := syncdomain.ValidateKey(key); err != nil {
			return fmt.Errorf("%s key %w", kind, err)
		}
		return syncdomain.ValidateCreate(kind, key, "", "")
	}
}

// RequiredValue returns a form validator that rejects a blank value.
func RequiredValue(label string) func(string) error {
	return func(value string) error {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("%s is required", label)
		}
		return nil
	}
}

// joinFlags joins flag names into a list, for example "--project and --key".
func joinFlags(flags []string) string {
	last := len(flags) - 1
	switch {
	case last < 0:
		return ""
	case last == 0:
		return flags[0]
	case last == 1:
		return flags[0] + " and " + flags[1]
	default:
		return strings.Join(flags[:last], ", ") + ", and " + flags[last]
	}
}

// capitalize returns text with an uppercase first letter.
func capitalize(text string) string {
	if text == "" {
		return ""
	}
	return strings.ToUpper(text[:1]) + text[1:]
}
