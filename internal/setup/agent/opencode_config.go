package agent

import (
	"encoding/json"
	"strings"
)

// openCodeConfig returns the OpenCode configuration for a run. The CLI passes
// it in OPENCODE_CONFIG_CONTENT, which overrides the user's own OpenCode
// configuration for this process only.
//
// The permission rules let OpenCode read and edit files in the project without
// asking. Every shell command needs the user's approval. Rules deny access to
// files outside the project, and they deny web access and sub-agents.
func openCodeConfig() ([]byte, error) {
	return json.Marshal(map[string]any{
		"$schema": "https://opencode.ai/config.json",
		"permission": map[string]string{
			"read":               "allow",
			"glob":               "allow",
			"grep":               "allow",
			"edit":               "allow",
			"bash":               "ask",
			"external_directory": "deny",
			"webfetch":           "deny",
			"websearch":          "deny",
			"task":               "deny",
		},
	})
}

// childEnv returns the environment for the OpenCode process. It removes every
// LD_ variable, so OpenCode cannot read the ldcli access token or other ldcli
// settings, and it removes the variables that the CLI sets itself.
func childEnv(parent []string, set map[string]string) []string {
	env := make([]string, 0, len(parent)+len(set))
	for _, kv := range parent {
		name, _, _ := strings.Cut(kv, "=")
		if strings.HasPrefix(strings.ToUpper(name), "LD_") {
			continue
		}
		if _, ok := set[name]; ok {
			continue
		}
		env = append(env, kv)
	}
	for name, value := range set {
		env = append(env, name+"="+value)
	}
	return env
}
