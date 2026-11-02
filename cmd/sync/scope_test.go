package sync

import (
	"slices"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// syncCommandFlags is the exact flag set of every sync command that runs. A
// new command, or a new question without a flag, must change this list.
var syncCommandFlags = map[string][]string{
	"sync all":           {"conflict", "diff", "dry-run", "no-input", "project", "yes"},
	"sync all watch":     {"conflict", "diff", "no-input", "project", "yes"},
	"sync prompts":       {"config-key", "conflict", "diff", "dry-run", "key", "no-input", "project", "resolve", "yes"},
	"sync prompts watch": {"config-key", "conflict", "diff", "key", "no-input", "project", "resolve", "yes"},
	"sync prompts add": {
		"config-key", "content", "diff", "dry-run", "key", "model-config-key", "name", "no-input", "project", "yes",
	},
	"sync prompts attach tool":  {"conflict", "diff", "no-input", "project", "resolve", "to", "yes"},
	"sync prompts attach skill": {"conflict", "diff", "no-input", "project", "resolve", "to", "yes"},
	"sync prompts detach":       {"archive", "config-key", "key", "no-input", "project", "yes"},
	"sync prompts link": {
		"conflict", "content", "diff", "format", "model-config-key", "name", "no-input", "project", "resolve", "to", "yes",
	},
	"sync tools":         {"conflict", "diff", "dry-run", "key", "no-input", "project", "yes"},
	"sync tools watch":   {"conflict", "diff", "key", "no-input", "project", "yes"},
	"sync tools add":     {"description", "diff", "dry-run", "json-schema", "key", "no-input", "project", "yes"},
	"sync tools detach":  {"key", "no-input", "project", "yes"},
	"sync skills":        {"conflict", "diff", "dry-run", "key", "no-input", "project", "yes"},
	"sync skills watch":  {"conflict", "diff", "key", "no-input", "project", "yes"},
	"sync skills add":    {"content", "description", "diff", "dry-run", "key", "name", "no-input", "project", "yes"},
	"sync skills detach": {"key", "no-input", "project", "yes"},
}

func TestSyncCommandsHaveExactFlags(t *testing.T) {
	var found []string
	for _, command := range runnableSyncCommands(NewSyncCmd(nil, nil)) {
		path := command.CommandPath()
		found = append(found, path)
		var flags []string
		command.LocalNonPersistentFlags().VisitAll(func(flag *pflag.Flag) { flags = append(flags, flag.Name) })
		slices.Sort(flags)
		want, listed := syncCommandFlags[path]
		require.True(t, listed, "%s is not in syncCommandFlags", path)
		assert.Equal(t, want, flags, path)
	}
	assert.Len(t, found, len(syncCommandFlags))
}

// No sync flag takes its value from an LD_* environment variable or from the
// ldcli configuration, in any sync command.
func TestSyncCommandsReadSyncFlagsOnlyFromTheCommandLine(t *testing.T) {
	t.Cleanup(viper.Reset)
	viper.Reset()
	viper.SetEnvPrefix("LD")
	viper.SetEnvKeyReplacer(strings.NewReplacer("-", "_"))
	viper.AutomaticEnv()
	viper.SetConfigType("yaml")
	require.NoError(t, viper.ReadConfig(strings.NewReader("project: from-config\nyes: true\noutput: json\n")))
	t.Setenv("LD_BASE_URI", "https://example.test")
	for _, name := range []string{"archive", "diff", "dry-run", "no-input", "yes"} {
		t.Setenv("LD_"+strings.ToUpper(strings.ReplaceAll(name, "-", "_")), "true")
	}
	for _, name := range []string{
		"config-key", "conflict", "content", "description", "format", "json-schema", "key", "model-config-key", "name", "resolve", "to",
	} {
		t.Setenv("LD_"+strings.ToUpper(strings.ReplaceAll(name, "-", "_")), "from-environment")
	}

	for _, command := range runnableSyncCommands(NewSyncCmd(nil, nil)) {
		require.NoError(t, command.ParseFlags(nil))
		var args []string
		if command.Name() == "link" {
			args = []string{"prompt.md"}
		}
		require.NoError(t, command.Args(command, args), command.CommandPath())
		command.LocalNonPersistentFlags().VisitAll(func(flag *pflag.Flag) {
			assert.False(t, flag.Changed, "%s --%s", command.CommandPath(), flag.Name)
		})
	}
}

// An add creates a resource only when the command line sets a create flag. A
// flag set to an empty value still counts.
func TestAddReadsTheCreateInputFromTheFlagsThatAreSet(t *testing.T) {
	root := NewSyncCmd(nil, nil)
	tools, _, err := root.Find([]string{"tools", "add"})
	require.NoError(t, err)
	require.NoError(t, tools.ParseFlags([]string{"--project", "demo"}))
	assert.Nil(t, createInput(tools))

	skills, _, err := root.Find([]string{"skills", "add"})
	require.NoError(t, err)
	require.NoError(t, skills.ParseFlags([]string{"--content", "Ask for a person.", "--description", ""}))
	input := createInput(skills)
	require.NotNil(t, input)
	assert.Equal(t, "Ask for a person.", *input.Content)
	assert.Equal(t, "", *input.Description)
	assert.Nil(t, input.Name)
	assert.Nil(t, input.JSONSchema)

	prompts, _, err := root.Find([]string{"prompts", "add"})
	require.NoError(t, err)
	require.NoError(t, prompts.ParseFlags([]string{"--content", "Escalate.", "--model-config-key", "claude"}))
	input = createInput(prompts)
	require.NotNil(t, input)
	assert.Equal(t, "claude", *input.ModelConfigKey)
	assert.Nil(t, input.Name)
}

func TestSyncCommandScopeNamesTheKindOfTheCommand(t *testing.T) {
	root := NewSyncCmd(nil, nil)
	for path, want := range map[string]string{
		"all watch": "all", "prompts attach tool": "prompts", "tools add": "tools", "skills detach": "skills",
	} {
		command, _, err := root.Find(strings.Fields(path))
		require.NoError(t, err)
		assert.Equal(t, want, commandScope(command), path)
	}
	assert.Empty(t, commandScope(root))
}

// runnableSyncCommands returns each command under root that runs an action.
func runnableSyncCommands(root *cobra.Command) []*cobra.Command {
	var commands []*cobra.Command
	if root.RunE != nil {
		commands = append(commands, root)
	}
	for _, child := range root.Commands() {
		commands = append(commands, runnableSyncCommands(child)...)
	}
	return commands
}
