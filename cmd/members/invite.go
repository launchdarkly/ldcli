package members

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"ldcli/cmd/cliflags"
	resourcescmd "ldcli/cmd/resources"
	"ldcli/cmd/validators"
	"ldcli/internal/errors"
	"ldcli/internal/members"
	"ldcli/internal/output"
	"ldcli/internal/resources"
)

func NewMembersInviteCmd(client resources.Client) *cobra.Command {
	cmd := &cobra.Command{
		Args:  validators.Validate(),
		Long:  "Create new members and send them an invitation email",
		RunE:  runE(client),
		Short: "Invite new members",
		Use:   "invite",
	}

	cmd.SetUsageTemplate(resourcescmd.SubcommandUsageTemplate())
	initFlags(cmd)

	return cmd
}

func runE(client resources.Client) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		emails := viper.GetStringSlice(cliflags.EmailsFlag)
		memberInputs := make([]members.MemberInput, 0, len(emails))
		for _, e := range emails {
			role := viper.GetString(cliflags.RoleFlag)
			memberInputs = append(memberInputs, members.MemberInput{Email: e, Role: role})
		}

		membersJson, err := json.Marshal(memberInputs)
		if err != nil {
			return errors.NewError(err.Error())
		}

		path := fmt.Sprintf(
			"%s/api/v2/members",
			viper.GetString(cliflags.BaseURIFlag),
		)
		res, err := client.MakeRequest(
			viper.GetString(cliflags.AccessTokenFlag),
			"POST",
			path,
			"application/json",
			nil,
			membersJson,
		)
		if err != nil {
			return errors.NewError(output.CmdOutputError(viper.GetString(cliflags.OutputFlag), err))
		}

		output, err := output.CmdOutput("update", viper.GetString(cliflags.OutputFlag), res)
		if err != nil {
			return errors.NewError(err.Error())
		}

		fmt.Fprintf(cmd.OutOrStdout(), output+"\n")

		return nil
	}
}

func initFlags(cmd *cobra.Command) {
	cmd.Flags().StringSliceP(cliflags.EmailsFlag, "e", []string{}, "A comma separated list of emails")
	_ = cmd.MarkFlagRequired(cliflags.EmailsFlag)
	_ = cmd.Flags().SetAnnotation(cliflags.EmailsFlag, "required", []string{"true"})
	_ = viper.BindPFlag(cliflags.EmailsFlag, cmd.Flags().Lookup(cliflags.EmailsFlag))

	cmd.Flags().StringP(
		cliflags.RoleFlag,
		"r",
		"reader",
		"Built-in role for the member - one of reader, writer, or admin",
	)
	_ = viper.BindPFlag(cliflags.RoleFlag, cmd.Flags().Lookup(cliflags.RoleFlag))
}
