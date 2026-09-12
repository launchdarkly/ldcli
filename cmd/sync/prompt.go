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
	syncbootstrap "github.com/launchdarkly/ldcli/internal/sync/bootstrap"
	synclocal "github.com/launchdarkly/ldcli/internal/sync/local"
	syncsource "github.com/launchdarkly/ldcli/internal/sync/source"
)

const addFlag = "add"

type bootstrapRunner func(syncbootstrap.Options) error

func NewPromptCmd(client resources.Client) *cobra.Command {
	return newPromptCmd(client, syncbootstrap.Run)
}

func newPromptCmd(
	client resources.Client,
	bootstrap bootstrapRunner,
) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "prompt",
		Short: "Synchronize local prompt variations with LaunchDarkly",
		Long:  "Bootstrap local prompt variations from LaunchDarkly, add more variations, or preview synchronization changes.",
		Args: func(cmd *cobra.Command, args []string) error {
			if err := cobra.NoArgs(cmd, args); err != nil {
				return err
			}

			return validators.Validate()(cmd, args)
		},
		RunE: runPrompt(client, bootstrap),
	}

	cmd.Flags().Bool(
		addFlag,
		false,
		"Select additional prompt variations from LaunchDarkly",
	)
	cmd.SetUsageTemplate(resourcescmd.SubcommandUsageTemplate())

	return cmd
}

func runPrompt(
	client resources.Client,
	bootstrap bootstrapRunner,
) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, _ []string) error {
		cwd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("get working directory: %w", err)
		}

		resolver := syncsource.NewResolver(config.GetConfigFile())
		root, err := resolver.ResolveRoot(cwd)
		if err != nil {
			return err
		}

		accessToken := viper.GetString(cliflags.AccessTokenFlag)
		baseURI := viper.GetString(cliflags.BaseURIFlag)
		store := synclocal.NewStore(root)

		storeExists, err := store.Exists()
		if err != nil {
			return err
		}
		add, _ := cmd.Flags().GetBool(addFlag)
		if !storeExists || add {
			err := bootstrap(syncbootstrap.Options{
				Catalog: syncapi.NewCatalogClient(
					client,
					accessToken,
					baseURI,
				),
				Store:   store,
				Input:   cmd.InOrStdin(),
				Output:  cmd.OutOrStdout(),
				Initial: !storeExists,
			})
			if err != nil {
				return output.NewCmdOutputError(
					err,
					cliflags.GetOutputKind(cmd),
				)
			}

			return nil
		}

		workspace, err := resolver.Resolve(cwd)
		if err != nil {
			return err
		}

		localResources, err := synclocal.Compile(os.DirFS(workspace.Root))
		if err != nil {
			return err
		}

		plans, err := syncapi.NewClient(client).Plan(
			accessToken,
			baseURI,
			workspace.Source,
			true,
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
