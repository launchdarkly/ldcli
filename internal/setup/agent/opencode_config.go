package agent

import (
	"encoding/json"
	"strings"
)

// openCodeConfig returns the OpenCode configuration for a run. The CLI passes
// it in OPENCODE_CONFIG_CONTENT, which overrides the user's own OpenCode
// configuration for this process only.
//
// The agent works without approvals: it reads, edits, and runs shell commands
// in the project without asking. It asks the user only with the question
// tool, when it needs information such as where a key is stored. Rules deny
// files outside the project, web access, sub-agents, and a few commands that
// the agent must never run, such as git push.
func openCodeConfig() ([]byte, error) {
	bash, err := orderedRules(bashRules)
	if err != nil {
		return nil, err
	}
	return json.Marshal(map[string]any{
		"$schema": "https://opencode.ai/config.json",
		"permission": map[string]any{
			"read":               "allow",
			"glob":               "allow",
			"grep":               "allow",
			"edit":               "allow",
			"bash":               bash,
			"question":           "allow",
			"external_directory": "deny",
			"webfetch":           "deny",
			"websearch":          "deny",
			"task":               "deny",
		},
	})
}

// bashRules are the shell command rules, in order. OpenCode applies the last
// rule that matches, so the catch-all allow comes first and the denials
// after it.
var bashRules = [][2]string{
	{"*", "allow"},
	// ldcli and the skills installer reach LaunchDarkly or change the repo
	// outside the agent's task. The CLI does the LaunchDarkly work itself.
	{"ldcli*", "deny"},
	{"npx skills*", "deny"},
	{"git commit*", "deny"},
	{"git push*", "deny"},
	{"git reset*", "deny"},
	{"sudo *", "deny"},
	{"rm -rf /*", "deny"},
	{"rm -fr /*", "deny"},
}

// orderedRules encodes rules as a JSON object with the keys in the given
// order. A Go map would sort the keys.
func orderedRules(rules [][2]string) (json.RawMessage, error) {
	var b strings.Builder
	b.WriteByte('{')
	for i, r := range rules {
		if i > 0 {
			b.WriteByte(',')
		}
		key, err := json.Marshal(r[0])
		if err != nil {
			return nil, err
		}
		value, err := json.Marshal(r[1])
		if err != nil {
			return nil, err
		}
		b.Write(key)
		b.WriteByte(':')
		b.Write(value)
	}
	b.WriteByte('}')
	return json.RawMessage(b.String()), nil
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
