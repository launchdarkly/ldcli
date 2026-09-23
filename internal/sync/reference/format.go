package reference

import (
	"fmt"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
	"github.com/launchdarkly/ldcli/internal/sync/reference/adapters"
	"github.com/launchdarkly/ldcli/internal/sync/reference/adapters/plain_markdown"
)

// PlainMarkdown identifies the built-in plain Markdown adapter.
const PlainMarkdown = "plain-markdown"

// ValidateFormat reports whether a reference format has a registered adapter.
func ValidateFormat(format string) error {
	_, err := adapterFor(format)
	return err
}

// Parse converts referenced file content into the common adapter domain.
func Parse(format string, content []byte) (adapters.Prompt, error) {
	adapter, err := adapterFor(format)
	if err != nil {
		return adapters.Prompt{}, err
	}
	return adapter.Parse(content)
}

// ApplyToVariation merges referenced prompt content into a variation's stored metadata.
func ApplyToVariation(format string, content []byte, variation *syncdomain.Variation) (adapters.Prompt, error) {
	prompt, err := Parse(format, content)
	if err != nil {
		return adapters.Prompt{}, err
	}
	if prompt.Mode != "" {
		if !prompt.Mode.Valid() {
			return adapters.Prompt{}, fmt.Errorf("unsupported referenced prompt mode %q", prompt.Mode)
		}
		variation.Mode = syncdomain.VariationMode(prompt.Mode)
	}
	if prompt.Key != "" {
		variation.Key = prompt.Key
	}
	if prompt.Name != "" {
		variation.Name = prompt.Name
	}

	messages := make([]syncdomain.Message, 0, len(prompt.Messages))
	for _, message := range prompt.Messages {
		if !message.Role.Valid() {
			return adapters.Prompt{}, fmt.Errorf("unsupported referenced prompt role %q", message.Role)
		}
		messages = append(messages, syncdomain.Message{Role: string(message.Role), Content: message.Content})
	}
	switch variation.Mode {
	case syncdomain.VariationModeAgent:
		if len(messages) > 1 || len(messages) == 1 && messages[0].Role != string(adapters.RoleSystem) {
			return adapters.Prompt{}, fmt.Errorf("agent variation %q requires one system message from its reference", variation.Key)
		}
		variation.Instructions = ""
		variation.Messages = nil
		if len(messages) == 1 {
			variation.Instructions = messages[0].Content
		}
	case syncdomain.VariationModeCompletion:
		variation.Instructions = ""
		variation.Messages = messages
	default:
		return adapters.Prompt{}, fmt.Errorf("referenced prompt does not specify a supported mode")
	}
	return prompt, nil
}

// Render converts a variation back to the selected external file format.
func Render(format string, variation syncdomain.Variation) ([]byte, error) {
	adapter, err := adapterFor(format)
	if err != nil {
		return nil, err
	}
	prompt := adapters.Prompt{Mode: adapters.Mode(variation.Mode), Key: variation.Key, Name: variation.Name}
	switch variation.Mode {
	case syncdomain.VariationModeAgent:
		if len(variation.Messages) != 0 {
			return nil, fmt.Errorf("agent variation %q cannot be represented because it contains messages", variation.Key)
		}
		if variation.Instructions != "" {
			prompt.Messages = []adapters.Message{{Role: adapters.RoleSystem, Content: variation.Instructions}}
		}
	case syncdomain.VariationModeCompletion:
		for _, message := range variation.Messages {
			role := adapters.Role(message.Role)
			if !role.Valid() {
				return nil, fmt.Errorf("variation %q has unsupported message role %q", variation.Key, message.Role)
			}
			prompt.Messages = append(prompt.Messages, adapters.Message{Role: role, Content: message.Content})
		}
	default:
		return nil, fmt.Errorf("referenced prompt does not support variation mode %q", variation.Mode)
	}
	return adapter.Render(prompt)
}

func adapterFor(format string) (adapters.Adapter, error) {
	switch format {
	case PlainMarkdown:
		return plain_markdown.Adapter{}, nil
	default:
		return nil, fmt.Errorf("unsupported referenced prompt format %q", format)
	}
}
