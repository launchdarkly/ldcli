package utils

import (
	"ldcli/cmd/cliflags"

	"github.com/google/uuid"
	"github.com/spf13/viper"
)

func BuildCommandRunProperties(name string, action string, flags []string) map[string]interface{} {
	id := uuid.New()
	baseURI := viper.GetString(cliflags.BaseURIFlag)
	properties := map[string]interface{}{
		"name":   name,
		"action": action,
		"flags":  flags,
		"id":     id.String(),
	}
	if baseURI != "https://app.launchdarkly.com" {
		properties["baseURI"] = baseURI
	}
	return properties
}
