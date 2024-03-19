package cmd

import (
	"errors"
	"fmt"
	"os"

	errs "ld-cli/internal/errors"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"ld-cli/cmd/projects"
)

func newRootCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "ldcli",
		Short:   "LaunchDarkly CLI",
		Long:    "LaunchDarkly CLI to control your feature flags",
		Version: "0.0.1", // TODO: set this based on release or use `cmd.SetVersionTemplate(s string)`

		// Handle errors differently based on type.
		// We don't want to show the usage if the user has the right structure but invalid data such as
		// the wrong key.
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	var (
		accessToken string
		baseURI     string
	)

	cmd.PersistentFlags().StringVarP(
		&accessToken,
		"accessToken",
		"t",
		"",
		"LaunchDarkly personal access token",
	)
	err := cmd.MarkPersistentFlagRequired("accessToken")
	if err != nil {
		panic(err)
	}
	err = viper.BindPFlag("accessToken", cmd.PersistentFlags().Lookup("accessToken"))
	if err != nil {
		panic(err)
	}

	cmd.PersistentFlags().StringVarP(
		&baseURI,
		"baseUri",
		"u",
		"https://app.launchdarkly.com",
		"LaunchDarkly base URI",
	)
	err = viper.BindPFlag("baseUri", cmd.PersistentFlags().Lookup("baseUri"))
	if err != nil {
		panic(err)
	}

	cmd.SetErrPrefix("")

	cmd.AddCommand(projects.NewProjectsCmd())
	cmd.AddCommand(setupCmd)

	return cmd
}

func Execute() {
	rootCmd := newRootCommand()
	err := rootCmd.Execute()
	if err != nil {
		switch {
		case errors.Is(err, errs.ErrForbidden),
			errors.Is(err, errs.ErrInvalidBaseURI),
			errors.Is(err, errs.ErrUnauthorized):
			fmt.Fprintln(os.Stderr, err.Error())
		default:
			fmt.Println(rootCmd.ErrPrefix(), err.Error())
			fmt.Println(rootCmd.UsageString())
		}
	}
}
