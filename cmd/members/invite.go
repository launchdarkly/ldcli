package members

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"ldcli/cmd/validators"
	"ldcli/internal/members"
)

const defaultRole = "reader"

func NewInviteCmd(client members.Client) (*cobra.Command, error) {
	cmd := &cobra.Command{
		Args:  validators.Validate(),
		Long:  "Invite new members",
		RunE:  runInvite(client),
		Short: "Invite new members",
		Use:   "invite",
	}

	cmd.Flags().StringSliceP("emails", "e", []string{}, "A comma separated list of emails")
	err := cmd.MarkFlagRequired("emails")
	if err != nil {
		return nil, err
	}
	err = viper.BindPFlag("emails", cmd.Flags().Lookup("emails"))
	if err != nil {
		return nil, err
	}

	return cmd, nil
}

func runInvite(client members.Client) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {

		response, err := client.Create(
			context.Background(),
			viper.GetString("accessToken"),
			viper.GetString("baseUri"),
			viper.GetStringSlice("emails"),
			defaultRole,
		)
		if err != nil {
			return err
		}

		fmt.Fprintf(cmd.OutOrStdout(), string(response)+"\n")

		return nil
	}
}
