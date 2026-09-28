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

func TestAdapterNormalizesLineEndings(t *testing.T) {
	prompt, err := (Adapter{}).Parse([]byte("First line.\r\nSecond line.\rThird line.\r\n"))

	require.NoError(t, err)
	require.Equal(t, adapters.Prompt{
		Messages: []adapters.Message{{
			Role:    adapters.RoleSystem,
			Content: "First line.\nSecond line.\nThird line.",
		}},
	}, prompt)

	rendered, err := (Adapter{}).Render(adapters.Prompt{
		Messages: []adapters.Message{{
			Role:    adapters.RoleSystem,
			Content: "First line.\r\nSecond line.\rThird line.",
		}},
	})

	require.NoError(t, err)
	require.Equal(t, "First line.\nSecond line.\nThird line.\n", string(rendered))
}
