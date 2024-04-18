package flags

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"ldcli/cmd/cliflags"
	"ldcli/cmd/utils"
	"ldcli/cmd/validators"
	"ldcli/internal/analytics"
	"ldcli/internal/flags"
)

func NewCreateCmd(analyticsTracker analytics.Tracker, client flags.Client) (*cobra.Command, error) {
	cmd := &cobra.Command{
		Args:  validators.Validate(),
		Long:  "Create a new flag",
		RunE:  runCreate(analyticsTracker, client),
		Short: "Create a new flag",
		Use:   "create",
	}

	cmd.Flags().StringP(cliflags.DataFlag, "d", "", "Input data in JSON")
	err := cmd.MarkFlagRequired(cliflags.DataFlag)
	if err != nil {
		return nil, err
	}
	err = viper.BindPFlag(cliflags.DataFlag, cmd.Flags().Lookup(cliflags.DataFlag))
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

	return cmd, nil
}

type inputData struct {
	Name string `json:"name"`
	Key  string `json:"key"`
}

func runCreate(analyticsTracker analytics.Tracker, client flags.Client) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		// rebind flags used in other subcommands
		_ = viper.BindPFlag(cliflags.DataFlag, cmd.Flags().Lookup(cliflags.DataFlag))
		_ = viper.BindPFlag(cliflags.ProjectFlag, cmd.Flags().Lookup(cliflags.ProjectFlag))

		var data inputData
		err := json.Unmarshal([]byte(viper.GetString(cliflags.DataFlag)), &data)
		if err != nil {
			return err
		}

		response, err := client.Create(
			context.Background(),
			viper.GetString(cliflags.AccessTokenFlag),
			viper.GetString(cliflags.BaseURIFlag),
			data.Name,
			data.Key,
			viper.GetString(cliflags.ProjectFlag),
		)
		if err != nil {
			return err
		}

		analyticsTracker.SendEvent(
			viper.GetString(cliflags.AccessTokenFlag),
			viper.GetString(cliflags.BaseURIFlag),
			"CLI Command Run",
			utils.BuildCommandRunProperties("flags", cmd.CalledAs(), []string{cliflags.DataFlag, cliflags.ProjectFlag}),
		)

		fmt.Fprintf(cmd.OutOrStdout(), string(response)+"\n")

		return nil
	}
}
