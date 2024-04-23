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
	"ldcli/internal/output"
)

// Validate is a validator for commands to print an error when the user input is invalid.
func Validate() cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		rebindFlags(cmd, cmd.ValidArgs) // rebind flags before validating them below

		_, err := url.ParseRequestURI(viper.GetString(cliflags.BaseURIFlag))
		if err != nil {
			return CmdError(errs.ErrInvalidBaseURI, cmd.CommandPath())
		}

		err = cmd.ValidateRequiredFlags()
		if err != nil {
			return CmdError(err, cmd.CommandPath())
		}

		err = validateOutput(viper.GetString(cliflags.OutputFlag))
		if err != nil {
			return CmdError(err, cmd.CommandPath())
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

func validateOutput(outputFlag string) error {
	validKinds := map[string]struct{}{
		"json":      {},
		"plaintext": {},
	}
	_, ok := validKinds[outputFlag]
	if !ok {
		return output.ErrInvalidOutputKind
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
