package dev_server

import (
	"context"
	"errors"
	"log"
	"os/exec"
	"runtime"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/launchdarkly/ldcli/cmd/cliflags"
	resourcescmd "github.com/launchdarkly/ldcli/cmd/resources"
	"github.com/launchdarkly/ldcli/cmd/validators"
	"github.com/launchdarkly/ldcli/internal/dev_server"
)

func NewStartServerCmd(client dev_server.Client) *cobra.Command {
	cmd := &cobra.Command{
		GroupID: "server",
		Args:    validators.Validate(),
		Long:    "start the dev server",
		RunE:    startServer(client),
		Short:   "start the dev server",
		Use:     "start",
	}

	cmd.SetUsageTemplate(resourcescmd.SubcommandUsageTemplate())

	return cmd
}

func startServer(client dev_server.Client) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		ctx := context.Background()
		params := dev_server.ServerParams{
			AccessToken:  viper.GetString(cliflags.AccessTokenFlag),
			BaseURI:      viper.GetString(cliflags.BaseURIFlag),
			DevStreamURI: viper.GetString(cliflags.DevStreamURIFlag),
			Port:         viper.GetString(cliflags.PortFlag),
		}

		client.RunServer(ctx, params)

		return nil
	}
}

func NewUICmd() *cobra.Command {
	cmd := &cobra.Command{
		GroupID: "server",
		Args:    validators.Validate(),
		Long:    "open the dev ui in your default browser",
		RunE:    openUI(),
		Short:   "open the ui",
		Use:     "ui",
	}

	cmd.SetUsageTemplate(resourcescmd.SubcommandUsageTemplate())

	return cmd
}

func openUI() func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		url := getDevServerUrl()

		var err error
		switch runtime.GOOS {
		case "linux":
			err = exec.Command("xdg-open", url).Start()
		case "windows":
			err = exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
		case "darwin":
			err = exec.Command("open", url).Start()
		default:
			err = errors.New("unsupported platform")
		}
		if err != nil {
			log.Fatalf("Unable to open ui, %v", err)
		}

		return nil
	}
}
