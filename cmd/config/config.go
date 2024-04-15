package config

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"gopkg.in/yaml.v3"

	"ldcli/internal/config"
)

const (
	ListFlag  = "list"
	SetFlag   = "set"
	UnsetFlag = "unset"
)

func NewConfigCmd() *cobra.Command {
	cmd := &cobra.Command{
		Long:  "View and modify specific configuration values",
		RunE:  run(),
		Short: "View and modify specific configuration values",
		Use:   "config",
	}

	cmd.Flags().Bool(ListFlag, false, "List configs")
	_ = viper.BindPFlag(ListFlag, cmd.Flags().Lookup(ListFlag))
	cmd.Flags().Bool(SetFlag, false, "Set a config field to a value")
	_ = viper.BindPFlag(SetFlag, cmd.Flags().Lookup(SetFlag))
	cmd.Flags().String(UnsetFlag, "", "Unset a config field")
	_ = viper.BindPFlag(UnsetFlag, cmd.Flags().Lookup(UnsetFlag))

	// TODO: running config should show help

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

			fmt.Fprint(cmd.OutOrStdout(), string(configJSON)+"\n")
		case viper.GetBool(SetFlag):
			fmt.Fprintln(cmd.OutOrStdout(), "called --set flag")
		case viper.GetBool(UnsetFlag):
			fmt.Fprintln(cmd.OutOrStdout(), "called --unset flag")
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

// getViperWithConfigFile ensures the viper instance has a config file written to the filesystem.
// We want to write the file when someone runs a config command.
func getViperWithConfigFile() (*viper.Viper, error) {
	v := viper.GetViper()
	if err := v.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); ok {
			newViper := viper.New()
			newViper.SetConfigFile(config.GetConfigFile())
			err = newViper.WriteConfigAs(config.GetConfigFile())
			if err != nil {
				return nil, err
			}

			return newViper, nil
		}

		return nil, err
	}

	return v, nil
}
