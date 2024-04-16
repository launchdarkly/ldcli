package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/mitchellh/go-homedir"

	"ldcli/cmd/cliflags"
)

const FileName = "config.yml"

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
	configPath := os.Getenv("XDG_CONFIG_HOME")
	if configPath == "" {
		home, err := homedir.Dir()
		if err != nil {
			return ""
		}
		configPath = filepath.Join(home, ".config")
	}

	return filepath.Join(configPath, "ldcli")
}

// GetConfigFile gets the full path to the config file.
// TODO: we should ensure this works on windows, linux, macos.
func GetConfigFile() string {
	return fmt.Sprintf("%s/%s", GetConfigPath(), FileName)
}
