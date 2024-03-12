package cmd

import (
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var RootCmd = &cobra.Command{
	Use:   "ld-cli",
	Short: "LaunchDarkly CLI",
	Long:  "LaunchDarkly CLI to control your feature flags",
}

func Execute() {
	err := RootCmd.Execute()
	if err != nil {
		os.Exit(1)
	}
}

func init() {
	var (
		accessToken string
		baseURI     string
	)

	RootCmd.PersistentFlags().StringVarP(
		&accessToken,
		"accessToken",
		"t",
		"",
		"LaunchDarkly personal access token.",
	)
	err := viper.BindPFlag("accessToken", RootCmd.PersistentFlags().Lookup("accessToken"))
	if err != nil {
		os.Exit(1)
	}
	RootCmd.PersistentFlags().StringVarP(
		&baseURI,
		"baseUri",
		"u",
		"http://localhost:3000",
		"LaunchDarkly base URI.",
	)
	err = viper.BindPFlag("baseUri", RootCmd.PersistentFlags().Lookup("baseUri"))
	if err != nil {
		os.Exit(1)
	}

	RootCmd.AddCommand(newHelloCmd())
	RootCmd.AddCommand(NewProjectsCmd())
	RootCmd.AddCommand(setupCmd)
}
