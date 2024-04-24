package members

import (
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"ldcli/cmd/cliflags"
	"ldcli/internal/analytics"
	"ldcli/internal/members"
)

func NewMembersCmd(analyticsTracker analytics.Tracker, client members.Client) (*cobra.Command, error) {
	cmd := &cobra.Command{
		Use:   "members",
		Short: "Make requests (list, create, etc.) on members",
		Long:  "Make requests (list, create, etc.) on members",
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			if viper.GetBool(cliflags.AnalyticsOptOut) {
				return
			}
			analyticsTracker.SendEvent(
				viper.GetString(cliflags.AccessTokenFlag),
				viper.GetString(cliflags.BaseURIFlag),
				"CLI Command Run",
				analytics.CmdRunEventProperties(cmd, "members"),
			)
		},
	}

	createCmd, err := NewCreateCmd(client)
	if err != nil {
		return nil, err
	}

	inviteCmd, err := NewInviteCmd(client)
	if err != nil {
		return nil, err
	}

	cmd.AddCommand(createCmd)
	cmd.AddCommand(inviteCmd)

	return cmd, nil

}
