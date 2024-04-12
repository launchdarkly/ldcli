package config

import (
	"ldcli/cmd/cliflags"
	"os"
	"path/filepath"

	"github.com/mitchellh/go-homedir"
)

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

func SetConfigPath() string {
	configPath := os.Getenv("XDG_CONFIG_HOME")
	if configPath == "" {
		home, err := homedir.Dir()
		if err != nil {
			return ""
		}
		configPath = filepath.Join(home, ".config")
	}
	configPath = filepath.Join(configPath, "ldcli")

	return configPath
}
