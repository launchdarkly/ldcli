package members

import (
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	cmdAnalytics "ldcli/cmd/analytics"
	"ldcli/cmd/cliflags"
	"ldcli/internal/analytics"
	"ldcli/internal/members"
)

func NewMembersCmd(analyticsTracker analytics.Tracker, client members.Client) (*cobra.Command, error) {
	cmd := &cobra.Command{
		Use:   "members",
		Short: "Make requests (list, create, etc.) on members",
		Long:  "Make requests (list, create, etc.) on members",
		PersistentPreRun: func(c *cobra.Command, args []string) {
			analyticsTracker.SendCommandRunEvent(
				"members",
				viper.GetString(cliflags.AccessTokenFlag),
				viper.GetString(cliflags.BaseURIFlag),
				cmdAnalytics.CmdRunEventProperties(c, "projects"),
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
