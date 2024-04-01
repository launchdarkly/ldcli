package members

import (
	"context"
	"fmt"

	ldapi "github.com/launchdarkly/api-client-go/v14"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"ldcli/cmd/cliflags"
	"ldcli/cmd/validators"
	"ldcli/internal/members"
)

func NewInviteCmd(client members.Client) (*cobra.Command, error) {
	cmd := &cobra.Command{
		Args:  validators.Validate(),
		Long:  "Create new members and send them an invitation email",
		RunE:  runInvite(client),
		Short: "Invite new members",
		Use:   "invite",
	}

	cmd.Flags().StringSliceP(cliflags.EmailsFlag, "e", []string{}, "A comma separated list of emails")
	err := cmd.MarkFlagRequired(cliflags.EmailsFlag)
	if err != nil {
		return nil, err
	}
	err = viper.BindPFlag(cliflags.EmailsFlag, cmd.Flags().Lookup(cliflags.EmailsFlag))
	if err != nil {
		return nil, err
	}

	cmd.Flags().StringP(
		cliflags.RoleFlag,
		"r",
		"reader",
		"Built-in role for the member - one of reader, writer, or admin",
	)
	err = viper.BindPFlag(cliflags.RoleFlag, cmd.Flags().Lookup(cliflags.RoleFlag))
	if err != nil {
		return nil, err
	}

	return cmd, nil
}

func runInvite(client members.Client) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		emails := viper.GetStringSlice(cliflags.EmailsFlag)
		members := make([]ldapi.NewMemberForm, 0, len(emails))
		for _, e := range emails {
			role := viper.GetString(cliflags.RoleFlag)
			members = append(members, ldapi.NewMemberForm{Email: e, Role: &role})
		}
		response, err := client.Create(
			context.Background(),
			viper.GetString(cliflags.AccessTokenFlag),
			viper.GetString(cliflags.BaseURIFlag),
			members,
		)
		if err != nil {
			return err
		}

		fmt.Fprintf(cmd.OutOrStdout(), string(response)+"\n")

		return nil
	}
}
