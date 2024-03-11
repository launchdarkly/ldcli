package cmd

import (
	"fmt"
	"ld-cli/internal/projects"

	"github.com/spf13/cobra"
)

var projectsCmd = &cobra.Command{
	Use:   "projects",
	Short: "",
	Long:  "",
	RunE:  runProjectsGet,
}

func runProjectsGet(cmd *cobra.Command, args []string) error {
	response, err := projects.GetProjects()
	if err != nil {
		return err
	}

	// TODO: should this return response and let caller output or pass in stdout-ish interface?
	fmt.Println(response)

	return nil
}
