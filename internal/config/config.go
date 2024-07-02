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

// ConfigFile represents the data stored in the config file.
type ConfigFile struct {
	AccessToken     string `json:"access-token,omitempty" yaml:"access-token,omitempty"`
	AnalyticsOptOut *bool  `json:"analytics-opt-out,omitempty" yaml:"analytics-opt-out,omitempty"`
	BaseURI         string `json:"base-uri,omitempty" yaml:"base-uri,omitempty"`
	Environment     string `json:"environment,omitempty" yaml:"environment,omitempty"`
	Flag            string `json:"flag,omitempty" yaml:"flag,omitempty"`
	Output          string `json:"output,omitempty" yaml:"output,omitempty"`
	Project         string `json:"project,omitempty" yaml:"project,omitempty"`
}

type Config struct {
	AccessToken     string `json:"access-token,omitempty" yaml:"access-token,omitempty"`
	AnalyticsOptOut *bool  `json:"analytics-opt-out,omitempty" yaml:"analytics-opt-out,omitempty"`
	BaseURI         string `json:"base-uri,omitempty" yaml:"base-uri,omitempty"`
	Environment     string `json:"environment,omitempty" yaml:"environment,omitempty"`
	Flag            string `json:"flag,omitempty" yaml:"flag,omitempty"`
	Output          string `json:"output,omitempty" yaml:"output,omitempty"`
	Project         string `json:"project,omitempty" yaml:"project,omitempty"`
	filename        string
}

func New(filename string, readFile ReadFile) (Config, error) {
	data, err := readFile(filename)
	if err != nil {
		return Config{}, err
	}

	c := Config{
		filename: filename,
	}
	err = yaml.Unmarshal([]byte(data), &c)
	if err != nil {
		return Config{}, errors.NewError("config file is invalid yaml")
	}

	return c, nil
}

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

type ReadFile func(name string) ([]byte, error)

func NewConfigFromFile(f string, readFile ReadFile) (ConfigFile, error) {
	data, err := readFile(f)
	if err != nil {
		return ConfigFile{}, errors.NewError("could not read config file")
	}

	var c ConfigFile
	err = yaml.Unmarshal([]byte(data), &c)
	if err != nil {
		return ConfigFile{}, errors.NewError("config file is invalid yaml")
	}

	return c, nil
}

func NewConfigFile(rawConfig map[string]interface{}) (ConfigFile, error) {
	var (
		accessToken     string
		analyticsOptOut bool
		baseURI         string
		environment     string
		err             error
		flag            string
		outputKind      output.OutputKind
		project         string
	)
	if rawConfig[cliflags.AccessTokenFlag] != nil {
		accessToken = rawConfig[cliflags.AccessTokenFlag].(string)
	}
	if rawConfig[cliflags.AnalyticsOptOut] != nil {
		stringValue := fmt.Sprintf("%v", rawConfig[cliflags.AnalyticsOptOut])
		analyticsOptOut, err = strconv.ParseBool(stringValue)
		if err != nil {
			return ConfigFile{}, errors.NewError("analytics-opt-out must be true or false")
		}
	}
	if rawConfig[cliflags.BaseURIFlag] != nil {
		baseURI = rawConfig[cliflags.BaseURIFlag].(string)
	}
	if rawConfig[cliflags.EnvironmentFlag] != nil {
		environment = rawConfig[cliflags.EnvironmentFlag].(string)
	}
	if rawConfig[cliflags.FlagFlag] != nil {
		flag = rawConfig[cliflags.FlagFlag].(string)
	}
	if rawConfig[cliflags.OutputFlag] != nil {
		outputKind, err = output.NewOutputKind(rawConfig[cliflags.OutputFlag].(string))
		if err != nil {
			return ConfigFile{}, err
		}
	}
	if rawConfig[cliflags.ProjectFlag] != nil {
		project = rawConfig[cliflags.ProjectFlag].(string)
	}

	return ConfigFile{
		AccessToken:     accessToken,
		AnalyticsOptOut: &analyticsOptOut,
		BaseURI:         baseURI,
		Environment:     environment,
		Flag:            flag,
		Output:          outputKind.String(),
		Project:         project,
	}, nil
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
	config, err := NewConfigFromFile(filename, os.ReadFile)
	if err != nil {
		return false, err
	}
	if config.AccessToken != "" {
		return false, errors.NewError("Your access token is already set. Remove it from the config if you wish to reset it.")
	}

	return true, nil
}

func Update(kvs []string, filename string) (ConfigFile, error) {
	if len(kvs)%2 != 0 {
		return ConfigFile{}, errors.NewError("flag needs an argument: --set")
	}
	for i := 0; i < len(kvs)-1; i += 2 {
		_, ok := cliflags.AllFlagsHelp()[kvs[i]]
		if !ok {
			return ConfigFile{}, errors.NewError(fmt.Sprintf("%s is not a valid configuration option", kvs[i]))
		}
	}

	data, err := os.ReadFile(filename)
	if err != nil {
		return ConfigFile{}, err
	}
	rawConfig := make(map[string]interface{}, 0)
	err = yaml.Unmarshal([]byte(data), &rawConfig)
	if err != nil {
		return ConfigFile{}, err
	}

	// add arg pairs to config where each argument is --set arg1 val1 --set arg2 val2
	for i, a := range kvs {
		if i%2 == 0 {
			rawConfig[a] = struct{}{}
		} else {
			rawConfig[kvs[i-1]] = a
		}
	}

	for key, value := range rawConfig {
		rawConfig[key] = value
	}

	configFile, err := NewConfigFile(rawConfig)
	if err != nil {
		return ConfigFile{}, err
	}

	return configFile, nil
}
