package projects

import (
	"context"
	"fmt"
	"net/url"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"ld-cli/internal/errors"
	"ld-cli/internal/projects"
)

type listCmd struct {
	Cmd *cobra.Command
}

func NewListCmd() listCmd {
	cmd := &cobra.Command{
		Use:     "list",
		Short:   "Return a list of projects",
		Long:    "Return a list of projects",
		PreRunE: validate,
		RunE:    runList,
	}

	return listCmd{
		Cmd: cmd,
	}
}

// validate ensures the flags are valid before using them.
func validate(cmd *cobra.Command, args []string) error {
	_, err := url.ParseRequestURI(viper.GetString("baseUri"))
	if err != nil {
		return errors.ErrInvalidBaseURI
	}

	return nil
}

// runList fetches a list of projects.
func runList(cmd *cobra.Command, args []string) error {
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

	fmt.Fprintf(cmd.OutOrStdout(), string(response)+"\n")

	return nil
}
