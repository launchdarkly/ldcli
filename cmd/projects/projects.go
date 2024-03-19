package projects

import (
	"github.com/spf13/cobra"
)

func NewProjectsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "projects",
		Short: "Make requests (list, create, etc.) on projects",
		Long:  "Make requests (list, create, etc.) on projects",
	}

	cmd.AddCommand(NewCreateCmd())
	cmd.AddCommand(NewListCmd())

	return cmd
}
