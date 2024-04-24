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
		PersistentPreRun: func(c *cobra.Command, args []string) {
			analytics.SendCommandRunEvent("members", c, analyticsTracker)
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

	for _, c := range cmd.Commands() {
		c.SetHelpFunc(func(c *cobra.Command, args []string) {
			analytics.SendCommandRunEvent("members", c, analyticsTracker)
			c.Root().Annotations = map[string]string{"help": "true"}
		})
	}

	return cmd, nil

}
