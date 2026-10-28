package analytics

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
)

type mockEnvChecker struct {
	envVars        map[string]string
	stdinTerminal  bool
	stdoutTerminal bool
}

func newMockEnv(envVars map[string]string, bothTerminal bool) mockEnvChecker {
	return mockEnvChecker{envVars: envVars, stdinTerminal: bothTerminal, stdoutTerminal: bothTerminal}
}

func (m mockEnvChecker) Getenv(key string) string { return m.envVars[key] }
func (m mockEnvChecker) IsTerminal(fd int) bool {
	if fd == 0 {
		return m.stdinTerminal
	}
	return m.stdoutTerminal
}
func (m mockEnvChecker) StdinFd() int  { return 0 }
func (m mockEnvChecker) StdoutFd() int { return 1 }

func TestDetectAgentContext(t *testing.T) {
	tests := []struct {
		name     string
		env      mockEnvChecker
		expected string
	}{
		{
			name:     "explicit LD_CLI_AGENT",
			env:      newMockEnv(map[string]string{"LD_CLI_AGENT": "my-skill"}, false),
			expected: "explicit:my-skill",
		},
		{
			name:     "explicit LD_CLI_AGENT takes precedence over AI_AGENT and known env vars",
			env:      newMockEnv(map[string]string{"LD_CLI_AGENT": "custom", "AI_AGENT": "devin_1_agent", "CLAUDECODE": "1"}, false),
			expected: "explicit:custom",
		},

		// Environment variables that each agent sets in the shell that runs ldcli.
		{
			name: "Claude Code",
			env: newMockEnv(map[string]string{
				"AI_AGENT":               "claude-code_2-1-295_agent",
				"CLAUDECODE":             "1",
				"CLAUDE_CODE_ENTRYPOINT": "cli",
				"CLAUDE_CODE_SESSION_ID": "1366a0bd-a5d9-4aef-a2b1-e96a3a313a34",
			}, false),
			expected: "claude-code",
		},
		{
			name:     "Claude Code before AI_AGENT support",
			env:      newMockEnv(map[string]string{"CLAUDECODE": "1", "CLAUDE_CODE_ENTRYPOINT": "cli"}, false),
			expected: "claude-code",
		},
		{
			name:     "Cursor agent",
			env:      newMockEnv(map[string]string{"CURSOR_AGENT": "1"}, false),
			expected: "cursor",
		},
		{
			name:     "Cursor agent terminal role",
			env:      newMockEnv(map[string]string{"CURSOR_EXTENSION_HOST_ROLE": "agent-exec"}, false),
			expected: "cursor",
		},
		{
			name:     "Cursor builds that also set CI=1",
			env:      newMockEnv(map[string]string{"CURSOR_AGENT": "1", "CI": "1"}, false),
			expected: "cursor",
		},
		{
			name: "Codex CLI",
			env: newMockEnv(map[string]string{
				"CODEX_THREAD_ID":                "t-1",
				"CODEX_SESSION_ID":               "s-1",
				"CODEX_SANDBOX_NETWORK_DISABLED": "1",
			}, false),
			expected: "codex",
		},
		{
			name:     "Codex CLI unified exec",
			env:      newMockEnv(map[string]string{"CODEX_CI": "1"}, false),
			expected: "codex",
		},
		{
			name:     "Codex CLI macOS sandbox",
			env:      newMockEnv(map[string]string{"CODEX_SANDBOX": "seatbelt"}, false),
			expected: "codex",
		},
		{
			name:     "Devin",
			env:      newMockEnv(map[string]string{"AI_AGENT": "devin_3000-11-1_agent"}, false),
			expected: "devin",
		},
		{
			name: "GitHub Copilot in VS Code",
			env: newMockEnv(map[string]string{
				"AI_AGENT":      "github_copilot_vscode_agent",
				"COPILOT_AGENT": "1",
				"TERM_PROGRAM":  "vscode",
			}, true),
			expected: "copilot",
		},
		{
			name:     "GitHub Copilot CLI",
			env:      newMockEnv(map[string]string{"COPILOT_CLI": "1", "COPILOT_AGENT_SESSION_ID": "s-1"}, false),
			expected: "copilot",
		},
		{
			name:     "Gemini CLI",
			env:      newMockEnv(map[string]string{"GEMINI_CLI": "1"}, false),
			expected: "gemini-cli",
		},
		{
			name:     "OpenCode",
			env:      newMockEnv(map[string]string{"OPENCODE": "1", "AGENT": "1", "OPENCODE_PID": "42"}, false),
			expected: "opencode",
		},
		{
			name:     "Kilo Code also sets OPENCODE",
			env:      newMockEnv(map[string]string{"KILO": "1", "KILOCODE_FEATURE": "cli", "OPENCODE": "1", "AGENT": "1"}, false),
			expected: "kilo-code",
		},
		{
			name:     "Crush",
			env:      newMockEnv(map[string]string{"CRUSH": "1", "AGENT": "crush", "AI_AGENT": "crush"}, false),
			expected: "crush",
		},
		{
			name:     "Augment",
			env:      newMockEnv(map[string]string{"AUGMENT_AGENT": "1"}, false),
			expected: "augment",
		},
		{
			name:     "Cline in a VS Code terminal",
			env:      newMockEnv(map[string]string{"CLINE_ACTIVE": "true", "TERM_PROGRAM": "vscode"}, true),
			expected: "cline",
		},
		{
			name:     "Roo Code in a VS Code terminal",
			env:      newMockEnv(map[string]string{"ROO_ACTIVE": "true"}, true),
			expected: "roo-code",
		},
		{
			name:     "Roo Code CLI",
			env:      newMockEnv(map[string]string{"ROO_CLI_RUNTIME": "1"}, false),
			expected: "roo-code",
		},
		{
			name:     "Amp",
			env:      newMockEnv(map[string]string{"AMP_CURRENT_THREAD_ID": "T-1"}, false),
			expected: "amp",
		},
		{
			name:     "Windsurf Cascade",
			env:      newMockEnv(map[string]string{"WINDSURF_CASCADE_TERMINAL": "1"}, false),
			expected: "windsurf",
		},
		{
			name:     "Amazon Q Developer CLI",
			env:      newMockEnv(map[string]string{"AWS_EXECUTION_ENV": "AmazonQ-For-CLI Version/1.12.0"}, false),
			expected: "amazon-q",
		},
		{
			name:     "Aider",
			env:      newMockEnv(map[string]string{"OR_APP_NAME": "Aider", "OR_SITE_URL": "https://aider.chat"}, false),
			expected: "aider",
		},

		// AI_AGENT parsing.
		{
			name:     "AI_AGENT takes precedence over agent env vars",
			env:      newMockEnv(map[string]string{"AI_AGENT": "claude-code_2-1-295_agent", "CODEX_THREAD_ID": "t-1"}, false),
			expected: "claude-code",
		},
		{
			name:     "AI_AGENT with an @ version",
			env:      newMockEnv(map[string]string{"AI_AGENT": "devin@1"}, false),
			expected: "devin",
		},
		{
			name:     "AI_AGENT is not case sensitive",
			env:      newMockEnv(map[string]string{"AI_AGENT": " Claude-Code_2-1-295_agent "}, false),
			expected: "claude-code",
		},
		{
			name:     "AI_AGENT with an unknown name drops the version",
			env:      newMockEnv(map[string]string{"AI_AGENT": "hermes-agent_0-4_agent"}, false),
			expected: "ai-agent:hermes-agent",
		},
		{
			name:     "AI_AGENT prefix must end at a name boundary",
			env:      newMockEnv(map[string]string{"AI_AGENT": "ampere"}, false),
			expected: "ai-agent:ampere",
		},
		{
			name:     "AI_AGENT removes characters that are not allowed",
			env:      newMockEnv(map[string]string{"AI_AGENT": "my agent!/../x"}, false),
			expected: "ai-agent:myagentx",
		},
		{
			name:     "AI_AGENT with a long unknown name is cut",
			env:      newMockEnv(map[string]string{"AI_AGENT": "abcdefghijklmnopqrstuvwxyz0123456789"}, false),
			expected: "ai-agent:abcdefghijklmnopqrstuvwxyz012345",
		},
		{
			name:     "AI_AGENT with no usable name is ignored",
			env:      newMockEnv(map[string]string{"AI_AGENT": "_@!!"}, false),
			expected: "unknown-non-interactive",
		},
		{
			name:     "AI_AGENT that is only spaces is ignored",
			env:      newMockEnv(map[string]string{"AI_AGENT": "   "}, true),
			expected: "",
		},

		// Values that must not match.
		{
			name:     "AWS_EXECUTION_ENV without AmazonQ is not an agent",
			env:      newMockEnv(map[string]string{"AWS_EXECUTION_ENV": "AWS_Lambda_go1.x"}, false),
			expected: "unknown-non-interactive",
		},
		{
			name:     "OR_APP_NAME for another app is not an agent",
			env:      newMockEnv(map[string]string{"OR_APP_NAME": "SomeOtherApp"}, false),
			expected: "unknown-non-interactive",
		},
		{
			name:     "CURSOR_EXTENSION_HOST_ROLE for a person is not an agent",
			env:      newMockEnv(map[string]string{"CURSOR_EXTENSION_HOST_ROLE": "user"}, true),
			expected: "",
		},
		{
			name:     "person in the Cursor terminal is not an agent",
			env:      newMockEnv(map[string]string{"CURSOR_TRACE_ID": "xyz", "TERM_PROGRAM": "vscode"}, true),
			expected: "",
		},
		{
			name:     "person with AIDER_MODEL in the shell profile is not an agent",
			env:      newMockEnv(map[string]string{"AIDER_MODEL": "gpt-4"}, true),
			expected: "",
		},
		{
			name:     "Replit workspace is not an agent",
			env:      newMockEnv(map[string]string{"REPL_ID": "r-1"}, true),
			expected: "",
		},
		{
			name: "environment variables that no agent sets are not detected",
			env: newMockEnv(map[string]string{
				"CLAUDE_CODE":         "1",
				"CLAUDE_CODE_SESSION": "s",
				"CURSOR_SESSION_ID":   "c",
				"CODEX_SESSION":       "s",
				"CODEX_SANDBOX_ID":    "sb",
				"DEVIN_SESSION":       "d",
				"GITHUB_COPILOT":      "1",
				"WINDSURF_SESSION":    "w",
				"CLINE_TASK_ID":       "t",
			}, false),
			expected: "unknown-non-interactive",
		},

		// Terminal and CI fallbacks.
		{
			name:     "no TTY and no env vars returns unknown-non-interactive",
			env:      newMockEnv(map[string]string{}, false),
			expected: "unknown-non-interactive",
		},
		{
			name:     "CI env var returns ci",
			env:      newMockEnv(map[string]string{"CI": "true"}, false),
			expected: "ci",
		},
		{
			name:     "GITHUB_ACTIONS env var returns ci",
			env:      newMockEnv(map[string]string{"GITHUB_ACTIONS": "true"}, false),
			expected: "ci",
		},
		{
			name:     "GITLAB_CI env var returns ci",
			env:      newMockEnv(map[string]string{"GITLAB_CI": "true"}, false),
			expected: "ci",
		},
		{
			name:     "agent env var takes precedence over CI env var",
			env:      newMockEnv(map[string]string{"COPILOT_AGENT": "1", "GITHUB_ACTIONS": "true", "CI": "true"}, false),
			expected: "copilot",
		},
		{
			name:     "interactive terminal with no agent env vars returns empty",
			env:      newMockEnv(map[string]string{}, true),
			expected: "",
		},
		{
			name:     "CI env var in interactive terminal returns empty",
			env:      newMockEnv(map[string]string{"CI": "true"}, true),
			expected: "",
		},
		{
			name:     "stdin is TTY but stdout is not still counts as interactive",
			env:      mockEnvChecker{envVars: map[string]string{}, stdinTerminal: true, stdoutTerminal: false},
			expected: "",
		},
		{
			name:     "stdout is TTY but stdin is not still counts as interactive",
			env:      mockEnvChecker{envVars: map[string]string{}, stdinTerminal: false, stdoutTerminal: true},
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := detectAgentContext(tt.env)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestKnownAgentsConfig(t *testing.T) {
	assert.NotEmpty(t, knownAIAgentPrefixes)
	assert.NotEmpty(t, knownAgentEnvVars)
	assert.NotEmpty(t, knownCIEnvVars)

	for _, p := range knownAIAgentPrefixes {
		assert.NotEmpty(t, p.Prefix)
		assert.NotEmpty(t, p.Label, "prefix %q", p.Prefix)
		assert.Equal(t, strings.ToLower(p.Prefix), p.Prefix, "prefix %q must be lowercase", p.Prefix)
	}
	for _, a := range knownAgentEnvVars {
		assert.NotEmpty(t, a.EnvVar)
		assert.NotEmpty(t, a.Label, "env var %q", a.EnvVar)
		assert.False(t, a.Equals != "" && a.Contains != "", "env var %q sets both equals and contains", a.EnvVar)
	}
}

func TestCmdRunEventProperties(t *testing.T) {
	t.Run("does not set agent_context itself", func(t *testing.T) {
		cmd := &cobra.Command{Use: "test"}
		cmd.SetArgs([]string{})
		_ = cmd.Execute()

		props := CmdRunEventProperties(cmd, "test-resource", nil)

		_, hasAgentCtx := props["agent_context"]
		assert.False(t, hasAgentCtx, "agent_context is injected by sendEvent, not CmdRunEventProperties")
	})

	t.Run("overrides can still set agent_context", func(t *testing.T) {
		cmd := &cobra.Command{Use: "test"}
		cmd.SetArgs([]string{})
		_ = cmd.Execute()

		overrides := map[string]interface{}{"agent_context": "explicit:test-agent"}
		props := CmdRunEventProperties(cmd, "test-resource", overrides)

		assert.Equal(t, "explicit:test-agent", props["agent_context"])
	})

	t.Run("returns expected base properties", func(t *testing.T) {
		cmd := &cobra.Command{Use: "test"}
		cmd.SetArgs([]string{})
		_ = cmd.Execute()

		props := CmdRunEventProperties(cmd, "flags", nil)

		assert.Equal(t, "flags", props["name"])
		assert.Contains(t, props, "action")
		assert.Contains(t, props, "flags")
	})
}
