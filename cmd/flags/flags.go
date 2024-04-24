package flags

import (
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"ldcli/cmd/cliflags"
	"ldcli/internal/analytics"
	"ldcli/internal/flags"
)

func NewFlagsCmd(analyticsTracker analytics.Tracker, client flags.Client) (*cobra.Command, error) {
	cmd := &cobra.Command{
		Use:   "flags",
		Short: "Make requests (list, create, etc.) on flags",
		Long:  "Make requests (list, create, etc.) on flags",
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			analyticsTracker.SendEvent(
				viper.GetString(cliflags.AccessTokenFlag),
				viper.GetString(cliflags.BaseURIFlag),
				viper.GetBool(cliflags.AnalyticsOptOut),
				"CLI Command Run",
				analytics.CmdRunEventProperties(cmd, "flags"),
			)
		},
	}

	createCmd, err := NewCreateCmd(client)
	if err != nil {
		return nil, err
	}
	getCmd, err := NewGetCmd(client)
	if err != nil {
		return nil, err
	}
	updateCmd, err := NewUpdateCmd(client)
	if err != nil {
		return nil, err
	}

	toggleOnUpdateCmd, err := NewToggleOnUpdateCmd(client)
	if err != nil {
		return nil, err
	}
	toggleOffUpdateCmd, err := NewToggleOffUpdateCmd(client)
	if err != nil {
		return nil, err
	}

	cmd.AddCommand(createCmd)
	cmd.AddCommand(getCmd)
	cmd.AddCommand(updateCmd)
	cmd.AddCommand(toggleOnUpdateCmd)
	cmd.AddCommand(toggleOffUpdateCmd)

	return cmd, nil
}
