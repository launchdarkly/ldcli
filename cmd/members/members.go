package members

import (
	"fmt"

	"github.com/spf13/cobra"

	"ldcli/internal/projects"
)

func NewMembersCmd(client projects.Client) (*cobra.Command, error) {
	cmd := &cobra.Command{
		Use:   "members",
		Short: "Make requests (create/invite) on members",
		Long:  "Make requests (create/invite) on members",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Println("members called")
		},
	}

	createCmd, err := NewCreateCmd(client)
	if err != nil {
		return nil, err
	}

	cmd.AddCommand(createCmd)

	return cmd, nil

}
