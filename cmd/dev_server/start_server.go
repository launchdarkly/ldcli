package dev_server

import (
	"github.com/spf13/cobra"

	resourcescmd "github.com/launchdarkly/ldcli/cmd/resources"
	"github.com/launchdarkly/ldcli/cmd/validators"
	"github.com/launchdarkly/ldcli/internal/dev_server"
)

func NewStartServerCmd() *cobra.Command {
	cmd := &cobra.Command{
		Args:  validators.Validate(),
		Long:  "Start the dev server",
		RunE:  startServer(),
		Short: "Start the dev server",
		Use:   "start",
	}

	cmd.SetUsageTemplate(resourcescmd.SubcommandUsageTemplate())

	return cmd
}

func startServer() func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {

		dev_server.RunServer()

		return nil
	}
}
