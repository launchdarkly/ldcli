package dev_server

import (
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/launchdarkly/ldcli/cmd/cliflags"
	resourcescmd "github.com/launchdarkly/ldcli/cmd/resources"
	"github.com/launchdarkly/ldcli/cmd/validators"
	"github.com/launchdarkly/ldcli/internal/dev_server"
	"github.com/launchdarkly/ldcli/internal/resources"
)

func NewStartServerCmd(client resources.Client) *cobra.Command {
	cmd := &cobra.Command{
		Args:  validators.Validate(),
		Long:  "Start the dev server",
		RunE:  startServer(client),
		Short: "Start the dev server",
		Use:   "start",
	}

	cmd.SetUsageTemplate(resourcescmd.SubcommandUsageTemplate())

	return cmd
}

func startServer(client resources.Client) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {

		dev_server.HelloWorld(viper.GetString(cliflags.AccessTokenFlag))

		return nil
	}
}
