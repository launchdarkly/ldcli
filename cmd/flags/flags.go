package flags

import (
	"github.com/google/uuid"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"

	"ldcli/cmd/cliflags"
	"ldcli/internal/analytics"
	"ldcli/internal/flags"
)

func NewFlagsCmd(analyticsTracker analytics.Tracker, client flags.Client) (*cobra.Command, error) {
	cmd := &cobra.Command{
		Use:   "flags",
		Short: "Make requests (list, create, etc.) on flags",
		Long:  "Make requests (list, create, etc.) on flags",
	}

	createCmd, err := NewCreateCmd(client)
	if err != nil {
		return nil, err
	}
	getCmd, err := NewGetCmd(client)
	if err != nil {
		return nil, err
	}
	updateCmd, err := NewUpdateCmd(client)
	if err != nil {
		return nil, err
	}

	toggleOnUpdateCmd, err := NewToggleOnUpdateCmd(client)
	if err != nil {
		return nil, err
	}
	toggleOffUpdateCmd, err := NewToggleOffUpdateCmd(client)
	if err != nil {
		return nil, err
	}

	cmd.AddCommand(createCmd)
	cmd.AddCommand(getCmd)
	cmd.AddCommand(updateCmd)
	cmd.AddCommand(toggleOnUpdateCmd)
	cmd.AddCommand(toggleOffUpdateCmd)

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
		"name":   "flags",
		"action": cmd.CalledAs(),
		"flags":  flags,
		"id":     id.String(),
	}
	if baseURI != cliflags.BaseURIDefault {
		properties["baseURI"] = baseURI
	}
	return properties
}
