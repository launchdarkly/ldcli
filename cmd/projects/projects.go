package projects

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"ld-cli/internal/projects"
)

func NewProjectsCmd(client projects.Client) *cobra.Command {
	cmd := &cobra.Command{
		Use:              "projects",
		Short:            "Make requests (list, create, etc.) on projects",
		Long:             "Make requests (list, create, etc.) on projects",
		TraverseChildren: true,
	}
	fmt.Println(">>> ProjectsCmd", viper.GetString("baseUri"))

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
