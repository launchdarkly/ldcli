package cmd

import (
	"fmt"
	"ldcli/internal/list_test"
	"log"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"
)

func NewListTestCmd() *cobra.Command {
	return &cobra.Command{
		Long:  "",
		RunE:  runListTest(),
		Short: "test a list",
		Use:   "list-test",
	}
}

func runListTest() func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		f, err := tea.LogToFile("debug.log", "")
		if err != nil {
			fmt.Println("could not open file for debugging:", err)
			os.Exit(1)
		}
		defer f.Close()

		_, err = tea.NewProgram(list_test.NewListTestModel()).Run()
		if err != nil {
			log.Fatal(err)
		}

		return nil
	}
}
