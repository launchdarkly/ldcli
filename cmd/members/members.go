package members

import (
	"fmt"

	"github.com/spf13/cobra"
)

func NewMembersCmd() (*cobra.Command, error) {
	cmd := &cobra.Command{
		Use:   "members",
		Short: "Make requests (create/invite) on members",
		Long:  "Make requests (create/invite) on members",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Println("members called")
		},
	}

	return cmd, nil

}
