package projects

import (
	"context"
	"errors"
	"fmt"
	"ld-cli/internal/projects"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

func NewListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "Return a list of projects",
		Long:  "Return a list of projects",
		RunE:  runList,
	}

	cmd.AddCommand()

	return cmd
}

func runList(cmd *cobra.Command, args []string) error {
	// TODO: handle missing flags
	if viper.GetString("accessToken") == "" {
		return errors.New("accessToken required")
	}
	if viper.GetString("baseUri") == "" {
		return errors.New("baseUri required")
	}

	client := projects.NewClient(
		viper.GetString("accessToken"),
		viper.GetString("baseUri"),
	)
	response, err := projects.ListProjects(
		context.Background(),
		client,
	)
	if err != nil {
		return err
	}

	// TODO: should this return response and let caller output or pass in stdout-ish interface?
	fmt.Fprintf(cmd.OutOrStdout(), string(response)+"\n")

	return nil
}
