package config

import (
	"fmt"

	"github.com/mitchellh/go-homedir"

	"ldcli/cmd/cliflags"
)

const Filename = ".ldcli-config.yml"

// ConfigFile represents the data stored in the config file.
type ConfigFile struct {
	AccessToken string `json:"access-token,omitempty" yaml:"access-token,omitempty"`
	BaseURI     string `json:"base-uri,omitempty" yaml:"base-uri,omitempty"`
}

func NewConfig(rawConfig map[string]interface{}) ConfigFile {
	var accessToken string
	if rawConfig[cliflags.AccessTokenFlag] != nil {
		accessToken = rawConfig[cliflags.AccessTokenFlag].(string)
	}
	var baseURI string
	if rawConfig[cliflags.BaseURIFlag] != nil {
		baseURI = rawConfig[cliflags.BaseURIFlag].(string)
	}

	return ConfigFile{
		AccessToken: accessToken,
		BaseURI:     baseURI,
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
