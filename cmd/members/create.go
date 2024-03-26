package members

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"ldcli/internal/errors"
	"ldcli/internal/members"
)

func NewCreateCmd(client members.Client) (*cobra.Command, error) {
	cmd := &cobra.Command{
		Use:     "create",
		Short:   "Create a new member",
		Long:    "Create a new member",
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

// validate ensures the flags are valid before using them.
// TODO: refactor with flags & projects validate().
func validate(cmd *cobra.Command, args []string) error {
	_, err := url.ParseRequestURI(viper.GetString("baseUri"))
	if err != nil {
		return errors.ErrInvalidBaseURI
	}

	return nil
}
