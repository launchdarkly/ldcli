package sync

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
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
	conflictFlag       = "conflict"
	contentFlag        = "content"
	dryRunFlag         = "dry-run"
	formatFlag         = "format"
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
			"Sync rechecks state before every write, rerun sync after a change.",
		Example: `  # Preview synchronization changes
  ldcli sync prompts --dry-run

  # Add the first variation without prompting
  ldcli sync prompts add production/support/default --no-input

  # Apply changes in an existing workspace without prompting
  ldcli sync prompts --yes --no-input

  # Prefer local changes when conflicts occur
  ldcli sync prompts --yes --conflict=local`,
		Args: func(cmd *cobra.Command, args []string) error {
			if err := cobra.NoArgs(cmd, args); err != nil {
				return err
			}
			return validators.Validate()(cmd, args)
		},
		RunE: runPrompt(client, func(cmd *cobra.Command, _ []string) (syncprompt.CommandAction, error) {
			dryRun, _ := cmd.Flags().GetBool(dryRunFlag)
			return syncprompt.SyncAction{DryRun: dryRun}, nil
		}),
	}

	addApplyFlags(cmd, true)
	cmd.AddCommand(
		newWatchCmd(client),
		newAddCmd(client),
		newAttachCmd(client),
		newDetachCmd(client),
		newLinkCmd(client),
	)
	cmd.SetUsageTemplate(resourcescmd.SubcommandUsageTemplate())

	return cmd
}

type actionBuilder func(*cobra.Command, []string) (syncprompt.CommandAction, error)

func runPrompt(client resources.Client, buildAction actionBuilder) func(*cobra.Command, []string) error {
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
		outputKind := cliflags.GetOutputKind(cmd)

		err = syncprompt.NewRunner(client).Run(syncprompt.Options{
			WorkingDirectory: workingDirectory,
			AccessToken:      viper.GetString(cliflags.AccessTokenFlag),
			BaseURI:          viper.GetString(cliflags.BaseURIFlag),
			OutputKind:       outputKind,
			Action:           action,
			ConflictPolicy:   policy,
			Yes:              yes,
			NoInput:          noInput,
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

func newWatchCmd(client resources.Client) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "watch",
		Short: "Synchronize when managed files change",
		Example: `  # Watch files and confirm each set of changes
  ldcli sync prompts watch

  # Apply watched changes without prompting and report unresolved conflicts
  ldcli sync prompts watch --yes --no-input`,
		Args: validatedArgs(cobra.NoArgs),
		RunE: runPrompt(client, func(*cobra.Command, []string) (syncprompt.CommandAction, error) {
			return syncprompt.SyncAction{Watch: true}, nil
		}),
	}
	addApplyFlags(cmd, false)
	return cmd
}

func newAddCmd(client resources.Client) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "add [project-key/config-key/variation-key...]",
		Short: "Add prompt variations from LaunchDarkly",
		Example: `  # Choose variations interactively
  ldcli sync prompts add

  # Add one variation by its full selector
  ldcli sync prompts add production/support/default

  # Add multiple variations without prompting
  ldcli sync prompts add production/support/default production/chat/concise --no-input`,
		Args: validatedArgs(cobra.MinimumNArgs(0)),
		RunE: runPrompt(client, func(cmd *cobra.Command, args []string) (syncprompt.CommandAction, error) {
			variations, err := parseVariationSelectors(args)
			if err != nil {
				return nil, err
			}
			dryRun, _ := cmd.Flags().GetBool(dryRunFlag)
			return syncprompt.AddAction{Variations: variations, DryRun: dryRun}, nil
		}),
	}
	cmd.Flags().Bool(dryRunFlag, false, "Preview local files without creating them")
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
		RunE: runPrompt(client, func(cmd *cobra.Command, args []string) (syncprompt.CommandAction, error) {
			target, err := optionalVariationFlag(cmd, toFlag)
			if err != nil {
				return nil, err
			}
			key := ""
			if len(args) == 1 {
				key = args[0]
			}
			return syncprompt.AttachAction{Kind: kind, Key: key, Target: target}, nil
		}),
	}
	cmd.Flags().String(toFlag, "", "Target variation (project-key/config-key/variation-key)")
	addApplyFlags(cmd, false)
	return cmd
}

func newDetachCmd(client resources.Client) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "detach [project-key/config-key/variation-key...]",
		Short: "Stop syncing local prompt variations",
		Example: `  # Choose variations interactively
  ldcli sync prompts detach

  # Stop syncing one variation
  ldcli sync prompts detach production/support/default

  # Stop syncing one variation and archive it in LaunchDarkly
  ldcli sync prompts detach production/support/default --archive --yes --no-input`,
		Args: validatedArgs(cobra.MinimumNArgs(0)),
		RunE: runPrompt(client, func(cmd *cobra.Command, args []string) (syncprompt.CommandAction, error) {
			variations, err := parseVariationSelectors(args)
			if err != nil {
				return nil, err
			}
			archive, _ := cmd.Flags().GetBool(archiveFlag)
			return syncprompt.DetachAction{Variations: variations, Archive: archive}, nil
		}),
	}
	cmd.Flags().Bool(archiveFlag, false, "Also archive the variations in LaunchDarkly")
	cmd.Flags().Bool(yesFlag, false, "Archive without confirmation")
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
		RunE: runPrompt(client, func(cmd *cobra.Command, args []string) (syncprompt.CommandAction, error) {
			format, _ := cmd.Flags().GetString(formatFlag)
			target, err := linkTarget(cmd)
			if err != nil {
				return nil, err
			}
			return syncprompt.LinkAction{File: args[0], Format: format, Target: target}, nil
		}),
	}
	cmd.Flags().String(formatFlag, "", "Format adapter (for example, plain-markdown)")
	cmd.Flags().String(toFlag, "", "New variation (project-key/config-key/variation-key)")
	cmd.Flags().String(modelConfigKeyFlag, "", "Model config key for the new variation")
	cmd.Flags().String(nameFlag, "", "Variation name when absent from the linked file")
	cmd.Flags().String(contentFlag, "", "Prompt content when absent from the linked file")
	_ = cmd.MarkFlagRequired(formatFlag)
	addApplyFlags(cmd, false)
	return cmd
}

func addApplyFlags(cmd *cobra.Command, dryRun bool) {
	if dryRun {
		cmd.Flags().Bool(dryRunFlag, false, "Preview synchronization changes without applying them")
	}
	cmd.Flags().String(conflictFlag, "", "Default conflict resolution: launchdarkly, local, or abort")
	cmd.Flags().StringArray(resolveFlag, nil, "Resolve one variation conflict (project-key/config-key/variation-key=choice)")
	cmd.Flags().Bool(yesFlag, false, "Apply changes without confirmation")
	cmd.Flags().Bool(noInputFlag, false, "Fail instead of prompting for missing input")
}

func validatedArgs(validate cobra.PositionalArgs) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		if err := validate(cmd, args); err != nil {
			return err
		}
		return validators.Validate()(cmd, args)
	}
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
