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
	Flag            string `json:"flag,omitempty" yaml:"flag,omitempty"`
	Environment     string `json:"environment,omitempty" yaml:"environment,omitempty"`
	Output          string `json:"output,omitempty" yaml:"output,omitempty"`
	Project         string `json:"project,omitempty" yaml:"project,omitempty"`
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

func NewConfig(rawConfig map[string]interface{}) (ConfigFile, error) {
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
