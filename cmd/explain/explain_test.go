package explain_test

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	explaincmd "github.com/launchdarkly/ldcli/cmd/explain"
	"github.com/launchdarkly/ldcli/internal/explain"
)

// runExplain wires up an explain command against the default registry, runs
// it with the given args, and returns stdout. It intentionally constructs
// the command in isolation (without the full root command) so we can test
// the subcommand behavior independent of root flag plumbing.
func runExplain(t *testing.T, args ...string) (string, error) {
	t.Helper()
	cmd := explaincmd.NewExplainCmd(explain.DefaultRegistry())
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), err
}

func TestExplain_JSONShape_FlagsUpdate(t *testing.T) {
	out, err := runExplain(t, "flags", "update")
	require.NoError(t, err)

	var parsed map[string]any
	require.NoError(t, json.Unmarshal([]byte(out), &parsed), "explain output must be valid JSON")

	assert.Equal(t, "ldcli flags update", parsed["command"])
	assert.Equal(t, "PATCH", parsed["httpMethod"])
	assert.Equal(t, "patchFeatureFlag", parsed["operationId"])

	inputs, ok := parsed["inputs"].([]any)
	require.True(t, ok, "inputs must be present and a list")
	assert.NotEmpty(t, inputs)

	// The whole point of this command is that the semantic-patch instruction
	// catalog is right there in the JSON. Sanity-check that the catalog made
	// it into the payload.
	body, _ := json.Marshal(parsed)
	for _, kind := range []string{"addRule", "removeRule", "addTargets", "updateName"} {
		assert.Contains(t, string(body), kind, "instruction kind %q should appear in the JSON", kind)
	}

	examples, ok := parsed["examples"].([]any)
	require.True(t, ok)
	assert.GreaterOrEqual(t, len(examples), 1, "must include at least one curated example")
}

func TestExplain_JSONShape_FlagsList(t *testing.T) {
	out, err := runExplain(t, "flags", "list")
	require.NoError(t, err)

	var parsed map[string]any
	require.NoError(t, json.Unmarshal([]byte(out), &parsed))

	assert.Equal(t, "ldcli flags list", parsed["command"])
	assert.Equal(t, "GET", parsed["httpMethod"])

	output, ok := parsed["output"].(map[string]any)
	require.True(t, ok)
	pagination, ok := output["pagination"].(map[string]any)
	require.True(t, ok, "flags list must document pagination semantics")
	assert.Equal(t, "offset-limit", pagination["style"])
}

func TestExplain_Markdown(t *testing.T) {
	out, err := runExplain(t, "flags", "update", "--markdown")
	require.NoError(t, err)

	assert.True(t, strings.HasPrefix(out, "# ldcli flags update"), "markdown should start with the command heading")
	assert.Contains(t, out, "## Inputs")
	assert.Contains(t, out, "## Examples")
	assert.Contains(t, out, "addRule")
}

func TestExplain_UnknownCommand(t *testing.T) {
	_, err := runExplain(t, "no-such-command")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no explanation available")
	assert.Contains(t, err.Error(), "no-such-command")
}

func TestExplain_NoArgs(t *testing.T) {
	_, err := runExplain(t)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "command path")
}

func TestExplain_UnsupportedFormat(t *testing.T) {
	_, err := runExplain(t, "flags", "update", "--format", "yaml")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported --format")
}

func TestExplain_List(t *testing.T) {
	out, err := runExplain(t, "--list")
	require.NoError(t, err)
	assert.Contains(t, out, "ldcli flags update")
	assert.Contains(t, out, "ldcli flags list")
}

// TestExplain_AcceptsLDCliPrefix verifies that the user can paste the full
// command path including "ldcli" without confusing the resolver.
func TestExplain_AcceptsLDCliPrefix(t *testing.T) {
	out, err := runExplain(t, "ldcli", "flags", "update")
	require.NoError(t, err)
	assert.Contains(t, out, `"command": "ldcli flags update"`)
}

// TestExplain_AcceptsSpaceJoinedArg verifies that a single quoted argument
// like `ldcli explain "flags update"` works the same as separate args.
func TestExplain_AcceptsSpaceJoinedArg(t *testing.T) {
	out, err := runExplain(t, "flags update")
	require.NoError(t, err)
	assert.Contains(t, out, `"command": "ldcli flags update"`)
}
