package validators

import (
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"

	"github.com/launchdarkly/ldcli/cmd/ldcli/cliflags"
	errs "github.com/launchdarkly/ldcli/internal/errors"
	"github.com/launchdarkly/ldcli/internal/output"
)

// Validate is a validator for commands to print an error when the user input is invalid.
func Validate() cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		rebindFlags(cmd, cmd.ValidArgs) // rebind flags before validating them below

		_, err := url.ParseRequestURI(viper.GetString(cliflags.BaseURIFlag))
		if err != nil {
			return CmdError(errs.ErrInvalidBaseURI, cmd.CommandPath(), "")
		}

		err = cmd.ValidateRequiredFlags()
		if err != nil {
			return CmdError(err, cmd.CommandPath(), viper.GetString(cliflags.BaseURIFlag))
		}

		err = validateOutput(viper.GetString(cliflags.OutputFlag))
		if err != nil {
			return CmdError(err, cmd.CommandPath(), viper.GetString(cliflags.BaseURIFlag))
		}

		return nil
	}
}

func CmdError(err error, commandPath string, baseURI string) error {
	errorMessage := err.Error() + "."

	// show additional help if a missing flag can be set in the config file
	if strings.Contains(err.Error(), cliflags.AccessTokenFlag) {
		errorMessage += "\n\n"
		if baseURI != "" {
			errorMessage += errs.AccessTokenInvalidErrMessage(baseURI)
			errorMessage += "\n"
		}
		errorMessage += fmt.Sprintf("Use `ldcli config --set %s <value>` to configure the value to persist across CLI commands.\n\n", cliflags.AccessTokenFlag)
	} else {
		errorMessage += " "
	}
	errorMessage += fmt.Sprintf("See `%s --help` for supported flags and usage.", commandPath)

	return errors.New(errorMessage)
}

func validateOutput(outputFlag string) error {
	_, err := output.NewOutputKind(outputFlag)
	if err != nil {
		return err
	}

	return nil
}

// rebindFlags sets the command's flags based on the values stored in viper because they may not
// be set yet when they (the flags) are set from environment variables or a configuration file.
func rebindFlags(cmd *cobra.Command, _ []string) {
	_ = viper.BindPFlags(cmd.Flags())
	cmd.Flags().VisitAll(func(f *pflag.Flag) {
		if viper.IsSet(f.Name) && viper.GetString(f.Name) != "" {
			_ = cmd.Flags().Set(f.Name, viper.GetString(f.Name))
		}
	})
}
