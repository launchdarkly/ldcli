package sync

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/launchdarkly/ldcli/cmd/cliflags"
	resourcescmd "github.com/launchdarkly/ldcli/cmd/resources"
	"github.com/launchdarkly/ldcli/cmd/validators"
	"github.com/launchdarkly/ldcli/internal/output"
	"github.com/launchdarkly/ldcli/internal/resources"
	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
	syncprompt "github.com/launchdarkly/ldcli/internal/sync/prompt"
)

const (
	addFlag         = "add"
	attachSkillFlag = "attach-skill"
	attachToolFlag  = "attach-tool"
	detachFlag      = "detach"
	dryRunFlag      = "dry-run"
	formatFlag      = "format"
	linkFlag        = "link"
	projectFlag     = "project"
	variationFlag   = "variation"
	watchFlag       = "watch"
	yesFlag         = "yes"

	interactiveAttachment = "__interactive__"
)

// NewPromptCmd creates the prompt synchronization command.
func NewPromptCmd(client resources.Client) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "prompt",
		Short: "Synchronize local prompt variations with LaunchDarkly",
		Long: "Bootstrap local prompt variations from LaunchDarkly, add more variations, or synchronize changes using the LaunchDarkly manifest. " +
			"Sync rechecks state before every write, rerun sync after a change.",
		Args: func(cmd *cobra.Command, args []string) error {
			if err := cobra.NoArgs(cmd, args); err != nil {
				return err
			}
			return validators.Validate()(cmd, args)
		},
		RunE: runPrompt(client),
	}

	cmd.Flags().Bool(addFlag, false, "Select additional prompt variations from LaunchDarkly")
	cmd.Flags().String(attachSkillFlag, "", "Attach the latest skill version by key, or select interactively")
	cmd.Flags().Lookup(attachSkillFlag).NoOptDefVal = interactiveAttachment
	cmd.Flags().String(attachToolFlag, "", "Attach the latest tool version by key, or select interactively")
	cmd.Flags().Lookup(attachToolFlag).NoOptDefVal = interactiveAttachment
	cmd.Flags().Bool(detachFlag, false, "Select local resources to stop syncing")
	cmd.Flags().Bool(dryRunFlag, false, "Preview synchronization changes without applying them")
	cmd.Flags().String(linkFlag, "", "Link an external prompt file")
	cmd.Flags().String(formatFlag, "", "Format adapter for --link (for example, plain-markdown)")
	cmd.Flags().String(projectFlag, "", "Project key for an attachment operation")
	cmd.Flags().String(variationFlag, "", "Config and variation key for an attachment operation (config/variation)")
	cmd.Flags().Bool(watchFlag, false, "Sync when managed or referenced files change")
	cmd.Flags().Bool(yesFlag, false, "Apply synchronization changes without interactive confirmation")
	cmd.SetUsageTemplate(resourcescmd.SubcommandUsageTemplate())

	return cmd
}

func runPrompt(client resources.Client) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, _ []string) error {
		workingDirectory, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("get working directory: %w", err)
		}

		add, _ := cmd.Flags().GetBool(addFlag)
		attachment, err := attachmentRequest(cmd)
		if err != nil {
			return err
		}
		detach, _ := cmd.Flags().GetBool(detachFlag)
		dryRun, _ := cmd.Flags().GetBool(dryRunFlag)
		format, _ := cmd.Flags().GetString(formatFlag)
		link, _ := cmd.Flags().GetString(linkFlag)
		watch, _ := cmd.Flags().GetBool(watchFlag)
		yes, _ := cmd.Flags().GetBool(yesFlag)
		outputKind := cliflags.GetOutputKind(cmd)

		err = syncprompt.NewRunner(client).Run(syncprompt.Options{
			WorkingDirectory: workingDirectory,
			AccessToken:      viper.GetString(cliflags.AccessTokenFlag),
			BaseURI:          viper.GetString(cliflags.BaseURIFlag),
			OutputKind:       outputKind,
			Add:              add,
			Attachment:       attachment,
			Detach:           detach,
			DryRun:           dryRun,
			Format:           format,
			Link:             link,
			Watch:            watch,
			Yes:              yes,
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

func attachmentRequest(cmd *cobra.Command) (*syncprompt.AttachmentRequest, error) {
	attachSkill := cmd.Flags().Changed(attachSkillFlag)
	attachTool := cmd.Flags().Changed(attachToolFlag)
	project, _ := cmd.Flags().GetString(projectFlag)
	variation, _ := cmd.Flags().GetString(variationFlag)

	if attachSkill && attachTool {
		return nil, fmt.Errorf("--attach-skill and --attach-tool cannot be combined")
	}
	if !attachSkill && !attachTool {
		if project != "" || variation != "" {
			return nil, fmt.Errorf("--project and --variation require --attach-skill or --attach-tool")
		}
		return nil, nil
	}

	kind, flagName := syncdomain.AttachmentSkill, attachSkillFlag
	if attachTool {
		kind, flagName = syncdomain.AttachmentTool, attachToolFlag
	}
	key, _ := cmd.Flags().GetString(flagName)
	if key == interactiveAttachment {
		key = ""
	}
	return &syncprompt.AttachmentRequest{Kind: kind, Key: key, ProjectKey: project, Variation: variation}, nil
}
