package projects

import (
	"github.com/spf13/cobra"

	"ldcli/internal/analytics"
	"ldcli/internal/projects"
)

func NewProjectsCmd(analyticsTracker analytics.Tracker, client projects.Client) (*cobra.Command, error) {
	cmd := &cobra.Command{
		Use:   "projects",
		Short: "Make requests (list, create, etc.) on projects",
		Long:  "Make requests (list, create, etc.) on projects",
	}

	createCmd, err := NewCreateCmd(analyticsTracker, client)
	if err != nil {
		return nil, err
	}
	listCmd := NewListCmd(analyticsTracker, client)

	cmd.AddCommand(createCmd)
	cmd.AddCommand(listCmd)

	return cmd, nil
}
