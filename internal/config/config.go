package config

import (
	"fmt"
	"strconv"

	"github.com/mitchellh/go-homedir"

	"ldcli/cmd/cliflags"
)

const Filename = ".ldcli-config.yml"

// ConfigFile represents the data stored in the config file.
type ConfigFile struct {
	AccessToken     string `json:"access-token,omitempty" yaml:"access-token,omitempty"`
	AnalyticsOptOut *bool  `json:"analytics-opt-out,omitempty" yaml:"analytics-opt-out,omitempty"`
	BaseURI         string `json:"base-uri,omitempty" yaml:"base-uri,omitempty"`
}

func NewConfig(rawConfig map[string]interface{}) ConfigFile {
	var accessToken string
	if rawConfig[cliflags.AccessTokenFlag] != nil {
		accessToken = rawConfig[cliflags.AccessTokenFlag].(string)
	}
	var analyticsOptOut bool
	if rawConfig[cliflags.AnalyticsOptOut] != nil {
		stringValue := rawConfig[cliflags.AnalyticsOptOut].(string)
		analyticsOptOut, _ = strconv.ParseBool(stringValue)
	}
	var baseURI string
	if rawConfig[cliflags.BaseURIFlag] != nil {
		baseURI = rawConfig[cliflags.BaseURIFlag].(string)
	}

	return ConfigFile{
		AccessToken:     accessToken,
		AnalyticsOptOut: &analyticsOptOut,
		BaseURI:         baseURI,
	}
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
