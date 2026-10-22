package sync

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"

	"github.com/launchdarkly/ldcli/cmd/cliflags"
	resourcescmd "github.com/launchdarkly/ldcli/cmd/resources"
	"github.com/launchdarkly/ldcli/cmd/validators"
	"github.com/launchdarkly/ldcli/internal/output"
	"github.com/launchdarkly/ldcli/internal/resources"
	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
	synclink "github.com/launchdarkly/ldcli/internal/sync/link"
	syncprompt "github.com/launchdarkly/ldcli/internal/sync/prompt"
)

const (
	archiveFlag        = "archive"
	configKeyFlag      = "config-key"
	conflictFlag       = "conflict"
	contentFlag        = "content"
	diffFlag           = "diff"
	dryRunFlag         = "dry-run"
	formatFlag         = "format"
	keyFlag            = "key"
	modelConfigKeyFlag = "model-config-key"
	nameFlag           = "name"
	noInputFlag        = "no-input"
	resolveFlag        = "resolve"
	toFlag             = "to"
	yesFlag            = "yes"
)

// NewPromptCmd creates the prompt synchronization command.
func NewPromptCmd(client resources.Client) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "prompts",
		Short: "Synchronize local prompt variations with LaunchDarkly",
		Long: "Bootstrap local prompt variations from LaunchDarkly, add more variations, or synchronize changes using the LaunchDarkly manifest. " +
			"Sync rechecks state before every write, rerun sync after a change. " +
			"Tool and skill files that no prompt variation uses are not in this plan. Use ldcli sync all, ldcli sync tools, or ldcli sync skills to sync them.",
		Example: `  # Preview synchronization changes
  ldcli sync prompts --dry-run

  # Add the first variation without prompting
  ldcli sync prompts add --project production --config-key support --key default --no-input

  # Apply changes in an existing workspace without prompting
  ldcli sync prompts --yes --no-input

  # Sync one config, and prefer local changes when conflicts occur
  ldcli sync prompts --project production --config-key support --yes --conflict=local`,
		Args: validatedArgs(cobra.NoArgs),
		RunE: runCommand(client, syncdomain.ScopePrompts, buildSyncAction),
	}

	addTargetFlags(cmd, filterFlagHelp)
	addApplyFlags(cmd, syncdomain.ScopePrompts, true)
	cmd.AddCommand(
		newWatchCmd(client, syncdomain.ScopePrompts, filterFlagHelp),
		newAddCmd(client),
		newAttachCmd(client),
		newDetachCmd(client),
		newLinkCmd(client),
	)
	cmd.SetUsageTemplate(resourcescmd.SubcommandUsageTemplate())

	return cmd
}

type actionBuilder func(*cobra.Command, []string) (syncprompt.CommandAction, error)

// buildSyncAction reads the flags of a root sync command.
func buildSyncAction(cmd *cobra.Command, _ []string) (syncprompt.CommandAction, error) {
	dryRun, _ := cmd.Flags().GetBool(dryRunFlag)
	return syncprompt.SyncAction{DryRun: dryRun, Filter: targetFilter(cmd)}, nil
}

// runCommand returns the RunE function of a sync command. It runs the action
// that buildAction reads from the flags, in the scope of the command.
func runCommand(client resources.Client, scope syncdomain.Scope, buildAction actionBuilder) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		workingDirectory, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("get working directory: %w", err)
		}
		action, err := buildAction(cmd, args)
		if err != nil {
			return err
		}
		policy, err := conflictPolicy(cmd)
		if err != nil {
			return err
		}
		yes, _ := cmd.Flags().GetBool(yesFlag)
		noInput, _ := cmd.Flags().GetBool(noInputFlag)
		diff, _ := cmd.Flags().GetBool(diffFlag)
		outputKind := cliflags.GetOutputKind(cmd)

		err = syncprompt.NewRunner(client).Run(syncprompt.Options{
			WorkingDirectory: workingDirectory,
			AccessToken:      viper.GetString(cliflags.AccessTokenFlag),
			BaseURI:          viper.GetString(cliflags.BaseURIFlag),
			OutputKind:       outputKind,
			Scope:            scope,
			Action:           action,
			ConflictPolicy:   policy,
			Yes:              yes,
			NoInput:          noInput,
			Diff:             diff,
			Context:          cmd.Context(),
			Input:            cmd.InOrStdin(),
			Output:           cmd.OutOrStdout(),
			ErrorOutput:      cmd.ErrOrStderr(),
		})
		if err != nil {
			return output.NewCmdOutputError(err, outputKind)
		}
		return nil
	}
}

// newWatchCmd creates the watch command of a scope. help is the help text of
// the target flags of the root sync of the scope.
func newWatchCmd(client resources.Client, scope syncdomain.Scope, help targetFlagHelp) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "watch",
		Short: "Synchronize when managed files change",
		Long: "Synchronize each time a managed file changes. " +
			"Watch creates a resource from a file with upsert when the file is saved.",
		Example: fmt.Sprintf(`  # Watch files and confirm each set of changes
  ldcli sync %s watch

  # Apply watched changes without prompting and report unresolved conflicts
  ldcli sync %s watch --yes --no-input`, scope, scope),
		Args: validatedArgs(cobra.NoArgs),
		RunE: runCommand(client, scope, func(cmd *cobra.Command, _ []string) (syncprompt.CommandAction, error) {
			return syncprompt.SyncAction{Watch: true, Filter: targetFilter(cmd)}, nil
		}),
	}
	addTargetFlags(cmd, help)
	addApplyFlags(cmd, scope, false)
	return cmd
}

func newAddCmd(client resources.Client) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "add [project-key/config-key/variation-key...]",
		Short: "Add prompt variations from LaunchDarkly",
		Example: `  # Choose variations interactively
  ldcli sync prompts add

  # Choose variations in one config
  ldcli sync prompts add --project production --config-key support

  # Add variations by key without prompting
  ldcli sync prompts add --project production --config-key support --key default --key concise --no-input`,
		Args: validatedArgs(cobra.MinimumNArgs(0)),
		RunE: runCommand(client, syncdomain.ScopePrompts, func(cmd *cobra.Command, args []string) (syncprompt.CommandAction, error) {
			variations, err := parseVariationSelectors(args)
			if err != nil {
				return nil, err
			}
			dryRun, _ := cmd.Flags().GetBool(dryRunFlag)
			return syncprompt.AddAction{Resources: variations, Filter: targetFilter(cmd), DryRun: dryRun}, nil
		}),
	}
	addTargetFlags(cmd, questionFlagHelp)
	cmd.Flags().Bool(dryRunFlag, false, "Preview local files without creating them")
	cmd.Flags().Bool(diffFlag, false, "Show full diffs and file content in the plan")
	cmd.Flags().Bool(noInputFlag, false, "Fail instead of prompting for missing input")
	return cmd
}

func newAttachCmd(client resources.Client) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "attach",
		Short: "Attach a tool or skill to a synced variation",
		Example: `  # Attach a tool
  ldcli sync prompts attach tool search --to production/support/default --yes

  # Attach a skill
  ldcli sync prompts attach skill summarize --to production/support/default --yes`,
	}
	cmd.AddCommand(
		newAttachKindCmd(client, syncdomain.AttachmentTool),
		newAttachKindCmd(client, syncdomain.AttachmentSkill),
	)
	return cmd
}

func newAttachKindCmd(client resources.Client, kind syncdomain.AttachmentKind) *cobra.Command {
	cmd := &cobra.Command{
		Use:   string(kind) + " [key]",
		Short: "Attach a " + string(kind) + " to a synced variation",
		Example: fmt.Sprintf(`  # Choose a %s and synced variation interactively
  ldcli sync prompts attach %s

  # Attach a %s by key to a synced variation
  ldcli sync prompts attach %s example-key \
    --to production/support/default \
    --yes --no-input`, kind, kind, kind, kind),
		Args: validatedArgs(cobra.MaximumNArgs(1)),
		RunE: runCommand(client, syncdomain.ScopePrompts, func(cmd *cobra.Command, args []string) (syncprompt.CommandAction, error) {
			target, err := optionalVariationFlag(cmd, toFlag)
			if err != nil {
				return nil, err
			}
			key := ""
			if len(args) == 1 {
				key = args[0]
			}
			return syncprompt.AttachAction{Kind: kind, Key: key, ProjectKey: targetFilter(cmd).Project, Target: target}, nil
		}),
	}
	addTargetFlags(cmd, targetFlagHelp{project: questionFlagHelp.project})
	cmd.Flags().String(toFlag, "", "Target variation (project-key/config-key/variation-key)")
	addApplyFlags(cmd, syncdomain.ScopePrompts, false)
	return cmd
}

func newDetachCmd(client resources.Client) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "detach [project-key/config-key/variation-key...]",
		Short: "Stop syncing local prompt variations",
		Long: "Stop syncing local prompt variations. Detach deletes their local files and their sync.lock entries. " +
			"LaunchDarkly keeps them, unless you pass --archive. Detach lists the variations and asks before it deletes files. " +
			"Without a terminal, pass --yes. Tools and skills that the variation used keep syncing on their own.",
		Example: `  # Choose variations interactively
  ldcli sync prompts detach

  # Stop syncing one variation without prompting
  ldcli sync prompts detach --project production --config-key support --key default --yes --no-input

  # Stop syncing one variation and archive it in LaunchDarkly
  ldcli sync prompts detach --project production --config-key support --key default --archive --yes --no-input`,
		Args: validatedArgs(cobra.MinimumNArgs(0)),
		RunE: runCommand(client, syncdomain.ScopePrompts, func(cmd *cobra.Command, args []string) (syncprompt.CommandAction, error) {
			variations, err := parseVariationSelectors(args)
			if err != nil {
				return nil, err
			}
			archive, _ := cmd.Flags().GetBool(archiveFlag)
			return syncprompt.DetachAction{Resources: variations, Filter: targetFilter(cmd), Archive: archive}, nil
		}),
	}
	addTargetFlags(cmd, questionFlagHelp)
	cmd.Flags().Bool(archiveFlag, false, "Also archive the variations in LaunchDarkly")
	cmd.Flags().Bool(yesFlag, false, "Detach, and archive, without confirmation")
	cmd.Flags().Bool(noInputFlag, false, "Fail instead of prompting for missing input")
	return cmd
}

func newLinkCmd(client resources.Client) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "link <file>",
		Short: "Link an external prompt file",
		Example: `  # Link a Markdown file as a new variation
  ldcli sync prompts link prompts/support.md \
    --format plain-markdown \
    --to production/support/default \
    --model-config-key claude \
    --yes --no-input`,
		Args: validatedArgs(cobra.ExactArgs(1)),
		RunE: runCommand(client, syncdomain.ScopePrompts, func(cmd *cobra.Command, args []string) (syncprompt.CommandAction, error) {
			format, _ := cmd.Flags().GetString(formatFlag)
			target, err := linkTarget(cmd)
			if err != nil {
				return nil, err
			}
			return syncprompt.LinkAction{File: args[0], Format: format, ProjectKey: targetFilter(cmd).Project, Target: target}, nil
		}),
	}
	addTargetFlags(cmd, targetFlagHelp{project: questionFlagHelp.project})
	cmd.Flags().String(formatFlag, "", "Format adapter (for example, plain-markdown)")
	cmd.Flags().String(toFlag, "", "New variation (project-key/config-key/variation-key)")
	cmd.Flags().String(modelConfigKeyFlag, "", "Model config key for the new variation")
	cmd.Flags().String(nameFlag, "", "Variation name when absent from the linked file")
	cmd.Flags().String(contentFlag, "", "Prompt content when absent from the linked file")
	_ = cmd.MarkFlagRequired(formatFlag)
	addApplyFlags(cmd, syncdomain.ScopePrompts, false)
	return cmd
}

// addApplyFlags registers the flags of a command that applies a plan. Only
// the prompts scope has --resolve, which names variations.
func addApplyFlags(cmd *cobra.Command, scope syncdomain.Scope, dryRun bool) {
	if dryRun {
		cmd.Flags().Bool(dryRunFlag, false, "Preview synchronization changes without applying them")
	}
	cmd.Flags().Bool(diffFlag, false, "Show full diffs in the plan")
	cmd.Flags().String(conflictFlag, "", "Default conflict resolution: launchdarkly, local, or abort")
	if scope == syncdomain.ScopePrompts {
		cmd.Flags().StringArray(resolveFlag, nil, "Resolve one variation conflict (project-key/config-key/variation-key=choice)")
	}
	cmd.Flags().Bool(yesFlag, false, "Apply changes without confirmation")
	cmd.Flags().Bool(noInputFlag, false, "Fail instead of prompting for missing input")
}

// targetFlagHelp is the help text of the target flags of one command. An
// empty text leaves out its flag.
type targetFlagHelp struct {
	project   string
	configKey string
	key       string
}

var (
	// filterFlagHelp describes target flags that limit a sync.
	filterFlagHelp = targetFlagHelp{
		project:   "Sync only this project",
		configKey: "Sync only this config. Needs --project.",
		key:       "Sync only this variation. Repeat for more variations. Needs --config-key.",
	}
	// questionFlagHelp describes target flags that answer the questions of a command.
	questionFlagHelp = targetFlagHelp{
		project:   "Project key. Skips the project question.",
		configKey: "Config key. Skips the config question. Needs --project.",
		key:       "Variation key. Repeat for more variations. Needs --config-key.",
	}
)

// attachmentFlagHelp describes the target flags of a tool or skill command.
// With filter, the flags limit a sync. Otherwise they answer questions.
func attachmentFlagHelp(kind syncdomain.AttachmentKind, filter bool) targetFlagHelp {
	if filter {
		return targetFlagHelp{
			project: filterFlagHelp.project,
			key:     fmt.Sprintf("Sync only this %s. Repeat for more %ss. Needs --project.", kind, kind),
		}
	}
	return targetFlagHelp{
		project: questionFlagHelp.project,
		key:     fmt.Sprintf("%s key. Repeat for more %ss. Needs --project.", strings.ToUpper(string(kind[:1]))+string(kind[1:]), kind),
	}
}

// addTargetFlags registers --project, --config-key, and --key.
func addTargetFlags(cmd *cobra.Command, help targetFlagHelp) {
	cmd.Flags().String(cliflags.ProjectFlag, "", help.project)
	if help.configKey != "" {
		cmd.Flags().String(configKeyFlag, "", help.configKey)
	}
	if help.key != "" {
		cmd.Flags().StringArray(keyFlag, nil, help.key)
	}
}

// targetFilter reads the target flags of a command. A flag that the command
// does not have is empty.
func targetFilter(cmd *cobra.Command) syncdomain.Filter {
	project, _ := cmd.Flags().GetString(cliflags.ProjectFlag)
	configKey, _ := cmd.Flags().GetString(configKeyFlag)
	keys, _ := cmd.Flags().GetStringArray(keyFlag)
	return syncdomain.Filter{Project: project, ConfigKey: configKey, Keys: keys}
}

// validatedArgs checks the positional arguments and runs the shared ldcli
// validation. That validation also copies each LD_* environment variable and
// ldcli configuration value into the flag with the same name. Sync flags come
// only from the command line, so validatedArgs then resets each sync flag that
// the command line did not set. Global flags, such as --access-token, keep
// their environment and configuration values.
func validatedArgs(validate cobra.PositionalArgs) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		if err := validate(cmd, args); err != nil {
			return err
		}
		syncFlags := cmd.LocalNonPersistentFlags()
		fromCommandLine := make(map[string]bool)
		syncFlags.VisitAll(func(flag *pflag.Flag) { fromCommandLine[flag.Name] = flag.Changed })
		if err := validators.Validate()(cmd, args); err != nil {
			return err
		}
		syncFlags.VisitAll(func(flag *pflag.Flag) {
			if flag.Changed && !fromCommandLine[flag.Name] {
				resetFlag(flag)
			}
		})
		return nil
	}
}

// resetFlag gives a flag its default value and marks it as not set.
func resetFlag(flag *pflag.Flag) {
	if values, ok := flag.Value.(pflag.SliceValue); ok {
		_ = values.Replace(nil)
	} else {
		_ = flag.Value.Set(flag.DefValue)
	}
	flag.Changed = false
}

func parseVariationSelectors(values []string) ([]syncdomain.ResourceID, error) {
	variations := make([]syncdomain.ResourceID, 0, len(values))
	for _, value := range values {
		variation, err := syncdomain.ParseVariationSelector(value)
		if err != nil {
			return nil, err
		}
		variations = append(variations, variation)
	}
	return variations, nil
}

func optionalVariationFlag(cmd *cobra.Command, name string) (*syncdomain.ResourceID, error) {
	value, _ := cmd.Flags().GetString(name)
	if value == "" {
		return nil, nil
	}
	variation, err := syncdomain.ParseVariationSelector(value)
	if err != nil {
		return nil, err
	}
	return &variation, nil
}

func linkTarget(cmd *cobra.Command) (*synclink.Target, error) {
	variation, err := optionalVariationFlag(cmd, toFlag)
	if err != nil {
		return nil, err
	}
	modelConfig, _ := cmd.Flags().GetString(modelConfigKeyFlag)
	name, _ := cmd.Flags().GetString(nameFlag)
	content, _ := cmd.Flags().GetString(contentFlag)
	if variation == nil {
		if modelConfig != "" || name != "" || content != "" {
			return nil, fmt.Errorf("--model-config-key, --name, and --content require --to")
		}
		return nil, nil
	}
	return &synclink.Target{
		Variation:      *variation,
		ModelConfigKey: modelConfig,
		Name:           name,
		Content:        content,
	}, nil
}

func conflictPolicy(cmd *cobra.Command) (syncprompt.ConflictPolicy, error) {
	var policy syncprompt.ConflictPolicy
	if cmd.Flags().Lookup(conflictFlag) == nil {
		return policy, nil
	}
	defaultValue, _ := cmd.Flags().GetString(conflictFlag)
	if defaultValue != "" {
		resolution, err := syncprompt.ParseConflictResolution(defaultValue)
		if err != nil {
			return policy, err
		}
		policy.Default = resolution
	}

	values, _ := cmd.Flags().GetStringArray(resolveFlag)
	if len(values) == 0 {
		return policy, nil
	}
	policy.Overrides = make(map[syncdomain.ResourceID]syncprompt.ConflictResolution, len(values))
	for _, value := range values {
		selector, choice, ok := strings.Cut(value, "=")
		if !ok {
			return policy, fmt.Errorf(
				"invalid --resolve %q; expected project-key/config-key/variation-key=choice",
				value,
			)
		}
		variation, err := syncdomain.ParseVariationSelector(selector)
		if err != nil {
			return policy, err
		}
		if _, duplicate := policy.Overrides[variation]; duplicate {
			return policy, fmt.Errorf("conflict resolution for %q was provided more than once", selector)
		}
		resolution, err := syncprompt.ParseConflictResolution(choice)
		if err != nil {
			return policy, err
		}
		policy.Overrides[variation] = resolution
	}
	return policy, nil
}
