package sync

import (
	"strings"
	"testing"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/launchdarkly/ldcli/cmd/cliflags"
	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
	syncprompt "github.com/launchdarkly/ldcli/internal/sync/prompt"
)

func TestPromptCommandUsesActionSubcommands(t *testing.T) {
	command := NewPromptCmd(nil)

	assert.Equal(t, "prompts", command.Use)
	for _, name := range []string{"watch", "add", "attach", "detach"} {
		child, _, err := command.Find([]string{name})
		require.NoError(t, err)
		assert.Equal(t, name, child.Name())
	}
	for _, name := range []string{"add", "attach-tool", "attach-skill", "detach", "watch"} {
		assert.Nil(t, command.Flags().Lookup(name), "legacy --%s should not exist", name)
	}
	add, _, err := command.Find([]string{"add"})
	require.NoError(t, err)
	assert.Equal(t, "add [project-key/config-key/variation-key...]", add.Use)
}

func TestSyncCommandIncludesExamples(t *testing.T) {
	command := NewSyncCmd(nil, nil)

	assert.Contains(t, command.Example, "ldcli sync all\n")
	assert.Contains(t, command.Example, "ldcli sync all --dry-run")
	assert.Contains(t, command.Example, "ldcli sync tools")
	for _, name := range []string{"all", "prompts", "tools", "skills"} {
		child, _, err := command.Find([]string{name})
		require.NoError(t, err)
		assert.Equal(t, name, child.Name())
	}
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

func TestPromptCommandsRegisterDiffFlag(t *testing.T) {
	command := NewPromptCmd(nil)
	tests := map[string]struct {
		path []string
		help string
	}{
		"prompts": {help: "Show full diffs in the plan"},
		"watch":   {path: []string{"watch"}, help: "Show full diffs in the plan"},
		"add":     {path: []string{"add"}, help: "Show full diffs and file content in the plan"},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			target := command
			if len(test.path) != 0 {
				var err error
				target, _, err = command.Find(test.path)
				require.NoError(t, err)
			}

			flag := target.Flags().Lookup(diffFlag)
			require.NotNil(t, flag)
			assert.Equal(t, test.help, flag.Usage)
		})
	}
}

func TestPromptCommandsRegisterTargetFlags(t *testing.T) {
	command := NewPromptCmd(nil)
	tests := map[string]struct {
		path []string
		help targetFlagHelp
	}{
		"prompts":     {help: filterFlagHelp},
		"watch":       {path: []string{"watch"}, help: filterFlagHelp},
		"add":         {path: []string{"add"}, help: questionFlagHelp},
		"detach":      {path: []string{"detach"}, help: questionFlagHelp},
		"attach tool": {path: []string{"attach", "tool"}, help: targetFlagHelp{project: questionFlagHelp.project}},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			target := command
			if len(test.path) != 0 {
				var err error
				target, _, err = command.Find(test.path)
				require.NoError(t, err)
			}

			usage := func(name string) string {
				if flag := target.Flags().Lookup(name); flag != nil {
					return flag.Usage
				}
				return ""
			}
			assert.Equal(t, test.help, targetFlagHelp{
				project: usage(cliflags.ProjectFlag), configKey: usage(configKeyFlag), key: usage(keyFlag),
			})
			assert.NotContains(t, target.Example, "production/support/default --", "examples use target flags")
		})
	}
}

// Sync flags come only from the command line. The shared ldcli validation
// copies environment variables and configuration values into every flag, so
// validatedArgs must undo that for the sync flags.
func TestValidatedArgsKeepsSyncFlagsFromTheCommandLineOnly(t *testing.T) {
	t.Cleanup(viper.Reset)
	viper.Reset()
	viper.SetEnvPrefix("LD")
	viper.SetEnvKeyReplacer(strings.NewReplacer("-", "_"))
	viper.AutomaticEnv()
	viper.SetConfigType("yaml")
	require.NoError(t, viper.ReadConfig(strings.NewReader("project: from-config\nyes: true\noutput: json\n")))
	t.Setenv("LD_BASE_URI", "https://example.test")
	t.Setenv("LD_CONFLICT", "bogus")
	t.Setenv("LD_KEY", "from-environment")
	t.Setenv("LD_NO_INPUT", "true")
	t.Setenv("LD_RESOLVE", "demo/support/default=local")

	command := NewPromptCmd(nil)
	require.NoError(t, command.ParseFlags([]string{"--dry-run", "--config-key", "support"}))
	require.NoError(t, command.Args(command, nil))

	for name, want := range map[string]string{
		cliflags.ProjectFlag: "", yesFlag: "false", conflictFlag: "", keyFlag: "[]", noInputFlag: "false",
		resolveFlag: "[]", dryRunFlag: "true", configKeyFlag: "support",
	} {
		flag := command.Flags().Lookup(name)
		assert.Equal(t, want, flag.Value.String(), name)
		assert.Equal(t, want != flag.DefValue, flag.Changed, name)
	}
	assert.Equal(t, "https://example.test", viper.GetString(cliflags.BaseURIFlag), "global flags keep environment values")
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
