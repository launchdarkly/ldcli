package projects

import (
	"github.com/spf13/cobra"

	"ld-cli/internal/projects"
)

type projectsCmd struct {
	Cmd *cobra.Command
}

func NewProjectsCmd(client projects.Client2) (projectsCmd, error) {
	cmd := &cobra.Command{
		Use:              "projects",
		Short:            "Make requests (list, create, etc.) on projects",
		Long:             "Make requests (list, create, etc.) on projects",
		TraverseChildren: true,
	}

	createCmd, err := NewCreateCmd(client)
	if err != nil {
		return projectsCmd{}, err
	}
	listCmd, err := NewListCmd(client)
	if err != nil {
		return projectsCmd{}, err
	}

	cmd.AddCommand(createCmd.Cmd)
	cmd.AddCommand(listCmd.Cmd)

	return projectsCmd{
		Cmd: cmd,
	}, nil
}
