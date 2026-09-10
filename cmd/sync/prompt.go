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
	syncapi "github.com/launchdarkly/ldcli/internal/sync"
)

const debugFlag = "debug"

func NewPromptCmd(client resources.Client) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "prompt",
		Short: "Check synchronization status for local prompts",
		Long:  "Read local prompt resources and check their synchronization status with LaunchDarkly.",
		Args: func(cmd *cobra.Command, args []string) error {
			if err := cobra.NoArgs(cmd, args); err != nil {
				return err
			}
			return validators.Validate()(cmd, args)
		},
		RunE: runPrompt(client),
	}

	cmd.Flags().Bool(debugFlag, false, "Print the status request method, path, and body")
	cmd.SetUsageTemplate(resourcescmd.SubcommandUsageTemplate())

	return cmd
}

func runPrompt(client resources.Client) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, _ []string) error {
		cwd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("get working directory: %w", err)
		}

		repo, err := syncapi.IdentifyRepo(cwd)
		if err != nil {
			return err
		}

		localResources, err := syncapi.Compile(os.DirFS(repo.Root))
		if err != nil {
			return err
		}

		requestClient := client
		debug, _ := cmd.Flags().GetBool(debugFlag)
		if debug {
			requestClient = debugClient{next: client, out: cmd.ErrOrStderr()}
		}

		_, err = syncapi.NewAPIClient(requestClient).Status(
			viper.GetString(cliflags.AccessTokenFlag),
			viper.GetString(cliflags.BaseURIFlag),
			repo.Identifier,
			localResources,
		)
		if err != nil {
			return output.NewCmdOutputError(err, cliflags.GetOutputKind(cmd))
		}

		return nil
	}
}
