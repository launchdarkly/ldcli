package prompt_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
)

// linkSupportVariation writes a descriptor that links the variation
// support/<key> to the constant id in agents.py.
func linkSupportVariation(t *testing.T, root, key, constant string) {
	t.Helper()
	writeLocalFile(t, root, "configs/support/"+key+".ref.yaml",
		"formatVersion: 1\nfile: agents.py\nformat: python\nselect: 'constants[id=\""+constant+"\"]'\nmap:\n  prompt: $\n")
}

// ownedByLaunchDarkly makes LaunchDarkly the owner of each variation of the
// config support, through a rule that matches it.
func ownedByLaunchDarkly(api *multiDirectAPI, keys ...string) {
	api.directions = map[string]syncdomain.SyncDirection{}
	api.patterns = map[string]string{}
	for _, key := range keys {
		setting := "variation:support/" + key
		api.directions[setting] = syncdomain.SyncDirectionServerCanonical
		api.patterns[setting] = "support/" + key
	}
}

// sharedSourceMessages counts the full messages, outside the plan table,
// that name the variation that shares the source. A plan row starts with
// blank space and shows only the first clause of its error.
func sharedSourceMessages(output, sharing string) int {
	count := 0
	for _, line := range strings.Split(output, "\n") {
		if !strings.HasPrefix(line, " ") && strings.Contains(line, sharing) {
			count++
		}
	}
	return count
}

func serverVariation(key, instructions string) syncdomain.Variation {
	server := variationWithKey(key, "Support")
	server.Instructions = instructions
	return server
}

// One shared-source problem can cover several pull rows. The refusal says
// it once, not once per row. Here two LaunchDarkly-owned variations pull
// into two constants of one file, and a third variation, owned by both
// sides and in sync, reads the first constant, so the pull would change it.
func TestSharedSourceRefusalCoveringTwoPullsPrintsOnce(t *testing.T) {
	root := initRepository(t)
	original := "FIRST = \"First\"\nSECOND = \"Second\"\n"
	require.NoError(t, os.WriteFile(filepath.Join(root, "agents.py"), []byte(original), 0o644))
	linkSupportVariation(t, root, "first", "FIRST")
	linkSupportVariation(t, root, "second", "SECOND")
	linkSupportVariation(t, root, "reader", "FIRST")
	api := &multiDirectAPI{variations: map[string]syncdomain.Variation{
		"first":  serverVariation("first", "First from LaunchDarkly"),
		"second": serverVariation("second", "Second from LaunchDarkly"),
		"reader": serverVariation("reader", "First"),
	}}
	ownedByLaunchDarkly(api, "first", "second")

	stdout, stderr, err := runPrompt(t, root, api, "--dry-run", "--output", "plaintext")

	require.Error(t, err, "the plan refuses the shared source")
	output := stdout + stderr + err.Error()
	assert.Contains(t, output, `constants[id="FIRST"] in agents.py`, "the refusal names the shared value")
	assert.Equal(t, 1, sharedSourceMessages(output, "production/support/reader"),
		"the shared-source message is printed once, not once per row:\n%s", output)
	content, readErr := os.ReadFile(filepath.Join(root, "agents.py"))
	require.NoError(t, readErr)
	assert.Equal(t, original, string(content), "the source file is unchanged")
}

// Two LaunchDarkly-owned variations that link one constant, with the same new
// LaunchDarkly text for both, are refused as a shared source. The run does
// not report the internal "source updates overlap", and the file is unchanged.
func TestTwoLaunchDarklyOwnedPullsIntoOneValueAreRefusedAsASharedSource(t *testing.T) {
	root := initRepository(t)
	original := "PROMPT = \"Original\"\n"
	require.NoError(t, os.WriteFile(filepath.Join(root, "agents.py"), []byte(original), 0o644))
	linkSupportVariation(t, root, "first", "PROMPT")
	linkSupportVariation(t, root, "second", "PROMPT")
	api := &multiDirectAPI{variations: map[string]syncdomain.Variation{
		"first":  serverVariation("first", "From LaunchDarkly"),
		"second": serverVariation("second", "From LaunchDarkly"),
	}}
	ownedByLaunchDarkly(api, "first", "second")

	stdout, stderr, err := runPrompt(t, root, api, "--dry-run", "--output", "plaintext")

	require.Error(t, err, "the plan refuses the shared value")
	output := stdout + stderr + err.Error()
	assert.NotContains(t, output, "source updates overlap")
	assert.Contains(t, output, "production/support/first", "the refusal names both variations")
	assert.Contains(t, output, "production/support/second", "the refusal names both variations")
	assert.Contains(t, output, `constants[id="PROMPT"] in agents.py`, "the refusal names the shared value")
	assert.Contains(t, output, "its own", "the refusal suggests giving each variation its own value")
	assert.NotContains(t, output, "same owner")
	content, readErr := os.ReadFile(filepath.Join(root, "agents.py"))
	require.NoError(t, readErr)
	assert.Equal(t, original, string(content), "the source file is unchanged")
}
