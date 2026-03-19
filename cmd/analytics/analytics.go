package analytics

import (
	_ "embed"
	"encoding/json"
	"os"

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

type agentEnvVar struct {
	EnvVar string `json:"env_var"`
	Label  string `json:"label"`
}

type knownAgentsConfig struct {
	Agents    []agentEnvVar `json:"agents"`
	CIEnvVars []string      `json:"ci_env_vars"`
}

var (
	knownAgentEnvVars []agentEnvVar
	knownCIEnvVars    []string
)

func init() {
	var cfg knownAgentsConfig
	if err := json.Unmarshal(knownAgentsJSON, &cfg); err != nil {
		panic("failed to parse embedded known_agents.json: " + err.Error())
	}
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

	for _, a := range knownAgentEnvVars {
		if env.Getenv(a.EnvVar) != "" {
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
