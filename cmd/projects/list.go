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
	Cmd    *cobra.Command
	client projects.Client
}

func NewListCmd(client projects.Client) (listCmd, error) {
	cmd := &cobra.Command{
		Use:     "list",
		Short:   "Return a list of projects",
		Long:    "Return a list of projects",
		PreRunE: validate,
		RunE:    runList,
	}

	return listCmd{
		Cmd:    cmd,
		client: client,
	}, nil
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
	response, err := client.List(context.Background())
	if err != nil {
		return err
	}

	fmt.Fprintf(cmd.OutOrStdout(), string(response)+"\n")

	return nil
}
