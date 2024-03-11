package cmd

import (
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var rootCmd = &cobra.Command{
	Use:   "ld-cli",
	Short: "LaunchDarkly CLI",
	Long:  "LaunchDarkly CLI to control your feature flags",
}

func Execute() {
	err := rootCmd.Execute()
	if err != nil {
		os.Exit(1)
	}
}

func init() {
	var accessToken string

	rootCmd.PersistentFlags().StringVarP(
		&accessToken,
		"baseUri",
		"u",
		"http://localhost:3000",
		`LaunchDarkly base URI. (default "https://app.launchdarkly.com")`,
	)
	err := viper.BindPFlag("baseUri", rootCmd.PersistentFlags().Lookup("baseUri"))
	if err != nil {
		os.Exit(1)
	}
	rootCmd.PersistentFlags().StringVarP(
		&accessToken,
		"accessToken",
		"t",
		"",
		"LaunchDarkly personal access token with write-level access.",
	)
	err = viper.BindPFlag("accessToken", rootCmd.PersistentFlags().Lookup("accessToken"))
	if err != nil {
		os.Exit(1)
	}

	rootCmd.AddCommand(newHelloCmd())
	rootCmd.AddCommand(setupCmd)
}
