// Package plainmarkdown reads and writes a prompt as one plain Markdown file.
package plainmarkdown

import (
	"errors"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
	"github.com/launchdarkly/ldcli/internal/sync/reference/adapters"
)

// Adapter stores the prompt as one system message. The file has no mode, key,
// or name.
type Adapter struct{}

var _ adapters.Adapter = Adapter{}

// Parse reads the file text as one system message.
func (Adapter) Parse(content []byte) (adapters.Prompt, error) {
	body := syncdomain.NormalizePromptText(string(content))
	if body == "" {
		return adapters.Prompt{}, nil
	}
	return adapters.Prompt{Messages: []syncdomain.Message{{Role: syncdomain.RoleSystem, Content: body}}}, nil
}

// Render writes the one system message of the prompt as the file text.
func (Adapter) Render(prompt adapters.Prompt) ([]byte, error) {
	if len(prompt.Messages) > 1 || len(prompt.Messages) == 1 && prompt.Messages[0].Role != syncdomain.RoleSystem {
		return nil, errors.New("plain-markdown supports at most one system message")
	}
	if len(prompt.Messages) == 0 {
		return nil, nil
	}
	body := syncdomain.NormalizePromptText(prompt.Messages[0].Content)
	if body == "" {
		return nil, nil
	}
	return []byte(body + "\n"), nil
}
