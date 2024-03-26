package members

import (
	"github.com/spf13/cobra"

	"ldcli/internal/members"
)

func NewMembersCmd(client members.Client) (*cobra.Command, error) {
	cmd := &cobra.Command{
		Use:   "members",
		Short: "Make requests (create/invite) on members",
		Long:  "Make requests (create/invite) on members",
	}

	createCmd, err := NewCreateCmd(client)
	if err != nil {
		return nil, err
	}

	cmd.AddCommand(createCmd)

	return cmd, nil

}
