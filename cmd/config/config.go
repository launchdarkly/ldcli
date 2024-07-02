package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"gopkg.in/yaml.v3"

	cmdAnalytics "github.com/launchdarkly/ldcli/cmd/analytics"
	"github.com/launchdarkly/ldcli/cmd/cliflags"
	"github.com/launchdarkly/ldcli/internal/analytics"
	"github.com/launchdarkly/ldcli/internal/config"
	errs "github.com/launchdarkly/ldcli/internal/errors"
	"github.com/launchdarkly/ldcli/internal/output"
)

const (
	ListFlag  = "list"
	SetFlag   = "set"
	UnsetFlag = "unset"
)

type ConfigCmd struct {
	cmd        *cobra.Command
	helpCalled bool
}

func (cmd ConfigCmd) Cmd() *cobra.Command {
	return cmd.cmd
}

func (cmd ConfigCmd) HelpCalled() bool {
	return cmd.helpCalled
}

func NewConfigCmd(service config.Service, analyticsTrackerFn analytics.TrackerFn) *ConfigCmd {
	cmd := &cobra.Command{
		Long:  "View and modify specific configuration values",
		RunE:  run(service),
		Short: "View and modify specific configuration values",
		Use:   "config",
		PreRun: func(cmd *cobra.Command, args []string) {
			// only track event if there are flags
			if len(os.Args[1:]) > 1 {
				analyticsTrackerFn(
					viper.GetString(cliflags.AccessTokenFlag),
					viper.GetString(cliflags.BaseURIFlag),
					viper.GetBool(cliflags.AnalyticsOptOut),
				).SendCommandRunEvent(cmdAnalytics.CmdRunEventProperties(cmd, "config", nil))
			}
		},
	}
	configCmd := ConfigCmd{
		cmd: cmd,
	}

	helpFun := cmd.HelpFunc()
	cmd.SetHelpFunc(func(cmd *cobra.Command, args []string) {
		configCmd.helpCalled = true

		var sb strings.Builder
		sb.WriteString("\n\nSupported settings:\n")
		writeAlphabetizedFlags(&sb)
		cmd.Long += sb.String()

		analyticsTrackerFn(
			viper.GetString(cliflags.AccessTokenFlag),
			viper.GetString(cliflags.BaseURIFlag),
			viper.GetBool(cliflags.AnalyticsOptOut),
		).SendCommandRunEvent(cmdAnalytics.CmdRunEventProperties(
			cmd,
			"config",
			map[string]interface{}{
				"action": "help",
			},
		))

		helpFun(cmd, args)
	})

	cmd.Flags().Bool(ListFlag, false, "List configs")
	_ = viper.BindPFlag(ListFlag, cmd.Flags().Lookup(ListFlag))
	cmd.Flags().Bool(SetFlag, false, "Set a config field to a value")
	_ = viper.BindPFlag(SetFlag, cmd.Flags().Lookup(SetFlag))
	cmd.Flags().String(UnsetFlag, "", "Unset a config field")
	_ = viper.BindPFlag(UnsetFlag, cmd.Flags().Lookup(UnsetFlag))

	return &configCmd
}

func run(service config.Service) func(*cobra.Command, []string) error {
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
			conf, err := config.New(viper.ConfigFileUsed(), os.ReadFile)
			if err != nil {
				return newErr(err.Error())
			}
			conf, updatedFields, err := conf.Update(args)
			if err != nil {
				return newErr(err.Error())
			}
			if isUpdatingAccessToken(updatedFields) {
				if !service.VerifyAccessToken(conf.AccessToken, viper.GetString(cliflags.BaseURIFlag)) {
					errorMessage := fmt.Sprintf("%s is invalid. ", cliflags.AccessTokenFlag)
					errorMessage += errs.AccessTokenInvalidErrMessage(viper.GetString(cliflags.BaseURIFlag))
					err := errors.New(errorMessage)

					return newErr(err.Error())
				}
			}
			err = Write(conf, SetKey)
			if err != nil {
				return newErr(err.Error())
			}

			output, err := outputSetAction(updatedFields)
			if err != nil {
				return newErr(err.Error())
			}

			fmt.Fprintf(cmd.OutOrStdout(), output+"\n")
		case viper.IsSet(UnsetFlag):
			_, ok := cliflags.AllFlagsHelp()[viper.GetString(UnsetFlag)]
			if !ok {
				return newErr(fmt.Sprintf("%s is not a valid configuration option", viper.GetString(UnsetFlag)))
			}

			config, v, err := getConfig()
			if err != nil {
				return newCmdErr(err)
			}

			err = writeConfigFile(config, v, UnsetKey)
			if err != nil {
				return newCmdErr(err)
			}

			output, err := outputUnsetAction(viper.GetString(UnsetFlag))
			if err != nil {
				return err
			}

			fmt.Fprintf(cmd.OutOrStdout(), output+"\n")
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

	c, err := config.NewConfigFromFile(v.ConfigFileUsed(), os.ReadFile)
	if err != nil {
		return config.ConfigFile{}, nil, err
	}

	return c, v, nil
}

// Write takes a Config and lets viper write it to the config file.
func Write(conf config.Config, filterFn filterFn) error {
	v, err := getViperWithConfigFile()
	if err != nil {
		return err
	}

	return writeConfig(conf, v, filterFn)
}

// filterFn decides how to write the in-memory config file to disk.
type filterFn func(key string, value interface{}, v *viper.Viper)

func SetKey(key string, value interface{}, v *viper.Viper) {
	v.Set(key, value)
}

func UnsetKey(key string, value interface{}, v *viper.Viper) {
	if key != viper.GetString("unset") {
		v.Set(key, value)
	}
}

// getViperWithConfigFile ensures the viper instance has a config file written to the filesystem.
// We want to write the file when someone runs a config command.
func getViperWithConfigFile() (*viper.Viper, error) {
	v := viper.GetViper()
	if err := v.ReadInConfig(); err != nil {
		newViper := viper.New()
		configFile := config.GetConfigFile()
		newViper.SetConfigType("yml")
		newViper.SetConfigFile(configFile)

		err = newViper.WriteConfig()
		if err != nil {
			return nil, err
		}

		return newViper, nil
	}

	return v, nil
}

// writeConfigFile writes the values in config to the config file based on the filter function.
func writeConfigFile(
	conf config.ConfigFile,
	v *viper.Viper,
	filterFn filterFn,
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

// writeConfig writes the values in config to the config file based on the filter function.
func writeConfig(
	conf config.Config,
	v *viper.Viper,
	filterFn filterFn,
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

func newCmdErr(err error) error {
	return output.NewCmdOutputError(err, viper.GetString(cliflags.OutputFlag))
}

func newErr(message string) error {
	err := errs.NewError(
		fmt.Sprintf(
			`{
				"message": %q
			}`,
			message,
		),
	)

	return newCmdErr(err)
}

func writeAlphabetizedFlags(sb *strings.Builder) {
	flags := make([]string, 0, len(cliflags.AllFlagsHelp()))
	for f := range cliflags.AllFlagsHelp() {
		flags = append(flags, f)
	}
	sort.Strings(flags)
	for _, flag := range flags {
		sb.WriteString(fmt.Sprintf("- `%s`: %s\n", flag, cliflags.AllFlagsHelp()[flag]))
	}
}

func outputSetAction(newFields []string) (string, error) {
	fields := struct {
		Items []string `json:"items"`
	}{
		Items: newFields,
	}
	fieldsJSON, _ := json.Marshal(fields)
	output, err := output.CmdOutput("update", viper.GetString(cliflags.OutputFlag), fieldsJSON)
	if err != nil {
		return "", errs.NewError(err.Error())
	}

	return output, nil
}

func outputUnsetAction(newField string) (string, error) {
	field := struct {
		Key string `json:"key"`
	}{
		Key: newField,
	}
	fieldJSON, _ := json.Marshal(field)
	output, err := output.CmdOutput("delete", viper.GetString(cliflags.OutputFlag), fieldJSON)
	if err != nil {
		return "", errs.NewError(err.Error())
	}

	return output, nil
}

func isUpdatingAccessToken(fields []string) bool {
	var updatingAccessToken bool
	for _, f := range fields {
		if f == cliflags.AccessTokenFlag {
			updatingAccessToken = true
			break
		}
	}

	return updatingAccessToken
}
