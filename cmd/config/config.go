package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"gopkg.in/yaml.v3"
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

type config struct {
	AccessToken string `json:"access-token" yaml:"access-token"`
	BaseURI     string `json:"base-uri" yaml:"base-uri"`
}

func run() func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		switch {
		case viper.GetBool("list"):
			config, err := getConfig()
			if err != nil {
				return err
			}

			configJSON, err := json.Marshal(config)
			if err != nil {
				return err
			}

			fmt.Fprint(cmd.OutOrStdout(), string(configJSON)+"\n")
		case viper.GetBool("set"):
			if len(args)%2 != 0 {
				return errors.New("odd amount of arguments")
			}

			config, err := getConfig()
			if err != nil {
				return err
			}

			// add arg pairs to config
			// where each argument is --set arg1 val1 --set arg2 val2
			for i, a := range args {
				if i%2 == 0 {
					config[a] = struct{}{}
				} else {
					config[args[i-1]] = a
				}
			}

			return writeConfig(
				config,
				func(key string, value interface{}, v *viper.Viper) {
					v.Set(key, value)
				},
			)
		case viper.IsSet("unset"):
			config, err := getConfig()
			if err != nil {
				return err
			}

			return writeConfig(
				config,
				func(key string, value interface{}, v *viper.Viper) {
					if key != viper.GetString("unset") {
						v.Set(key, value)
					}
				},
			)
		default:
		}

		return nil
	}
}

func getConfig() (map[string]interface{}, error) {
	data, err := os.ReadFile(viper.ConfigFileUsed())
	if err != nil {
		return nil, err
	}

	config := make(map[string]interface{}, 0)
	err = yaml.Unmarshal([]byte(data), &config)
	if err != nil {
		return nil, err
	}

	return config, nil
}

func writeConfig(
	config map[string]interface{},
	fn func(key string, value interface{}, v *viper.Viper),
) error {
	v := viper.New()
	v.SetConfigFile(viper.GetViper().ConfigFileUsed())
	for key, value := range config {
		fn(key, value, v)
	}

	err := v.WriteConfig()
	if err != nil {
		return err
	}

	return nil
}
