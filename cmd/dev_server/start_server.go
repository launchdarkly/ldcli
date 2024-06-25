package dev_server

import (
	"context"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/launchdarkly/ldcli/cmd/cliflags"
	resourcescmd "github.com/launchdarkly/ldcli/cmd/resources"
	"github.com/launchdarkly/ldcli/cmd/validators"
	"github.com/launchdarkly/ldcli/internal/dev_server"
)

func NewStartServerCmd(client dev_server.Client) *cobra.Command {
	cmd := &cobra.Command{
		Args:  validators.Validate(),
		Long:  "start the dev server",
		RunE:  startServer(client),
		Short: "start the dev server",
		Use:   "start",
	}

	cmd.SetUsageTemplate(resourcescmd.SubcommandUsageTemplate())

	return cmd
}

func startServer(client dev_server.Client) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		ctx := context.Background()

		client.RunServer(ctx, viper.GetString(cliflags.AccessTokenFlag), viper.GetString(cliflags.BaseURIFlag))

		return nil
	}
}
