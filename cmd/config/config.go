package config

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"gopkg.in/yaml.v3"

	cmdAnalytics "ldcli/cmd/analytics"
	"ldcli/cmd/cliflags"
	"ldcli/internal/analytics"
	"ldcli/internal/config"
	"ldcli/internal/errors"
	"ldcli/internal/output"
)

const (
	ListFlag  = "list"
	SetFlag   = "set"
	UnsetFlag = "unset"
)

func NewConfigCmd(analyticsTracker analytics.Tracker) *cobra.Command {
	cmd := &cobra.Command{
		Long:  "View and modify specific configuration values",
		RunE:  run(),
		Short: "View and modify specific configuration values",
		Use:   "config",
		PreRun: func(cmd *cobra.Command, args []string) {
			analyticsTracker.SendCommandRunEvent(
				viper.GetString(cliflags.AccessTokenFlag),
				viper.GetString(cliflags.BaseURIFlag),
				viper.GetBool(cliflags.AnalyticsOptOut),
				cmdAnalytics.CmdRunEventProperties(cmd, "config"),
			)
		},
	}

	cmd.Flags().Bool(ListFlag, false, "List configs")
	_ = viper.BindPFlag(ListFlag, cmd.Flags().Lookup(ListFlag))
	cmd.Flags().Bool(SetFlag, false, "Set a config field to a value")
	_ = viper.BindPFlag(SetFlag, cmd.Flags().Lookup(SetFlag))
	cmd.Flags().String(UnsetFlag, "", "Unset a config field")
	_ = viper.BindPFlag(UnsetFlag, cmd.Flags().Lookup(UnsetFlag))

	return cmd
}

func run() func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		switch {
		case viper.GetBool(ListFlag):
			config, _, err := getConfig()
			if err != nil {
				return err
			}

			configJSON, err := json.Marshal(config)
			if err != nil {
				return err
			}

			output, err := output.CmdOutputSingular(
				viper.GetString(cliflags.OutputFlag),
				configJSON,
				output.ConfigPlaintextOutputFn,
			)
			if err != nil {
				return err
			}

			fmt.Fprintf(cmd.OutOrStdout(), output+"\n")
		case viper.GetBool(SetFlag):
			// flag needs two arguments: a key and value
			if len(args)%2 != 0 {
				return errors.NewError("flag needs an argument: --set")
			}

			rawConfig, v, err := getRawConfig()
			if err != nil {
				return err
			}

			// add arg pairs to config
			// where each argument is --set arg1 val1 --set arg2 val2
			for i, a := range args {
				if i%2 == 0 {
					rawConfig[a] = struct{}{}
				} else {
					rawConfig[args[i-1]] = a
				}
			}

			setKeyFn := func(key string, value interface{}, v *viper.Viper) {
				v.Set(key, value)
			}

			configFile, err := config.NewConfig(rawConfig)
			if err != nil {
				return errors.NewError(err.Error())
			}

			return writeConfig(configFile, v, setKeyFn)
		case viper.IsSet(UnsetFlag):
			config, v, err := getConfig()
			if err != nil {
				return err
			}

			unsetKeyFn := func(key string, value interface{}, v *viper.Viper) {
				if key != viper.GetString("unset") {
					v.Set(key, value)
				}
			}

			return writeConfig(config, v, unsetKeyFn)
		default:
			return cmd.Help()
		}

		return nil
	}
}

// getConfig builds a struct type of the values in the config file.
func getConfig() (config.ConfigFile, *viper.Viper, error) {
	v, err := getViperWithConfigFile()
	if err != nil {
		return config.ConfigFile{}, nil, err
	}

	data, err := os.ReadFile(v.ConfigFileUsed())
	if err != nil {
		return config.ConfigFile{}, nil, err
	}

	var c config.ConfigFile
	err = yaml.Unmarshal([]byte(data), &c)
	if err != nil {
		return config.ConfigFile{}, nil, err
	}

	return c, v, nil
}

// getRawConfig builds a map of the values in the config file.
func getRawConfig() (map[string]interface{}, *viper.Viper, error) {
	v, err := getViperWithConfigFile()
	if err != nil {
		return nil, nil, err
	}

	data, err := os.ReadFile(v.ConfigFileUsed())
	if err != nil {
		return nil, nil, err
	}

	config := make(map[string]interface{}, 0)
	err = yaml.Unmarshal([]byte(data), &config)
	if err != nil {
		return nil, nil, err
	}

	return config, v, nil
}

// getViperWithConfigFile ensures the viper instance has a config file written to the filesystem.
// We want to write the file when someone runs a config command.
func getViperWithConfigFile() (*viper.Viper, error) {
	v := viper.GetViper()
	if err := v.ReadInConfig(); err != nil {
		newViper := viper.New()
		newViper.SetConfigFile(config.GetConfigFile())
		err = newViper.WriteConfig()
		if err != nil {
			return nil, err
		}

		return newViper, nil
	}

	return v, nil
}

// writeConfig writes the values in config to the config file based on the filter function.
func writeConfig(
	conf config.ConfigFile,
	v *viper.Viper,
	filterFn func(key string, value interface{}, v *viper.Viper),
) error {
	// create a new viper instance since the existing one has the command name and flags already set,
	// and these will get written to the config file.
	newViper := viper.New()
	newViper.SetConfigFile(v.ConfigFileUsed())

	configYAML, err := yaml.Marshal(conf)
	if err != nil {
		return err
	}
	rawConfig := make(map[string]interface{})
	err = yaml.Unmarshal(configYAML, &rawConfig)
	if err != nil {
		return err
	}

	for key, value := range rawConfig {
		filterFn(key, value, newViper)
	}

	err = newViper.WriteConfig()
	if err != nil {
		return err
	}

	return nil
}
