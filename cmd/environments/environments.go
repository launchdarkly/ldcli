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
	}

	getCmd, err := NewGetCmd(client)
	if err != nil {
		return nil, err
	}
	cmd.AddCommand(getCmd)

	cmd.PersistentPreRun = func(cmd *cobra.Command, args []string) {
		analyticsTracker.SendEvent(
			viper.GetString(cliflags.AccessTokenFlag),
			viper.GetString(cliflags.BaseURIFlag),
			"CLI Command Run",
			analytics.CmdRunEventProperties(cmd, "environments"),
		)
	}

	return cmd, nil
}
