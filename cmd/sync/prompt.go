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
	syncprompt "github.com/launchdarkly/ldcli/internal/sync/prompt"
)

const dryRunFlag = "dry-run"

func NewPromptCmd(client resources.Client) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "prompt",
		Short: "Synchronize local prompt variations with LaunchDarkly",
		Long:  "Plan synchronization changes for local prompt variations. Use --dry-run to preview changes without creating a plan.",
		Args: func(cmd *cobra.Command, args []string) error {
			if err := cobra.NoArgs(cmd, args); err != nil {
				return err
			}
			return validators.Validate()(cmd, args)
		},
		RunE: runPrompt(client),
	}
	cmd.Flags().Bool(dryRunFlag, false, "Preview synchronization changes without creating a plan")
	cmd.SetUsageTemplate(resourcescmd.SubcommandUsageTemplate())
	return cmd
}

func runPrompt(client resources.Client) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, _ []string) error {
		workingDirectory, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("get working directory: %w", err)
		}
		dryRun, _ := cmd.Flags().GetBool(dryRunFlag)
		outputKind := cliflags.GetOutputKind(cmd)
		err = syncprompt.NewRunner(client).Run(syncprompt.Options{
			WorkingDirectory: workingDirectory,
			AccessToken:      viper.GetString(cliflags.AccessTokenFlag),
			BaseURI:          viper.GetString(cliflags.BaseURIFlag),
			OutputKind:       outputKind,
			DryRun:           dryRun,
			Output:           cmd.OutOrStdout(),
		})
		if err != nil {
			return output.NewCmdOutputError(err, outputKind)
		}
		return nil
	}
}
