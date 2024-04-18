package projects

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"ldcli/cmd/cliflags"
	"ldcli/cmd/utils"
	"ldcli/cmd/validators"
	"ldcli/internal/analytics"
	"ldcli/internal/projects"
)

func NewListCmd(analyticsTracker analytics.Tracker, client projects.Client) *cobra.Command {
	cmd := &cobra.Command{
		Args:  validators.Validate(),
		Long:  "Return a list of projects",
		RunE:  runList(client),
		Short: "Return a list of projects",
		Use:   "list",
		PreRun: func(cmd *cobra.Command, args []string) {
			commandRunEvent := utils.CommandRunEventType{
				EventName: "projects",
			}
			commandRunEvent.SendEvents(analyticsTracker, cmd)
		},
	}

	return cmd
}

func runList(client projects.Client) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		response, err := client.List(
			context.Background(),
			viper.GetString(cliflags.AccessTokenFlag),
			viper.GetString(cliflags.BaseURIFlag),
		)
		if err != nil {
			return err
		}

		fmt.Fprintf(cmd.OutOrStdout(), string(response)+"\n")

		return nil
	}
}
