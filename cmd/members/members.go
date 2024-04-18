package members

import (
	"github.com/spf13/cobra"

	"ldcli/internal/analytics"
	"ldcli/internal/members"
)

func NewMembersCmd(analyticsTracker analytics.Tracker, client members.Client) (*cobra.Command, error) {
	cmd := &cobra.Command{
		Use:   "members",
		Short: "Make requests (list, create, etc.) on members",
		Long:  "Make requests (list, create, etc.) on members",
	}

	createCmd, err := NewCreateCmd(analyticsTracker, client)
	if err != nil {
		return nil, err
	}

	inviteCmd, err := NewInviteCmd(analyticsTracker, client)
	if err != nil {
		return nil, err
	}

	cmd.AddCommand(createCmd)
	cmd.AddCommand(inviteCmd)

	return cmd, nil

}
