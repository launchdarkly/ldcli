package members

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"ldcli/cmd/cliflags"
	"ldcli/cmd/validators"
	"ldcli/internal/members"
	"ldcli/internal/output"
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
		memberInputs := make([]members.MemberInput, 0, len(emails))
		for _, e := range emails {
			role := viper.GetString(cliflags.RoleFlag)
			memberInputs = append(memberInputs, members.MemberInput{Email: e, Role: role})
		}
		response, err := client.Create(
			context.Background(),
			viper.GetString(cliflags.AccessTokenFlag),
			viper.GetString(cliflags.BaseURIFlag),
			memberInputs,
		)
		if err != nil {
			return err
		}

		output, err := output.CmdOutput(
			viper.GetString(cliflags.OutputFlag),
			output.NewSingularOutputterFn(response),
		)
		if err != nil {
			return err
		}

		fmt.Fprintf(cmd.OutOrStdout(), output+"\n")

		return nil
	}
}
