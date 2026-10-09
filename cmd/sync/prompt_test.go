package sync

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
	syncprompt "github.com/launchdarkly/ldcli/internal/sync/prompt"
)

func TestPromptCommandUsesActionSubcommands(t *testing.T) {
	command := NewPromptCmd(nil)

	assert.Equal(t, "prompts", command.Use)
	for _, name := range []string{"watch", "add", "attach", "detach", "link"} {
		child, _, err := command.Find([]string{name})
		require.NoError(t, err)
		assert.Equal(t, name, child.Name())
	}
	for _, name := range []string{"add", "attach-tool", "attach-skill", "detach", "link", "watch"} {
		assert.Nil(t, command.Flags().Lookup(name), "legacy --%s should not exist", name)
	}
	add, _, err := command.Find([]string{"add"})
	require.NoError(t, err)
	assert.Equal(t, "add [project-key/config-key/variation-key...]", add.Use)
	link, _, err := command.Find([]string{"link"})
	require.NoError(t, err)
	assert.NotNil(t, link.Flags().Lookup(modelConfigKeyFlag))
	assert.Nil(t, link.Flags().Lookup("model-config"))
}

func TestSyncCommandIncludesExamples(t *testing.T) {
	command := NewSyncCmd(nil, nil)

	assert.Contains(t, command.Example, "ldcli sync prompts")
}

func TestPromptCommandHelpIncludesExamples(t *testing.T) {
	command := NewPromptCmd(nil)
	tests := map[string][]string{
		"prompts":      {},
		"watch":        {"watch"},
		"add":          {"add"},
		"attach":       {"attach"},
		"attach tool":  {"attach", "tool"},
		"attach skill": {"attach", "skill"},
		"detach":       {"detach"},
		"link":         {"link"},
	}

	for name, path := range tests {
		t.Run(name, func(t *testing.T) {
			target := command
			if len(path) != 0 {
				var err error
				target, _, err = command.Find(path)
				require.NoError(t, err)
			}

			assert.Contains(t, target.Example, "ldcli sync prompts")
		})
	}
}

func TestAttachSubcommandsAcceptOptionalKeyAndTarget(t *testing.T) {
	command := NewPromptCmd(nil)
	tool, _, err := command.Find([]string{"attach", "tool"})
	require.NoError(t, err)
	require.NoError(t, tool.ParseFlags([]string{"--to", "project/config/variation"}))

	target, err := optionalVariationFlag(tool, toFlag)
	require.NoError(t, err)
	require.NotNil(t, target)
	assert.Equal(t, syncdomain.ResourceID{
		Kind: syncdomain.KindVariation, ProjectKey: "project", LookupKey: "config/variation",
	}, *target)
}

func TestParseVariationSelectorsRejectsAmbiguousValues(t *testing.T) {
	for _, selector := range []string{
		"project/config",
		"project/../variation",
		"project/con\\fig/variation",
		"project/config/vari\x00ation",
	} {
		_, err := parseVariationSelectors([]string{selector})
		require.ErrorContains(t, err, "expected project-key/config-key/variation-key")
	}

	variations, err := parseVariationSelectors([]string{"project/config/first", "project/config/second"})
	require.NoError(t, err)
	require.Len(t, variations, 2)
}

func TestConflictPolicyParsesDefaultAndOverrides(t *testing.T) {
	command := NewPromptCmd(nil)
	require.NoError(t, command.ParseFlags([]string{
		"--conflict=launchdarkly",
		"--resolve=project/config/variation=local",
	}))

	policy, err := conflictPolicy(command)

	require.NoError(t, err)
	assert.Equal(t, syncprompt.ConflictUseLaunchDarkly, policy.Default)
	assert.Equal(t, syncprompt.ConflictUseLocal, policy.Overrides[syncdomain.ResourceID{
		Kind: syncdomain.KindVariation, ProjectKey: "project", LookupKey: "config/variation",
	}])
}
