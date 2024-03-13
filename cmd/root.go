package cmd

import (
	"errors"
	"fmt"
	"ld-cli/internal/projects"
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var rootCmd = &cobra.Command{
	Use:     "ld-cli",
	Short:   "LaunchDarkly CLI",
	Long:    "LaunchDarkly CLI to control your feature flags",
	Version: "0.0.1", // TODO: set this based on release or use `cmd.SetVersionTemplate(s string)`

	// handle errors differently based on type
	SilenceUsage:  true,
	SilenceErrors: true,
}

func Execute() {
	err := rootCmd.Execute()
	if err != nil {
		switch {
		case errors.Is(err, projects.ErrUnauthorized):
			fmt.Fprintln(os.Stderr, err.Error())
		default:
			fmt.Println(rootCmd.ErrPrefix(), err.Error())
			fmt.Println(rootCmd.UsageString())
		}
	}
}

func init() {
	var (
		accessToken string
		baseURI     string
	)

	rootCmd.PersistentFlags().StringVarP(
		&accessToken,
		"accessToken",
		"t",
		"",
		"LaunchDarkly personal access token.",
	)
	err := rootCmd.MarkPersistentFlagRequired("accessToken")
	if err != nil {
		panic(err)
	}
	err = viper.BindPFlag("accessToken", rootCmd.PersistentFlags().Lookup("accessToken"))
	if err != nil {
		panic(err)
	}
	rootCmd.PersistentFlags().StringVarP(
		&baseURI,
		"baseUri",
		"u",
		"http://localhost:3000",
		"LaunchDarkly base URI.",
	)
	err = viper.BindPFlag("baseUri", rootCmd.PersistentFlags().Lookup("baseUri"))
	if err != nil {
		panic(err)
	}

	rootCmd.SetErrPrefix("")

	rootCmd.AddCommand(newHelloCmd())
	rootCmd.AddCommand(NewProjectsCmd())
	rootCmd.AddCommand(setupCmd)
}
