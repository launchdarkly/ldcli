package cmd

import (
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"ld-cli/cmd/projects"
)

var rootCmd = &cobra.Command{
	Use:   "ldcli",
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
	var (
		accessToken string
		baseURI     string
	)

	rootCmd.PersistentFlags().StringVarP(
		&accessToken,
		"accessToken",
		"t",
		"",
		"LaunchDarkly personal access token",
	)
	err := viper.BindPFlag("accessToken", rootCmd.PersistentFlags().Lookup("accessToken"))
	if err != nil {
		os.Exit(1)
	}
	rootCmd.PersistentFlags().StringVarP(
		&baseURI,
		"baseUri",
		"u",
		"http://localhost:3000",
		"LaunchDarkly base URI",
	)
	err = viper.BindPFlag("baseUri", rootCmd.PersistentFlags().Lookup("baseUri"))
	if err != nil {
		os.Exit(1)
	}

	rootCmd.AddCommand(projects.NewProjectsCmd())
	rootCmd.AddCommand(setupCmd)
}
