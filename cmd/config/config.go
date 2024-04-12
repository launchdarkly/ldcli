package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"gopkg.in/yaml.v3"

	"ldcli/internal/config"
)

func NewConfigCmd() *cobra.Command {
	cmd := &cobra.Command{
		Long:  "View and modify specific configuration values",
		RunE:  run(),
		Short: "View and modify specific configuration values",
		Use:   "config",
	}

	cmd.Flags().Bool("list", false, "List configs")
	_ = viper.BindPFlag("list", cmd.Flags().Lookup("list"))

	cmd.Flags().Bool("set", false, "Set a config field to a value")
	_ = viper.BindPFlag("set", cmd.Flags().Lookup("set"))

	cmd.Flags().String("unset", "", "Unset a config field")
	_ = viper.BindPFlag("unset", cmd.Flags().Lookup("unset"))

	// TODO: running config should show help

	return cmd
}

func run() func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		switch {
		case viper.GetBool("list"):
			config, _, err := getConfig()
			if err != nil {
				return err
			}

			configJSON, err := json.Marshal(config)
			if err != nil {
				return err
			}

			fmt.Fprint(cmd.OutOrStdout(), string(configJSON)+"\n")
		case viper.GetBool("set"):
			// flag needs two arguments: a key and value
			if len(args)%2 != 0 {
				return errors.New("flag needs an argument: --set")
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
			return writeConfig(config.NewConfig(rawConfig), v, setKeyFn)
		case viper.IsSet("unset"):
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
		}

		return nil
	}
}

// get a map of the values in the config file.
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

// get a struct type of the values in the config file.
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

// write the values in config to the config file based on the filter function.
func writeConfig(
	conf config.ConfigFile,
	v *viper.Viper,
	filterFn func(key string, value interface{}, v *viper.Viper),
) error {
	newViper := viper.New()
	newViper.SetConfigFile(v.ConfigFileUsed())
	if err := v.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); ok {
			configPath := config.SetConfigPath()
			newViper.SetConfigFile(configPath + "/config.yml")
		}

		return err
	}

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

// ensures the viper instance has a config file written to the filesystem.
func getViperWithConfigFile() (*viper.Viper, error) {
	v := viper.GetViper()
	if err := v.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); ok {
			newViper := viper.New()
			configPath := config.SetConfigPath()
			newViper.SetConfigFile(configPath + "/config.yml")
			err = newViper.WriteConfigAs(configPath + "/config.yml")
			if err != nil {
				return nil, err
			}

			return newViper, nil
		}

		return nil, err
	}

	return v, nil
}
