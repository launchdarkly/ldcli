package config

import (
	"fmt"
	"strconv"

	"github.com/mitchellh/go-homedir"

	"ldcli/cmd/cliflags"
	"ldcli/internal/errors"
	"ldcli/internal/output"
)

const Filename = ".ldcli-config.yml"

// ConfigFile represents the data stored in the config file.
type ConfigFile struct {
	AccessToken     string `json:"access-token,omitempty" yaml:"access-token,omitempty"`
	AnalyticsOptOut *bool  `json:"analytics-opt-out,omitempty" yaml:"analytics-opt-out,omitempty"`
	BaseURI         string `json:"base-uri,omitempty" yaml:"base-uri,omitempty"`
	Output          string `json:"output,omitempty" yaml:"output,omitempty"`
}

func NewConfig(rawConfig map[string]interface{}) (ConfigFile, error) {
	var (
		accessToken     string
		analyticsOptOut bool
		baseURI         string
		err             error
		outputKind      output.OutputKind
	)
	if rawConfig[cliflags.AccessTokenFlag] != nil {
		accessToken = rawConfig[cliflags.AccessTokenFlag].(string)
	}
	if rawConfig[cliflags.AnalyticsOptOut] != nil {
		maybeString, ok := rawConfig[cliflags.AnalyticsOptOut].(string)
		if ok {
			analyticsOptOut, _ = strconv.ParseBool(maybeString)
		} else {
			maybeBool, ok := rawConfig[cliflags.AnalyticsOptOut].(bool)
			if !ok {
				return ConfigFile{}, errors.NewError("analanalyticsOptOut must be true or false")
			}
			analyticsOptOut = maybeBool
		}
	}
	if rawConfig[cliflags.BaseURIFlag] != nil {
		baseURI = rawConfig[cliflags.BaseURIFlag].(string)
	}
	if rawConfig[cliflags.OutputFlag] != nil {
		outputKind, err = output.NewOutputKind(rawConfig[cliflags.OutputFlag].(string))
		if err != nil {
			return ConfigFile{}, err
		}
	}

	return ConfigFile{
		AccessToken:     accessToken,
		AnalyticsOptOut: &analyticsOptOut,
		BaseURI:         baseURI,
		Output:          outputKind.String(),
	}, nil
}

func GetConfigPath() string {
	home, err := homedir.Dir()
	if err != nil {
		return ""
	}

	return home
}

// GetConfigFile gets the full path to the config file.
func GetConfigFile() string {
	return fmt.Sprintf("%s/%s", GetConfigPath(), Filename)
}
