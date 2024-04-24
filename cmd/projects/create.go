package projects

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"ldcli/cmd/cliflags"
	"ldcli/cmd/validators"
	"ldcli/internal/errors"
	"ldcli/internal/output"
	"ldcli/internal/projects"
)

func NewCreateCmd(client projects.Client) (*cobra.Command, error) {
	cmd := &cobra.Command{
		Args:  validators.Validate(),
		Long:  "Create a new project",
		RunE:  runCreate(client),
		Short: "Create a new project",
		Use:   "create",
	}

	cmd.Flags().StringP(cliflags.DataFlag, "d", "", "Input data in JSON")
	err := cmd.MarkFlagRequired(cliflags.DataFlag)
	if err != nil {
		return nil, err
	}
	err = viper.BindPFlag(cliflags.DataFlag, cmd.Flags().Lookup(cliflags.DataFlag))
	if err != nil {
		return nil, err
	}

	return cmd, nil
}

type inputData struct {
	Name string `json:"name"`
	Key  string `json:"key"`
}

func runCreate(client projects.Client) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		var data inputData
		// TODO: why does viper.GetString(cliflags.DataFlag) not work?
		err := json.Unmarshal([]byte(cmd.Flags().Lookup(cliflags.DataFlag).Value.String()), &data)
		if err != nil {
			return err
		}

		response, err := client.Create(
			context.Background(),
			viper.GetString(cliflags.AccessTokenFlag),
			viper.GetString(cliflags.BaseURIFlag),
			data.Name,
			data.Key,
		)
		if err != nil {
			output, err := output.CmdOutputSingular(
				viper.GetString(cliflags.OutputFlag),
				[]byte(err.Error()),
				output.ErrorPlaintextOutputFn,
			)
			if err != nil {
				return errors.NewError(err.Error())
			}

			return errors.NewError(output)
		}

		output, err := output.CmdOutputSingular(
			viper.GetString(cliflags.OutputFlag),
			response,
			output.SingularPlaintextOutputFn,
		)
		if err != nil {
			return errors.NewError(err.Error())
		}

		fmt.Fprintf(cmd.OutOrStdout(), output+"\n")

		return nil
	}
}
