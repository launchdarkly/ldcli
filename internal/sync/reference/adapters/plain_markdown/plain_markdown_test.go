package plain_markdown

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/launchdarkly/ldcli/internal/sync/reference/adapters"
)

func TestAdapterParsesAndRendersRawPrompt(t *testing.T) {
	prompt, err := (Adapter{}).Parse([]byte("\nBe helpful.\n"))

	require.NoError(t, err)
	require.Equal(t, adapters.Prompt{
		Messages: []adapters.Message{{Role: adapters.RoleSystem, Content: "Be helpful."}},
	}, prompt)

	rendered, err := (Adapter{}).Render(prompt)
	require.NoError(t, err)
	require.Equal(t, "Be helpful.\n", string(rendered))
}
