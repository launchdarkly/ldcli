package members

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"ldcli/cmd/validators"
	"ldcli/internal/members"
)

func NewCreateCmd(client members.Client) (*cobra.Command, error) {
	cmd := &cobra.Command{
		Args:  validators.Validate(),
		Long:  "Create a new member",
		RunE:  runCreate(client),
		Short: "Create a new member",
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

	return cmd, nil
}

type inputData struct {
	Email string `json:"email"`
	Role  string `json:"role"`
}

func runCreate(client members.Client) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		var data inputData
		// TODO: why does viper.GetString("data") not work?
		err := json.Unmarshal([]byte(cmd.Flags().Lookup("data").Value.String()), &data)
		if err != nil {
			return err
		}

		response, err := client.Create(
			context.Background(),
			viper.GetString("accessToken"),
			viper.GetString("baseUri"),
			data.Email,
			data.Role,
		)
		if err != nil {
			return err
		}

		fmt.Fprintf(cmd.OutOrStdout(), string(response)+"\n")

		return nil
	}
}
