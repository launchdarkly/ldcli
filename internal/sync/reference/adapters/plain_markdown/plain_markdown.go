package plain_markdown

import (
	"fmt"
	"strings"

	"github.com/launchdarkly/ldcli/internal/sync/reference/adapters"
)

// Adapter converts plain Markdown to and from the common prompt representation.
type Adapter struct{}

// Parse reads raw text as one system message. Raw files do not provide mode,
// key, or name metadata.
func (Adapter) Parse(content []byte) (adapters.Prompt, error) {
	body := strings.TrimSpace(string(content))
	prompt := adapters.Prompt{}
	if body != "" {
		prompt.Messages = []adapters.Message{{Role: adapters.RoleSystem, Content: body}}
	}
	return prompt, nil
}

// Render writes a prompt that contains at most one system message.
func (Adapter) Render(prompt adapters.Prompt) ([]byte, error) {
	if len(prompt.Messages) > 1 || len(prompt.Messages) == 1 && prompt.Messages[0].Role != adapters.RoleSystem {
		return nil, fmt.Errorf("plain-markdown supports at most one system message")
	}
	if len(prompt.Messages) == 0 {
		return nil, nil
	}
	body := strings.TrimSpace(prompt.Messages[0].Content)
	if body == "" {
		return nil, nil
	}
	return []byte(body + "\n"), nil
}

var _ adapters.Adapter = Adapter{}
