package utils

import (
	"ldcli/cmd/cliflags"
	"ldcli/cmd/constants"
	"ldcli/internal/analytics"

	"github.com/google/uuid"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

type CommandRunEventType struct {
	EventName string
}

func (c CommandRunEventType) SendEvents(analyticsTracker analytics.Tracker, cmd *cobra.Command) {
	analyticsTracker.SendEvent(
		viper.GetString(cliflags.AccessTokenFlag),
		viper.GetString(cliflags.BaseURIFlag),
		"CLI Command Run",
		c.buildProperties(cmd),
	)
}

func (c CommandRunEventType) buildProperties(cmd *cobra.Command) map[string]interface{} {
	id := uuid.New()
	baseURI := viper.GetString(cliflags.BaseURIFlag)
	var flags []string
	cmd.Flags().Visit(func(f *pflag.Flag) {
		flags = append(flags, f.Name)
	})

	properties := map[string]interface{}{
		"name":   c.EventName,
		"action": cmd.CalledAs(),
		"flags":  flags,
		"id":     id.String(),
	}
	if baseURI != constants.LaunchDarklyBaseURI {
		properties["baseURI"] = baseURI
	}
	return properties
}
