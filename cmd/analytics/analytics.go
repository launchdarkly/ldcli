package analytics

import (
	_ "embed"
	"encoding/json"
	"os"
	"strings"

	"github.com/launchdarkly/ldcli/cmd/cliflags"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
	"golang.org/x/term"
)

type envChecker interface {
	Getenv(key string) string
	IsTerminal(fd int) bool
	StdinFd() int
	StdoutFd() int
}

type osEnvChecker struct{}

func (osEnvChecker) Getenv(key string) string { return os.Getenv(key) }
func (osEnvChecker) IsTerminal(fd int) bool   { return term.IsTerminal(fd) }
func (osEnvChecker) StdinFd() int             { return int(os.Stdin.Fd()) }
func (osEnvChecker) StdoutFd() int            { return int(os.Stdout.Fd()) }

//go:embed known_agents.json
var knownAgentsJSON []byte

// agentEnvVar maps an environment variable to an agent label. If Equals or
// Contains is set, the value must also match. Otherwise any non-empty value
// matches.
// agentEnvVar maps an environment variable to an agent label. If Equals or
// Contains is set, the value must also match. Otherwise any non-empty value
// matches.
type agentEnvVar struct {
	EnvVar   string `json:"env_var"`
	Equals   string `json:"equals,omitempty"`
	Contains string `json:"contains,omitempty"`
	Label    string `json:"label"`
}

func (a agentEnvVar) matches(value string) bool {
	switch {
	case value == "":
		return false
	case a.Equals != "":
		return value == a.Equals
	case a.Contains != "":
		return strings.Contains(value, a.Contains)
	default:
		return true
	}
}

// aiAgentPrefix maps the start of an AI_AGENT value to an agent label.
type aiAgentPrefix struct {
	Prefix string `json:"prefix"`
	Label  string `json:"label"`
}

type knownAgentsConfig struct {
	AIAgentPrefixes []aiAgentPrefix `json:"ai_agent_prefixes"`
	Agents          []agentEnvVar   `json:"agents"`
	CIEnvVars       []string        `json:"ci_env_vars"`
}

var (
	knownAIAgentPrefixes []aiAgentPrefix
	knownAgentEnvVars    []agentEnvVar
	knownCIEnvVars       []string
)

func init() {
	var cfg knownAgentsConfig
	if err := json.Unmarshal(knownAgentsJSON, &cfg); err != nil {
		panic("failed to parse embedded known_agents.json: " + err.Error())
	}
	knownAIAgentPrefixes = cfg.AIAgentPrefixes
	knownAgentEnvVars = cfg.Agents
	knownCIEnvVars = cfg.CIEnvVars
}

// DetectAgentContext returns a label identifying the agent environment, or ""
// if the CLI appears to be running in an interactive human terminal.
func DetectAgentContext() string {
	return detectAgentContext(osEnvChecker{})
}

func detectAgentContext(env envChecker) string {
	if v := env.Getenv("LD_CLI_AGENT"); v != "" {
		return "explicit:" + v
	}

	if label := aiAgentLabel(env.Getenv("AI_AGENT")); label != "" {
		return label
	}

	for _, a := range knownAgentEnvVars {
		if a.matches(env.Getenv(a.EnvVar)) {
			return a.Label
		}
	}

	if env.IsTerminal(env.StdinFd()) || env.IsTerminal(env.StdoutFd()) {
		return ""
	}

	for _, ciVar := range knownCIEnvVars {
		if env.Getenv(ciVar) != "" {
			return "ci"
		}
	}

	return "unknown-non-interactive"
}

// maxAIAgentNameLen limits the length of an unknown AI_AGENT name in a label.
const maxAIAgentNameLen = 32

// aiAgentLabel converts an AI_AGENT value to a label. AI_AGENT is a shared
// convention: the value starts with the agent name and can have a version
// after it, for example "claude-code_2-1-295_agent" or "devin@1". A known
// name gets its label. An unknown name gets "ai-agent:<name>", without the
// version. The function returns "" if the value has no usable name.
func aiAgentLabel(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return ""
	}

	for _, p := range knownAIAgentPrefixes {
		if hasNamePrefix(value, p.Prefix) {
			return p.Label
		}
	}

	name, _, _ := strings.Cut(value, "@")
	name, _, _ = strings.Cut(name, "_")
	name = strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			return r
		}
		return -1
	}, name)
	if len(name) > maxAIAgentNameLen {
		name = name[:maxAIAgentNameLen]
	}
	if name == "" {
		return ""
	}

	return "ai-agent:" + name
}

// hasNamePrefix reports whether value starts with prefix and the prefix ends at
// a name boundary. For example, "amp" matches "amp" and "amp_1" but not "ampere".
func hasNamePrefix(value, prefix string) bool {
	if !strings.HasPrefix(value, prefix) {
		return false
	}
	if len(value) == len(prefix) {
		return true
	}
	switch value[len(prefix)] {
	case '_', '@', '-':
		return true
	default:
		return false
	}
}

func CmdRunEventProperties(
	cmd *cobra.Command,
	name string,
	overrides map[string]interface{},
) map[string]interface{} {
	baseURI := viper.GetString(cliflags.BaseURIFlag)
	var flags []string
	cmd.Flags().Visit(func(f *pflag.Flag) {
		flags = append(flags, f.Name)
	})

	properties := map[string]interface{}{
		"name":   name,
		"action": cmd.CalledAs(),
		"flags":  flags,
	}
	if baseURI != cliflags.BaseURIDefault {
		properties["baseURI"] = baseURI
	}

	for k, v := range overrides {
		properties[k] = v
	}

	return properties
}
