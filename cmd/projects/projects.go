package projects

import (
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"ldcli/cmd/cliflags"
	"ldcli/internal/analytics"
	"ldcli/internal/projects"
)

func NewProjectsCmd(analyticsTracker analytics.Tracker, client projects.Client) (*cobra.Command, error) {
	cmd := &cobra.Command{
		Use:   "projects",
		Short: "Make requests (list, create, etc.) on projects",
		Long:  "Make requests (list, create, etc.) on projects",
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			if viper.GetBool(cliflags.AnalyticsOptOut) {
				return
			}
			analyticsTracker.SendEvent(
				viper.GetString(cliflags.AccessTokenFlag),
				viper.GetString(cliflags.BaseURIFlag),
				"CLI Command Run",
				analytics.CmdRunEventProperties(cmd, "projects"),
			)
		},
	}

	createCmd, err := NewCreateCmd(client)
	if err != nil {
		return nil, err
	}
	listCmd := NewListCmd(client)

	cmd.AddCommand(createCmd)
	cmd.AddCommand(listCmd)

	return cmd, nil
}
