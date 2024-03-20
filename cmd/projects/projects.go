package projects

import (
	"github.com/spf13/cobra"

	"ld-cli/internal/projects"
)

func NewProjectsCmd(client projects.Client) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "projects",
		Short: "Make requests (list, create, etc.) on projects",
		Long:  "Make requests (list, create, etc.) on projects",
	}

	createCmd, err := NewCreateCmd(client)
	if err != nil {
		panic(err)
	}
	listCmd, err := NewListCmd(client)
	if err != nil {
		panic(err)
	}

	cmd.AddCommand(createCmd.Cmd)
	cmd.AddCommand(listCmd.Cmd)

	return cmd
}
