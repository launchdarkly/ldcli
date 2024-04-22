package validators

import (
	"errors"
	"fmt"
	"net/url"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"

	"ldcli/cmd/cliflags"
	errs "ldcli/internal/errors"
)

// Validate is a validator for commands to print an error when the user input is invalid.
func Validate() cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		rebindFlags(cmd, cmd.ValidArgs) // rebind flags before validating them below
		commandPath := getCommandPath(cmd)

		_, err := url.ParseRequestURI(viper.GetString(cliflags.BaseURIFlag))
		if err != nil {
			return CmdError(errs.ErrInvalidBaseURI, commandPath)
		}

		err = cmd.ValidateRequiredFlags()
		if err != nil {
			return CmdError(err, commandPath)
		}

		err = validateOutput(viper.GetString(cliflags.OutputFlag))
		if err != nil {
			return CmdError(err, commandPath)
		}

		return nil
	}
}

func CmdError(err error, commandPath string) error {
	errorMessage := fmt.Sprintf(
		"%s. See `%s --help` for supported flags and usage.",
		err.Error(),
		commandPath,
	)

	return errors.New(errorMessage)
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

func validateOutput(outputFlag string) error {
	validKinds := map[string]struct{}{
		"json":      {},
		"plaintext": {},
	}
	_, ok := validKinds[outputFlag]
	if !ok {
		return errors.New("output is invalid")
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
