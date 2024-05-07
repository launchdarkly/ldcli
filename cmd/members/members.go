package members

import (
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	cmdAnalytics "ldcli/cmd/analytics"
	"ldcli/cmd/cliflags"
	"ldcli/internal/analytics"
	"ldcli/internal/members"
)

func NewMembersCmd(analyticsTrackerFn analytics.TrackerFn, client members.Client) (*cobra.Command, error) {
	cmd := &cobra.Command{
		Use:   "members",
		Short: "Make requests (list, create, etc.) on members",
		Long:  "Make requests (list, create, etc.) on members",
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			analyticsTrackerFn(
				viper.GetString(cliflags.AccessTokenFlag),
				viper.GetString(cliflags.BaseURIFlag),
				viper.GetBool(cliflags.AnalyticsOptOut),
			).SendCommandRunEvent(cmdAnalytics.CmdRunEventProperties(cmd, "members", nil))
		},
	}

	inviteCmd, err := NewInviteCmd(client)
	if err != nil {
		return nil, err
	}

	cmd.AddCommand(inviteCmd)

	return cmd, nil

}
