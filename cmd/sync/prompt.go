package sync

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/launchdarkly/ldcli/cmd/cliflags"
	resourcescmd "github.com/launchdarkly/ldcli/cmd/resources"
	"github.com/launchdarkly/ldcli/cmd/validators"
	"github.com/launchdarkly/ldcli/internal/config"
	"github.com/launchdarkly/ldcli/internal/output"
	"github.com/launchdarkly/ldcli/internal/resources"
	syncapi "github.com/launchdarkly/ldcli/internal/sync/api"
	synclocal "github.com/launchdarkly/ldcli/internal/sync/local"
	syncsource "github.com/launchdarkly/ldcli/internal/sync/source"
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

	cmd.Flags().Bool(
		dryRunFlag,
		false,
		"Preview synchronization changes without creating a plan",
	)
	cmd.SetUsageTemplate(resourcescmd.SubcommandUsageTemplate())

	return cmd
}

func runPrompt(client resources.Client) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, _ []string) error {
		cwd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("get working directory: %w", err)
		}

		workspace, err := syncsource.NewResolver(config.GetConfigFile()).Resolve(cwd)
		if err != nil {
			return err
		}

		localResources, err := synclocal.Compile(os.DirFS(workspace.Root))
		if err != nil {
			return err
		}

		dryRun, _ := cmd.Flags().GetBool(dryRunFlag)
		plans, err := syncapi.NewClient(client).Plan(
			viper.GetString(cliflags.AccessTokenFlag),
			viper.GetString(cliflags.BaseURIFlag),
			workspace.Source,
			dryRun,
			localResources,
		)
		if err != nil {
			return output.NewCmdOutputError(err, cliflags.GetOutputKind(cmd))
		}

		return writePlanOutput(
			cmd.OutOrStdout(),
			cliflags.GetOutputKind(cmd),
			plans,
		)
	}
}
