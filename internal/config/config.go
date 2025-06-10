package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"github.com/mitchellh/go-homedir"
	"gopkg.in/yaml.v3"

	"github.com/launchdarkly/ldcli/cmd/cliflags"
	"github.com/launchdarkly/ldcli/internal/errors"
	"github.com/launchdarkly/ldcli/internal/output"
)

const Filename = ".ldcli-config.yml"

type ReadFile func(name string) ([]byte, error)

// Config represents the data stored in the config file.
type Config struct {
	AccessToken     string `json:"access-token,omitempty" yaml:"access-token,omitempty"`
	AnalyticsOptOut *bool  `json:"analytics-opt-out,omitempty" yaml:"analytics-opt-out,omitempty"`
	BaseURI         string `json:"base-uri,omitempty" yaml:"base-uri,omitempty"`
	DatabasePath    string `json:"database-path,omitempty" yaml:"database-path,omitempty"`
	DevStreamURI    string `json:"dev-stream-uri,omitempty" yaml:"dev-stream-uri,omitempty"`
	Environment     string `json:"environment,omitempty" yaml:"environment,omitempty"`
	Flag            string `json:"flag,omitempty" yaml:"flag,omitempty"`
	Output          string `json:"output,omitempty" yaml:"output,omitempty"`
	Project         string `json:"project,omitempty" yaml:"project,omitempty"`
}

func New(filename string, readFile ReadFile) (Config, error) {
	data, err := readFile(filename)
	if err != nil {
		return Config{}, errors.NewError(
			fmt.Sprintf("unable to open config file. The file or directory may not exist: %s", filename),
		)
	}

	var c Config
	err = yaml.Unmarshal([]byte(data), &c)
	if err != nil {
		return Config{}, errors.NewError("config file is invalid yaml")
	}

	return c, nil
}

// Update validates the updating fields and sets them on the Config. It returns the updated fields
// in addition to the Config.
func (c Config) Update(kvs []string) (Config, []string, error) {
	updatedFields := make([]string, 0, len(kvs))

	if len(kvs)%2 != 0 {
		return Config{}, updatedFields, errors.NewError("flag needs an argument: --set")
	}
	for i := 0; i < len(kvs)-1; i += 2 {
		// TODO: move this list to this package?
		_, ok := cliflags.AllFlagsHelp()[kvs[i]]
		if !ok {
			return Config{}, updatedFields, errors.NewError(fmt.Sprintf("%s is not a valid configuration option", kvs[i]))
		}
	}

	var currField string
	for i, v := range kvs {
		if i%2 == 0 {
			currField = v
			updatedFields = append(updatedFields, v)
		} else {
			switch currField {
			case cliflags.AccessTokenFlag:
				c.AccessToken = v
			case cliflags.AnalyticsOptOut:
				val, err := strconv.ParseBool(v)
				if err != nil {
					return Config{}, nil, errors.NewError("analytics-opt-out must be true or false")
				}

				c.AnalyticsOptOut = &val
			case cliflags.BaseURIFlag:
				c.BaseURI = v
			case cliflags.DatabasePathFlag:
				c.DatabasePath = v
			case cliflags.DevStreamURIFlag:
				c.DevStreamURI = v
			case cliflags.EnvironmentFlag:
				c.Environment = v
			case cliflags.FlagFlag:
				c.Flag = v
			case cliflags.OutputFlag:
				val, err := output.NewOutputKind(v)
				if err != nil {
					return Config{}, nil, err
				}
				c.Output = val.String()
			case cliflags.ProjectFlag:
				c.Project = v
			}
		}
	}

	return c, updatedFields, nil
}

// Remove validates the key exists but doesn't do anything else since we unset the value when
// writing to disk.
func (c Config) Remove(key string) (Config, error) {
	_, ok := cliflags.AllFlagsHelp()[key]
	if !ok {
		return Config{}, errors.NewError(fmt.Sprintf("%s is not a valid configuration option", key))
	}

	return c, nil
}

// GetConfigFile gets the full path to the config file.
func GetConfigFile() string {
	configPath := os.Getenv("XDG_CONFIG_HOME")
	if configPath == "" {
		home, err := homedir.Dir()
		if err != nil {
			return ""
		}
		configPath = filepath.Join(home, ".config")
	}
	configFilePath := filepath.Join(configPath, "ldcli")

	return filepath.Join(configFilePath, "config.yml")
}

func AccessTokenIsSet(filename string) (bool, error) {
	config, err := New(filename, os.ReadFile)
	if err != nil {
		return false, err
	}
	if config.AccessToken != "" {
		return false, errors.NewError("Your access token is already set. Remove it from the config if you wish to reset it.")
	}

	return true, nil
}
