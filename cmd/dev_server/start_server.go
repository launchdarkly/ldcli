package dev_server

import (
	resourcescmd "github.com/launchdarkly/ldcli/cmd/resources"
	"github.com/launchdarkly/ldcli/cmd/validators"
	"github.com/launchdarkly/ldcli/internal/dev_server"
	"github.com/launchdarkly/ldcli/internal/resources"
	"github.com/spf13/cobra"
)

func NewStartServerCmd(client resources.Client) *cobra.Command {
	cmd := &cobra.Command{
		Args:  validators.Validate(),
		Long:  "Start the dev server",
		RunE:  startServer(client),
		Short: "Start the dev server",
		Use:   "start-dev-server",
	}

	cmd.SetUsageTemplate(resourcescmd.SubcommandUsageTemplate())

	return cmd
}

func startServer(client resources.Client) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {

		dev_server.HelloWorld()

		return nil
	}
}
