// Package reference converts a variation to and from the external file that a
// linked variation uses. Each file format has one adapter.
package reference

import (
	"fmt"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
	"github.com/launchdarkly/ldcli/internal/sync/reference/adapters"
	"github.com/launchdarkly/ldcli/internal/sync/reference/adapters/plainmarkdown"
)

// PlainMarkdown identifies the built-in plain Markdown format.
const PlainMarkdown = "plain-markdown"

// adapterFor is the registry of external formats. To add a format, implement
// adapters.Adapter and add one case here.
func adapterFor(format string) (adapters.Adapter, error) {
	switch format {
	case PlainMarkdown:
		return plainmarkdown.Adapter{}, nil
	default:
		return nil, fmt.Errorf("unsupported referenced prompt format %q", format)
	}
}

// ValidateFormat reports whether a format has an adapter.
func ValidateFormat(format string) error {
	_, err := adapterFor(format)
	return err
}

// Parse reads the content of an external file.
func Parse(format string, content []byte) (adapters.Prompt, error) {
	adapter, err := adapterFor(format)
	if err != nil {
		return adapters.Prompt{}, err
	}
	return adapter.Parse(content)
}

// ApplyToVariation replaces the prompt content of the variation with the
// content of an external file. If the file stores a mode, key, or name, that
// value also replaces the value in the variation.
func ApplyToVariation(format string, content []byte, variation *syncdomain.Variation) error {
	prompt, err := Parse(format, content)
	if err != nil {
		return err
	}
	if prompt.Mode != "" {
		if !prompt.Mode.Valid() {
			return fmt.Errorf("unsupported referenced prompt mode %q", prompt.Mode)
		}
		variation.Mode = prompt.Mode
	}
	if prompt.Key != "" {
		variation.Key = prompt.Key
	}
	if prompt.Name != "" {
		variation.Name = prompt.Name
	}
	for _, message := range prompt.Messages {
		if !syncdomain.ValidMessageRole(message.Role) {
			return fmt.Errorf("unsupported referenced prompt role %q", message.Role)
		}
	}

	switch variation.Mode {
	case syncdomain.VariationModeAgent:
		if !atMostOneSystemMessage(prompt.Messages) {
			return fmt.Errorf("agent variation %q requires one system message from its reference", variation.Key)
		}
		variation.Instructions = ""
		variation.Messages = nil
		if len(prompt.Messages) == 1 {
			variation.Instructions = prompt.Messages[0].Content
		}
	case syncdomain.VariationModeCompletion:
		variation.Instructions = ""
		variation.Messages = append([]syncdomain.Message{}, prompt.Messages...)
	default:
		return fmt.Errorf("referenced prompt does not specify a supported mode")
	}
	return nil
}

// Render converts the variation to the content of an external file.
func Render(format string, variation syncdomain.Variation) ([]byte, error) {
	adapter, err := adapterFor(format)
	if err != nil {
		return nil, err
	}

	prompt := adapters.Prompt{Mode: variation.Mode, Key: variation.Key, Name: variation.Name}
	switch variation.Mode {
	case syncdomain.VariationModeAgent:
		if len(variation.Messages) != 0 {
			return nil, fmt.Errorf("agent variation %q cannot be represented because it contains messages", variation.Key)
		}
		if variation.Instructions != "" {
			prompt.Messages = []syncdomain.Message{{Role: syncdomain.RoleSystem, Content: variation.Instructions}}
		}
	case syncdomain.VariationModeCompletion:
		for _, message := range variation.Messages {
			if !syncdomain.ValidMessageRole(message.Role) {
				return nil, fmt.Errorf("variation %q has unsupported message role %q", variation.Key, message.Role)
			}
		}
		prompt.Messages = variation.Messages
	default:
		return nil, fmt.Errorf("referenced prompt does not support variation mode %q", variation.Mode)
	}
	return adapter.Render(prompt)
}

// atMostOneSystemMessage reports whether messages is empty or is one system message.
func atMostOneSystemMessage(messages []syncdomain.Message) bool {
	return len(messages) == 0 || len(messages) == 1 && messages[0].Role == syncdomain.RoleSystem
}
