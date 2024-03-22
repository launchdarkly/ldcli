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

func NewListCmd(client projects.Client) (listCmd, error) {
	cmd := &cobra.Command{
		Use:     "list",
		Short:   "Return a list of projects",
		Long:    "Return a list of projects",
		PreRunE: validate,
		RunE:    runList(client),
	}

	return listCmd{
		Cmd: cmd,
	}, nil
}

// validate ensures the flags are valid before using them.
// TODO: refactor with flags validate().
func validate(cmd *cobra.Command, args []string) error {
	_, err := url.ParseRequestURI(viper.GetString("baseUri"))
	if err != nil {
		return errors.ErrInvalidBaseURI
	}

	return nil
}

func runList(client projects.Client) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		response, err := client.List(
			context.Background(),
			viper.GetString("accessToken"),
			viper.GetString("baseUri"),
		)
		if err != nil {
			return err
		}

		fmt.Fprintf(cmd.OutOrStdout(), string(response)+"\n")

		return nil
	}
}
