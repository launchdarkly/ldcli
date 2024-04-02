package cmd

import (
	"fmt"
	"log"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"ldcli/cmd/cliflags"
	"ldcli/internal/flags"
	"ldcli/internal/quickstart"
)

func NewQuickStartCmd(client flags.Client) *cobra.Command {
	return &cobra.Command{
		Long:  "",
		RunE:  runQuickStart(client),
		Short: "Setup guide to create your first feature flag",
		Use:   "setup",
	}
}

func runQuickStart(client flags.Client) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		f, err := tea.LogToFile("debug.log", "")
		if err != nil {
			fmt.Println("could not open file for debugging:", err)
			os.Exit(1)
		}
		defer f.Close()

		_, err = tea.NewProgram(quickstart.NewContainerModel(
			client,
			viper.GetString(cliflags.AccessTokenFlag),
			viper.GetString(cliflags.BaseURIFlag),
		)).Run()
		if err != nil {
			log.Fatal(err)
		}

		return nil
	}
}
