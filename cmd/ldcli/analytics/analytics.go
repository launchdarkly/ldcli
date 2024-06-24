package analytics

import (
	"github.com/launchdarkly/ldcli/cmd/ldcli/cliflags"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

func CmdRunEventProperties(
	cmd *cobra.Command,
	name string,
	overrides map[string]interface{},
) map[string]interface{} {
	baseURI := viper.GetString(cliflags.BaseURIFlag)
	var flags []string
	cmd.Flags().Visit(func(f *pflag.Flag) {
		flags = append(flags, f.Name)
	})

	properties := map[string]interface{}{
		"name":   name,
		"action": cmd.CalledAs(),
		"flags":  flags,
	}
	if baseURI != cliflags.BaseURIDefault {
		properties["baseURI"] = baseURI
	}

	for k, v := range overrides {
		properties[k] = v
	}

	return properties
}
