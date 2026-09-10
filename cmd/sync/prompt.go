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
	syncapi "github.com/launchdarkly/ldcli/internal/sync/api"
	syncbootstrap "github.com/launchdarkly/ldcli/internal/sync/bootstrap"
	synclocal "github.com/launchdarkly/ldcli/internal/sync/local"
	syncrepository "github.com/launchdarkly/ldcli/internal/sync/repository"
)

const (
	addFlag   = "add"
	debugFlag = "debug"
)

type bootstrapRunner func(syncbootstrap.Options) error

func NewPromptCmd(client resources.Client) *cobra.Command {
	return newPromptCmd(client, syncbootstrap.Run)
}

func newPromptCmd(client resources.Client, bootstrap bootstrapRunner) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "prompt",
		Short: "Synchronize local prompt variations with LaunchDarkly",
		Long:  "Bootstrap, add, and synchronize local prompt variations with LaunchDarkly.",
		Args: func(cmd *cobra.Command, args []string) error {
			if err := cobra.NoArgs(cmd, args); err != nil {
				return err
			}
			return validators.Validate()(cmd, args)
		},
		RunE: runPrompt(client, bootstrap),
	}

	cmd.Flags().Bool(addFlag, false, "Select additional prompt variations to sync")
	cmd.Flags().Bool(debugFlag, false, "Print request methods, paths, and bodies")
	cmd.SetUsageTemplate(resourcescmd.SubcommandUsageTemplate())

	return cmd
}

func runPrompt(client resources.Client, bootstrap bootstrapRunner) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, _ []string) error {
		cwd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("get working directory: %w", err)
		}

		repo, err := syncrepository.IdentifyRepo(cwd)
		if err != nil {
			return err
		}

		requestClient := client
		debug, _ := cmd.Flags().GetBool(debugFlag)
		if debug {
			requestClient = debugClient{next: client, out: cmd.ErrOrStderr()}
		}

		api := syncapi.NewAPIClient(
			requestClient,
			viper.GetString(cliflags.AccessTokenFlag),
			viper.GetString(cliflags.BaseURIFlag),
		)
		store := synclocal.NewStore(repo.Root)
		storeExists, err := store.Exists()
		if err != nil {
			return err
		}
		add, _ := cmd.Flags().GetBool(addFlag)
		if !storeExists || add {
			err := bootstrap(syncbootstrap.Options{
				API:     api,
				Store:   store,
				Input:   cmd.InOrStdin(),
				Output:  cmd.OutOrStdout(),
				Initial: !storeExists,
			})
			if err != nil {
				return output.NewCmdOutputError(err, cliflags.GetOutputKind(cmd))
			}
			return nil
		}

		localResources, err := synclocal.Compile(os.DirFS(repo.Root))
		if err != nil {
			return err
		}

		_, err = api.Status(
			repo.Identifier,
			localResources,
		)
		if err != nil {
			return output.NewCmdOutputError(err, cliflags.GetOutputKind(cmd))
		}

		return nil
	}
}
