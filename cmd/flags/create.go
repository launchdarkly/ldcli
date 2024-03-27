package flags

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"ldcli/cmd/validators"
	"ldcli/internal/flags"
)

func NewCreateCmd(client flags.Client) (*cobra.Command, error) {
	cmd := &cobra.Command{
		Args:  validators.Validate(),
		Long:  "Create a new flag",
		RunE:  runCreate(client),
		Short: "Create a new flag",
		Use:   "create",
	}

	cmd.Flags().StringP("data", "d", "", "Input data in JSON")
	err := cmd.MarkFlagRequired("data")
	if err != nil {
		return nil, err
	}
	err = viper.BindPFlag("data", cmd.Flags().Lookup("data"))
	if err != nil {
		return nil, err
	}

	cmd.Flags().String("projKey", "", "Project key")
	err = cmd.MarkFlagRequired("projKey")
	if err != nil {
		return nil, err
	}
	err = viper.BindPFlag("projKey", cmd.Flags().Lookup("projKey"))
	if err != nil {
		return nil, err
	}

	return cmd, nil
}

type inputData struct {
	Name string `json:"name"`
	Key  string `json:"key"`
}

func runCreate(client flags.Client) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		// rebind flags used in other subcommands
		_ = viper.BindPFlag("data", cmd.Flags().Lookup("data"))
		_ = viper.BindPFlag("projKey", cmd.Flags().Lookup("projKey"))

		var data inputData
		err := json.Unmarshal([]byte(viper.GetString("data")), &data)
		if err != nil {
			return err
		}

		response, err := client.Create(
			context.Background(),
			viper.GetString("api-token"),
			viper.GetString("base-uri"),
			data.Name,
			data.Key,
			viper.GetString("projKey"),
		)
		if err != nil {
			return err
		}

		fmt.Fprintf(cmd.OutOrStdout(), string(response)+"\n")

		return nil
	}
}
