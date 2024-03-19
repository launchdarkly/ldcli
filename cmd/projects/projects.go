package projects

import (
	"github.com/spf13/cobra"
)

type projectsCmd struct {
	Cmd *cobra.Command
}

func NewProjectsCmd() (*projectsCmd, error) {
	cmd := &cobra.Command{
		Use:   "projects",
		Short: "Make requests (list, create, etc.) on projects",
		Long:  "Make requests (list, create, etc.) on projects",
	}

	createCmd, err := NewCreateCmd()
	if err != nil {
		return &projectsCmd{}, err
	}
	cmd.AddCommand(createCmd.Cmd)
	listCmd, err := NewListCmd()
	if err != nil {
		return &projectsCmd{}, err
	}
	cmd.AddCommand(listCmd.Cmd)

	cmd.Flags().AddFlagSet(createCmd.Cmd.Flags())
	cmd.Flags().AddFlagSet(listCmd.Cmd.Flags())

	return &projectsCmd{
		Cmd: cmd,
	}, nil
}
