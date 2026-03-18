package analytics

import (
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

func (osEnvChecker) Getenv(key string) string  { return os.Getenv(key) }
func (osEnvChecker) IsTerminal(fd int) bool    { return term.IsTerminal(fd) }
func (osEnvChecker) StdinFd() int              { return int(os.Stdin.Fd()) }
func (osEnvChecker) StdoutFd() int             { return int(os.Stdout.Fd()) }

type agentEnvVar struct {
	envVar string
	label  string
}

// Ordered list so detection priority is deterministic.
var knownAgentEnvVars = []agentEnvVar{
	{"CURSOR_SESSION_ID", "cursor"},
	{"CURSOR_TRACE_ID", "cursor"},
	{"CLAUDE_CODE", "claude-code"},
	{"CLAUDE_CODE_SESSION", "claude-code"},
	{"CODEX_SESSION", "codex"},
	{"CODEX_SANDBOX_ID", "codex"},
	{"DEVIN_SESSION", "devin"},
	{"GITHUB_COPILOT", "copilot"},
	{"WINDSURF_SESSION", "windsurf"},
	{"CLINE_TASK_ID", "cline"},
	{"AIDER_MODEL", "aider"},
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
		if env.Getenv(a.envVar) != "" {
			return a.label
		}
	}

	if !env.IsTerminal(env.StdinFd()) && !env.IsTerminal(env.StdoutFd()) {
		return "no-tty"
	}

	return ""
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
