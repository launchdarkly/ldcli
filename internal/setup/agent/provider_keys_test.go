package agent

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProviderKeys_NamesFromTheEnvironmentAndDotEnv(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".env"),
		[]byte("# keys\nexport OPENAI_API_KEY=\"sk-123\"\nGEMINI_API_KEY=\nLAUNCHDARKLY_SDK_KEY=sdk-1\n"), 0o600))

	keys := ProviderKeys(dir, []string{"ANTHROPIC_API_KEY=sk-ant", "MISTRAL_API_KEY=", "PATH=/bin"})

	assert.Equal(t, []string{"ANTHROPIC_API_KEY", "OPENAI_API_KEY"}, keys, "empty values do not count")
}

func TestBuildFacts_HasProjectEnvironmentAndKeyNames(t *testing.T) {
	facts := BuildFacts(Task{
		SDKID: "python-server-sdk", ProjectKey: "experimentation", EnvironmentKey: "production",
		ProviderKeys: []string{"ANTHROPIC_API_KEY"},
	})

	assert.Contains(t, facts, "- LaunchDarkly project: experimentation")
	assert.Contains(t, facts, "- LaunchDarkly environment: production")
	assert.Contains(t, facts, "- Model provider keys that are set: ANTHROPIC_API_KEY (names only)")
	assert.Contains(t, facts, "Do not look for keys")
	assert.Contains(t, BuildFacts(Task{}), "- Model provider keys that are set: none")
}

func TestProviderKeys_BlankQuotedValueIsNotSet(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".env"), []byte("OPENAI_API_KEY=\"   \"\nANTHROPIC_API_KEY=' x '\n"), 0o600))
	assert.Equal(t, []string{"ANTHROPIC_API_KEY"}, ProviderKeys(dir, nil))
}
