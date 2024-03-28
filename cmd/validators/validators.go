package validators

import (
	"errors"
	"fmt"
	"net/url"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"ldcli/cmd/cliflags"
	errs "ldcli/internal/errors"
)

// Validate is a validator for commands to print an error when the user input is invalid.
func Validate() cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		commandPath := getCommandPath(cmd)

		_, err := url.ParseRequestURI(viper.GetString(cliflags.BaseURIFlag))
		if err != nil {
			errorMessage := fmt.Sprintf(
				"%s. See `%s --help` for supported flags and usage.",
				errs.ErrInvalidBaseURI,
				commandPath,
			)
			return errors.New(errorMessage)
		}

		err = cmd.ValidateRequiredFlags()
		if err != nil {
			errorMessage := fmt.Sprintf(
				"%s. See `%s --help` for supported flags and usage.",
				err.Error(),
				commandPath,
			)

			return errors.New(errorMessage)
		}

		return nil
	}
}

func getCommandPath(cmd *cobra.Command) string {
	var commandPath string
	if cmd.Annotations["scope"] == "plugin" {
		commandPath = fmt.Sprintf("stripe %s", cmd.CommandPath())
	} else {
		commandPath = cmd.CommandPath()
	}

	return commandPath
}
