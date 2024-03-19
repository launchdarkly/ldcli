package projects

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"ld-cli/internal/projects"
)

type createCmd struct {
	Cmd *cobra.Command
}

func NewCreateCmd() (createCmd, error) {
	var data string

	cmd := &cobra.Command{
		Use:     "create",
		Short:   "Create a new project",
		Long:    "Create a new project",
		PreRunE: validate,
		RunE:    runCreate,
	}

	cmd.Flags().StringVarP(
		&data,
		"data",
		"d",
		"",
		"Input data in JSON",
	)
	err := cmd.MarkFlagRequired("data")
	if err != nil {
		return createCmd{}, err
	}
	err = viper.BindPFlag("accessToken", cmd.Flags().Lookup("data"))
	if err != nil {
		return createCmd{}, err
	}

	return createCmd{
		Cmd: cmd,
	}, nil
}

// runCreate creates a new project.
func runCreate(cmd *cobra.Command, args []string) error {
	fmt.Println(">>> runCreate")
	fmt.Println(">>> accessToken", viper.GetString("accessToken"))
	fmt.Println(">>> baseUri", viper.GetString("baseUri"))
	fmt.Println(">>> data", viper.GetString("data"))
	client := projects.NewClient(
		viper.GetString("accessToken"),
		viper.GetString("baseUri"),
	)
	response, err := projects.CreateProject(
		context.Background(),
		client,
		"test-name",
		"test-key",
	)
	if err != nil {
		return err
	}

	fmt.Fprintf(cmd.OutOrStdout(), string(response)+"\n")

	return nil
}
