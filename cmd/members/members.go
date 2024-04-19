package members

import (
	"github.com/google/uuid"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"

	"ldcli/cmd/cliflags"
	"ldcli/internal/analytics"
	"ldcli/internal/members"
)

func NewMembersCmd(analyticsTracker analytics.Tracker, client members.Client) (*cobra.Command, error) {
	cmd := &cobra.Command{
		Use:   "members",
		Short: "Make requests (list, create, etc.) on members",
		Long:  "Make requests (list, create, etc.) on members",
	}

	createCmd, err := NewCreateCmd(client)
	if err != nil {
		return nil, err
	}

	inviteCmd, err := NewInviteCmd(client)
	if err != nil {
		return nil, err
	}

	cmd.AddCommand(createCmd)
	cmd.AddCommand(inviteCmd)

	cmd.PersistentPreRun = func(cmd *cobra.Command, args []string) {
		analyticsTracker.SendEvent(
			viper.GetString(cliflags.AccessTokenFlag),
			viper.GetString(cliflags.BaseURIFlag),
			"CLI Command Run",
			buildPropertiesForCommandRunEvent(cmd),
		)
	}

	return cmd, nil

}

func buildPropertiesForCommandRunEvent(cmd *cobra.Command) map[string]interface{} {
	id := uuid.New()
	baseURI := viper.GetString(cliflags.BaseURIFlag)
	var flags []string
	cmd.Flags().Visit(func(f *pflag.Flag) {
		flags = append(flags, f.Name)
	})

	properties := map[string]interface{}{
		"name":   "members",
		"action": cmd.CalledAs(),
		"flags":  flags,
		"id":     id.String(),
	}
	if baseURI != cliflags.BaseURIDefault {
		properties["baseURI"] = baseURI
	}
	return properties
}
