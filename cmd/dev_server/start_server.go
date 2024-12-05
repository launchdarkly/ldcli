package dev_server

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"os/exec"
	"runtime"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/launchdarkly/go-sdk-common/v3/ldcontext"
	"github.com/launchdarkly/ldcli/cmd/cliflags"
	resourcescmd "github.com/launchdarkly/ldcli/cmd/resources"
	"github.com/launchdarkly/ldcli/cmd/validators"
	"github.com/launchdarkly/ldcli/internal/dev_server"
	"github.com/launchdarkly/ldcli/internal/dev_server/model"
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

	cmd.Flags().String(cliflags.ProjectFlag, "", "The project key")
	_ = viper.BindPFlag(cliflags.ProjectFlag, cmd.Flags().Lookup(cliflags.ProjectFlag))

	cmd.Flags().String(SourceEnvironmentFlag, "", "environment to copy flag values from")
	_ = viper.BindPFlag(SourceEnvironmentFlag, cmd.Flags().Lookup(SourceEnvironmentFlag))

	cmd.Flags().String(ContextFlag, "", `Stringified JSON representation of your context object ex. {"user": { "email": "test@gmail.com", "username": "foo", "key": "bar"}}`)
	_ = viper.BindPFlag(ContextFlag, cmd.Flags().Lookup(ContextFlag))

	cmd.Flags().String(OverrideFlag, "", `Stringified JSON representation of flag overrides ex. {"flagName": true, "stringFlagName": "test" }`)
	_ = viper.BindPFlag(OverrideFlag, cmd.Flags().Lookup(OverrideFlag))

	return cmd
}

func startServer(client dev_server.Client) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		ctx := context.Background()

		var initialSetting model.InitialProjectSettings

		if viper.IsSet(cliflags.ProjectFlag) && viper.IsSet(SourceEnvironmentFlag) {

			initialSetting = model.InitialProjectSettings{
				Enabled:    true,
				ProjectKey: viper.GetString(cliflags.ProjectFlag),
				EnvKey:     viper.GetString(SourceEnvironmentFlag),
			}
			if viper.IsSet(ContextFlag) {
				var c ldcontext.Context
				contextString := viper.GetString(ContextFlag)
				err := c.UnmarshalJSON([]byte(contextString))
				if err != nil {
					return err
				}
				initialSetting.Context = &c
			}

			if viper.IsSet(OverrideFlag) {
				var override map[string]model.FlagValue
				overrideString := viper.GetString(OverrideFlag)
				err := json.Unmarshal([]byte(overrideString), &override)
				if err != nil {
					return err
				}
				initialSetting.Overrides = override
			}
		}

		params := dev_server.ServerParams{
			AccessToken:            viper.GetString(cliflags.AccessTokenFlag),
			BaseURI:                viper.GetString(cliflags.BaseURIFlag),
			DevStreamURI:           viper.GetString(cliflags.DevStreamURIFlag),
			Port:                   viper.GetString(cliflags.PortFlag),
			InitialProjectSettings: initialSetting,
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
