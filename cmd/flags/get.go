package flags

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"ldcli/cmd/cliflags"
	"ldcli/cmd/utils"
	"ldcli/cmd/validators"
	"ldcli/internal/analytics"
	"ldcli/internal/flags"
)

func NewGetCmd(analyticsTracker analytics.Tracker, client flags.Client) (*cobra.Command, error) {
	cmd := &cobra.Command{
		Args:  validators.Validate(),
		Long:  "Get a flag to check its details",
		RunE:  runGet(analyticsTracker, client),
		Short: "Get a flag",
		Use:   "get",
	}

	cmd.Flags().String(cliflags.FlagFlag, "", "Flag key")
	err := cmd.MarkFlagRequired(cliflags.FlagFlag)
	if err != nil {
		return nil, err
	}
	err = viper.BindPFlag(cliflags.FlagFlag, cmd.Flags().Lookup(cliflags.FlagFlag))
	if err != nil {
		return nil, err
	}

	cmd.Flags().String(cliflags.ProjectFlag, "", "Project key")
	err = cmd.MarkFlagRequired(cliflags.ProjectFlag)
	if err != nil {
		return nil, err
	}
	err = viper.BindPFlag(cliflags.ProjectFlag, cmd.Flags().Lookup(cliflags.ProjectFlag))
	if err != nil {
		return nil, err
	}

	cmd.Flags().String(cliflags.EnvironmentFlag, "", "Environment key")
	err = cmd.MarkFlagRequired(cliflags.EnvironmentFlag)
	if err != nil {
		return nil, err
	}
	err = viper.BindPFlag(cliflags.EnvironmentFlag, cmd.Flags().Lookup(cliflags.EnvironmentFlag))
	if err != nil {
		return nil, err
	}

	return cmd, nil
}

func runGet(analyticsTracker analytics.Tracker, client flags.Client) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		// rebind flags used in other subcommands
		_ = viper.BindPFlag(cliflags.FlagFlag, cmd.Flags().Lookup(cliflags.FlagFlag))
		_ = viper.BindPFlag(cliflags.ProjectFlag, cmd.Flags().Lookup(cliflags.ProjectFlag))
		_ = viper.BindPFlag(cliflags.EnvironmentFlag, cmd.Flags().Lookup(cliflags.EnvironmentFlag))

		response, err := client.Get(
			context.Background(),
			viper.GetString(cliflags.AccessTokenFlag),
			viper.GetString(cliflags.BaseURIFlag),
			viper.GetString(cliflags.FlagFlag),
			viper.GetString(cliflags.ProjectFlag),
			viper.GetString(cliflags.EnvironmentFlag),
		)
		if err != nil {
			return err
		}

		analyticsTracker.SendEvent(
			viper.GetString(cliflags.AccessTokenFlag),
			viper.GetString(cliflags.BaseURIFlag),
			"CLI Command Run",
			utils.BuildCommandRunProperties("flags", cmd.CalledAs(), []string{cliflags.FlagFlag, cliflags.ProjectFlag, cliflags.EnvironmentFlag}),
		)

		fmt.Fprintf(cmd.OutOrStdout(), string(response)+"\n")

		return nil
	}
}
