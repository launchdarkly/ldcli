package cmd

import (
	"fmt"
	"log"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"ldcli/cmd/cliflags"
	"ldcli/internal/environments"
	"ldcli/internal/flags"
	"ldcli/internal/quickstart"
)

func NewQuickStartCmd(
	environmentsClient environments.Client,
	flagsClient flags.Client,
) *cobra.Command {
	return &cobra.Command{
		Long:  "",
		RunE:  runQuickStart(environmentsClient, flagsClient),
		Short: "Setup guide to create your first feature flag",
		Use:   "setup",
	}
}

func runQuickStart(
	environmentsClient environments.Client,
	flagsClient flags.Client,
) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		f, err := tea.LogToFile("debug.log", "")
		if err != nil {
			fmt.Println("could not open file for debugging:", err)
			os.Exit(1)
		}
		defer f.Close()

		_, err = tea.NewProgram(quickstart.NewContainerModel(
			environmentsClient,
			flagsClient,
			viper.GetString(cliflags.AccessTokenFlag),
			viper.GetString(cliflags.BaseURIFlag),
		)).Run()
		if err != nil {
			log.Fatal(err)
		}

		return nil
	}
}
