package analytics

import (
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
)

type mockEnvChecker struct {
	envVars       map[string]string
	stdinTerminal bool
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
			name:     "explicit LD_CLI_AGENT takes precedence over known env vars",
			env:      newMockEnv(map[string]string{"LD_CLI_AGENT": "custom", "CURSOR_SESSION_ID": "abc"}, false),
			expected: "explicit:custom",
		},
		{
			name:     "CURSOR_SESSION_ID detected",
			env:      newMockEnv(map[string]string{"CURSOR_SESSION_ID": "abc"}, false),
			expected: "cursor",
		},
		{
			name:     "CURSOR_TRACE_ID detected",
			env:      newMockEnv(map[string]string{"CURSOR_TRACE_ID": "xyz"}, false),
			expected: "cursor",
		},
		{
			name:     "CLAUDE_CODE detected",
			env:      newMockEnv(map[string]string{"CLAUDE_CODE": "1"}, false),
			expected: "claude-code",
		},
		{
			name:     "CLAUDE_CODE_SESSION detected",
			env:      newMockEnv(map[string]string{"CLAUDE_CODE_SESSION": "sess"}, false),
			expected: "claude-code",
		},
		{
			name:     "CODEX_SESSION detected",
			env:      newMockEnv(map[string]string{"CODEX_SESSION": "s"}, false),
			expected: "codex",
		},
		{
			name:     "CODEX_SANDBOX_ID detected",
			env:      newMockEnv(map[string]string{"CODEX_SANDBOX_ID": "sb"}, false),
			expected: "codex",
		},
		{
			name:     "DEVIN_SESSION detected",
			env:      newMockEnv(map[string]string{"DEVIN_SESSION": "d"}, false),
			expected: "devin",
		},
		{
			name:     "GITHUB_COPILOT detected",
			env:      newMockEnv(map[string]string{"GITHUB_COPILOT": "1"}, false),
			expected: "copilot",
		},
		{
			name:     "WINDSURF_SESSION detected",
			env:      newMockEnv(map[string]string{"WINDSURF_SESSION": "w"}, false),
			expected: "windsurf",
		},
		{
			name:     "CLINE_TASK_ID detected",
			env:      newMockEnv(map[string]string{"CLINE_TASK_ID": "t"}, false),
			expected: "cline",
		},
		{
			name:     "AIDER_MODEL detected",
			env:      newMockEnv(map[string]string{"AIDER_MODEL": "gpt-4"}, false),
			expected: "aider",
		},
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
			env:      newMockEnv(map[string]string{"CURSOR_SESSION_ID": "abc", "CI": "true"}, false),
			expected: "cursor",
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
			name:     "first matching env var wins by priority order",
			env:      newMockEnv(map[string]string{"CURSOR_SESSION_ID": "c", "CLAUDE_CODE": "1"}, false),
			expected: "cursor",
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
