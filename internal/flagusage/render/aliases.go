package render

import (
	_ "embed"
	"encoding/json"
	"os"

	"github.com/adrg/xdg"
)

// embeddedAliases holds the default env-key → display-name map, kept in a JSON
// file (aliases.json) for easy editing rather than buried in code. Users can
// override/extend it via $XDG_CONFIG_HOME/ldcli/flagusage-aliases.json.
//
//go:embed aliases.json
var embeddedAliases []byte

// envAliases is loaded lazily on first use (nil = not yet loaded). Tests may set
// it directly to control aliasing.
var envAliases map[string]string

// loadEnvAliases merges the embedded defaults with an optional user file
// (user entries win).
func loadEnvAliases() map[string]string {
	m := map[string]string{}
	_ = json.Unmarshal(embeddedAliases, &m)
	if path, err := xdg.SearchConfigFile("ldcli/flagusage-aliases.json"); err == nil {
		if data, err := os.ReadFile(path); err == nil {
			var u map[string]string
			if json.Unmarshal(data, &u) == nil {
				for k, v := range u {
					m[k] = v
				}
			}
		}
	}
	return m
}

// aliasEnv maps a raw env key to its display alias (e.g. managed-federal-prod →
// "federal prod"), falling back to the key itself. Rendering is single-threaded,
// so the lazy load needs no synchronization.
func aliasEnv(key string) string {
	if envAliases == nil {
		envAliases = loadEnvAliases()
	}
	if a, ok := envAliases[key]; ok && a != "" {
		return a
	}
	return key
}
