package flags

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"ldcli/internal/errors"
	"ldcli/internal/flags"
)

func NewCreateCmd(client flags.Client) (*cobra.Command, error) {
	cmd := &cobra.Command{
		Use:     "create",
		Short:   "Create a new flag",
		Long:    "Create a new flag",
		PreRunE: validate,
		RunE:    runCreate(client),
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
			viper.GetString("accessToken"),
			viper.GetString("baseUri"),
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

// validate ensures the flags are valid before using them.
// TODO: refactor with projects validate().
func validate(cmd *cobra.Command, args []string) error {
	_, err := url.ParseRequestURI(viper.GetString("baseUri"))
	if err != nil {
		return errors.ErrInvalidBaseURI
	}

	return nil
}
