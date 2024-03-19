package projects

import (
	"github.com/spf13/cobra"
)

type projectCmd struct {
	Cmd *cobra.Command
}

func NewProjectsCmd() projectCmd {
	cmd := &cobra.Command{
		Use:   "projects",
		Short: "Make requests (list, create, etc.) on projects",
		Long:  "Make requests (list, create, etc.) on projects",
	}

	cmd.AddCommand(NewCreateCmd().Cmd)
	cmd.AddCommand(NewListCmd().Cmd)

	return projectCmd{
		Cmd: cmd,
	}
}
