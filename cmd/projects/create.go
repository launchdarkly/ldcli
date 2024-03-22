package projects

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"ld-cli/internal/projects"
)

type createCmd struct {
	Cmd *cobra.Command
}

func NewCreateCmd(clientFn projects.ProjectsClientFn) (createCmd, error) {
	cmd := &cobra.Command{
		Use:     "create",
		Short:   "Create a new project",
		Long:    "Create a new project",
		PreRunE: validate,
		RunE:    runCreate(clientFn),
	}

	cmd.Flags().StringP("data", "d", "", "Input data in JSON")
	err := cmd.MarkFlagRequired("data")
	if err != nil {
		return createCmd{}, nil
	}
	err = viper.BindPFlag("data", cmd.Flags().Lookup("data"))
	if err != nil {
		return createCmd{}, nil
	}

	return createCmd{
		Cmd: cmd,
	}, nil
}

type inputData struct {
	Name string `json:"name"`
	Key  string `json:"key"`
}

func runCreate(clientFn projects.ProjectsClientFn) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		client := clientFn(
			viper.GetString("accessToken"),
			viper.GetString("baseUri"),
		)

		var data inputData
		err := json.Unmarshal([]byte(viper.GetString("data")), &data)
		if err != nil {
			return err
		}

		response, err := client.Create(
			context.Background(),
			data.Name,
			data.Key,
		)
		if err != nil {
			return err
		}

		fmt.Fprintf(cmd.OutOrStdout(), string(response)+"\n")

		return nil
	}
}
