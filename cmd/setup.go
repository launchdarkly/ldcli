package cmd

import (
	"fmt"
	"log"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"ld-cli/internal/setup"
)

var setupCmd = &cobra.Command{
	Use:   "setup",
	Short: "Setup guide to create your first feature flag",
	Long:  "",
	RunE:  runSetup,
}

func runSetup(cmd *cobra.Command, args []string) error {
	f, err := tea.LogToFile("debug.log", "")
	if err != nil {
		fmt.Println("could not open file for debugging:", err)
		os.Exit(1)
	}
	defer f.Close()

	_, err = tea.NewProgram(setup.NewWizardModel()).Run()
	if err != nil {
		log.Fatal(err)
	}

	return nil
}
