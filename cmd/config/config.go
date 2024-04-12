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
			var config config
			data, err := os.ReadFile(viper.ConfigFileUsed())
			if err != nil {
				return err
			}

			err = yaml.Unmarshal([]byte(data), &config)
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

			newConfigValues := make(map[string]interface{}, 0)
			for i, a := range args {
				if i%2 == 0 {
					newConfigValues[a] = struct{}{}
				} else {
					newConfigValues[args[i-1]] = a
				}
			}

			data, err := os.ReadFile(viper.ConfigFileUsed())
			if err != nil {
				return err
			}

			config := make(map[string]interface{}, 0)
			err = yaml.Unmarshal([]byte(data), &config)
			if err != nil {
				return err
			}
			for k, v := range newConfigValues {
				config[k] = v
			}

			v := viper.New()
			v.SetConfigFile(viper.GetViper().ConfigFileUsed())
			for key, value := range config {
				v.Set(key, value)
			}
			err = v.WriteConfig()
			if err != nil {
				return err
			}
		case viper.IsSet("unset"):
			data, err := os.ReadFile(viper.ConfigFileUsed())
			if err != nil {
				return err
			}

			config := make(map[string]interface{}, 0)
			err = yaml.Unmarshal([]byte(data), &config)
			if err != nil {
				return err
			}

			v := viper.New()
			v.SetConfigFile(viper.GetViper().ConfigFileUsed())
			for key, value := range config {
				if key != viper.GetString("unset") {
					v.Set(key, value)
				}
			}
			err = v.WriteConfig()
			if err != nil {
				return err
			}
		default:
		}

		return nil
	}
}
