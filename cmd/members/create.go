package members

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"ldcli/cmd/cliflags"
	"ldcli/cmd/validators"
	"ldcli/internal/members"
	"ldcli/internal/output"
)

func NewCreateCmd(client members.Client) (*cobra.Command, error) {
	cmd := &cobra.Command{
		Args:  validators.Validate(),
		Long:  "Create new members and send them an invitation email",
		RunE:  runCreate(client),
		Short: "Create new members",
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

func runCreate(client members.Client) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		var data []members.MemberInput
		// TODO: why does viper.GetString(cliflags.DataFlag) not work?
		err := json.Unmarshal([]byte(cmd.Flags().Lookup(cliflags.DataFlag).Value.String()), &data)
		if err != nil {
			return err
		}

		response, err := client.Create(
			context.Background(),
			viper.GetString(cliflags.AccessTokenFlag),
			viper.GetString(cliflags.BaseURIFlag),
			data,
		)
		if err != nil {
			return err
		}

		output, err := output.CmdOutput(
			viper.GetString(cliflags.OutputFlag),
			output.NewSingularOutput(response),
		)
		if err != nil {
			return err
		}

		fmt.Fprintf(cmd.OutOrStdout(), output+"\n")

		return nil
	}
}
