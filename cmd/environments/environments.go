package environments

import (
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"ldcli/cmd/cliflags"
	"ldcli/internal/analytics"
	"ldcli/internal/environments"
)

func NewEnvironmentsCmd(
	analyticsTracker analytics.Tracker,
	client environments.Client,
) (*cobra.Command, error) {
	cmd := &cobra.Command{
		Use:   "environments",
		Short: "Make requests (list, create, etc.) on environments",
		Long:  "Make requests (list, create, etc.) on environments",
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			analyticsTracker.SendEvent(
				viper.GetString(cliflags.AccessTokenFlag),
				viper.GetString(cliflags.BaseURIFlag),
				"CLI Command Run",
				analytics.CmdRunEventProperties(cmd, "environments"),
			)
		},
		PersistentPostRun: func(cmd *cobra.Command, args []string) {
			outcome := analytics.SUCCESS

			if _, hasErr := cmd.Annotations["err"]; hasErr {
				outcome = analytics.ERROR
			}
			if cmd.HasHelpSubCommands() {
				outcome = analytics.HELP
			}

			analyticsTracker.SendEvent(
				viper.GetString(cliflags.AccessTokenFlag),
				viper.GetString(cliflags.BaseURIFlag),
				"CLI Command Completed",
				map[string]interface{}{
					"outcome": outcome,
				},
			)
		},
	}

	getCmd, err := NewGetCmd(client)
	if err != nil {
		return nil, err
	}
	cmd.AddCommand(getCmd)

	return cmd, nil
}
