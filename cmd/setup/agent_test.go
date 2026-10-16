package setup

import (
	"bufio"
	"bytes"
	"context"
	"strings"
	"sync"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/launchdarkly/ldcli/internal/setup"
	"github.com/launchdarkly/ldcli/internal/setup/agent"
)

type fakeReply struct {
	id    string
	allow bool
}

type fakeRun struct {
	events chan agent.Event

	mu      sync.Mutex
	replies []fakeReply
	closed  int
}

func newFakeRun(events ...agent.Event) *fakeRun {
	r := &fakeRun{events: make(chan agent.Event, len(events)+1)}
	for _, e := range events {
		r.events <- e
	}
	return r
}

func (r *fakeRun) Events() <-chan agent.Event { return r.events }

func (r *fakeRun) Reply(_ context.Context, req agent.PermissionRequest, allow bool) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.replies = append(r.replies, fakeReply{id: req.ID, allow: allow})
	return nil
}

func (r *fakeRun) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.closed++
	return nil
}

type fakeRunner struct {
	checkErr error
	run      *fakeRun
	tasks    []agent.Task
}

func (f *fakeRunner) Check(context.Context) error { return f.checkErr }

func (f *fakeRunner) Start(_ context.Context, task agent.Task) (agent.Run, error) {
	f.tasks = append(f.tasks, task)
	return f.run, nil
}

func agentWizard(mode agentMode, runner agent.Runner) wizardModel {
	return wizardModel{
		step:         stepCreateFlag,
		width:        80,
		height:       30,
		agentMode:    mode,
		agentRunner:  runner,
		agentRuns:    &runTracker{},
		sdkKey:       "sdk-123",
		detectResult: &setup.DetectResult{SDKID: "node-server", Language: "JavaScript", EntryPoint: "index.js", EntryPointExists: true},
	}
}

func update(t *testing.T, m wizardModel, msg tea.Msg) (wizardModel, tea.Cmd) {
	t.Helper()
	next, cmd := m.Update(msg)
	return next.(wizardModel), cmd
}

// startedAgent drives the wizard to a running agent step.
func startedAgent(t *testing.T, mode agentMode, run *fakeRun) wizardModel {
	t.Helper()
	runner := &fakeRunner{run: run}
	m, cmd := update(t, agentWizard(mode, runner), flagCreatedMsg{key: "my-new-flag"})
	require.Equal(t, stepAgent, m.step)
	require.NotNil(t, cmd)
	started, ok := cmd().(agentStartedMsg)
	require.True(t, ok)
	m, _ = update(t, m, started)
	require.Len(t, runner.tasks, 1)
	return m
}

func TestParseAgentMode(t *testing.T) {
	for in, want := range map[string]agentMode{"auto": agentAuto, "ON": agentOn, " off ": agentOff} {
		got, err := parseAgentMode(in)
		require.NoError(t, err)
		assert.Equal(t, want, got)
	}
	_, err := parseAgentMode("sometimes")
	assert.Error(t, err)
}

func TestWizard_AgentOff_UsesInit(t *testing.T) {
	m, _ := update(t, agentWizard(agentOff, &fakeRunner{}), flagCreatedMsg{key: "f"})

	assert.Equal(t, stepInit, m.step)
}

func TestWizard_AgentAuto_StartsAgentWithTheTask(t *testing.T) {
	runner := &fakeRunner{run: newFakeRun()}
	m, cmd := update(t, agentWizard(agentAuto, runner), flagCreatedMsg{key: "my-new-flag"})
	require.Equal(t, stepAgent, m.step)

	msg := cmd()

	require.IsType(t, agentStartedMsg{}, msg)
	require.Len(t, runner.tasks, 1)
	assert.Equal(t, "node-server", runner.tasks[0].SDKID)
	assert.Equal(t, "index.js", runner.tasks[0].EntryPoint)
	assert.Equal(t, "my-new-flag", runner.tasks[0].FlagKey)
	assert.NotEmpty(t, runner.tasks[0].Dir)
	assert.Contains(t, m.View(), "Starting OpenCode")
}

func TestWizard_AgentUnavailable_AutoFallsBackToInit(t *testing.T) {
	m, cmd := update(t, agentWizard(agentAuto, &fakeRunner{checkErr: agent.ErrNotInstalled}), flagCreatedMsg{key: "f"})

	m, _ = update(t, m, cmd())

	assert.Equal(t, stepInit, m.step)
	assert.Contains(t, m.agentNote, "OpenCode is not installed")
	assert.Nil(t, m.err)
}

func TestWizard_AgentUnavailable_OnStopsWithError(t *testing.T) {
	m, cmd := update(t, agentWizard(agentOn, &fakeRunner{checkErr: agent.ErrNotInstalled}), flagCreatedMsg{key: "f"})

	m, _ = update(t, m, cmd())

	require.Error(t, m.err)
	assert.Contains(t, m.View(), "OpenCode is not installed")
}

func TestWizard_AgentEvents_LogToolsAndFiles(t *testing.T) {
	run := newFakeRun()
	m := startedAgent(t, agentAuto, run)

	m, cmd := update(t, m, agentEventMsg{run: run, ev: agent.Event{Kind: agent.EventTool, Tool: agent.ToolCall{Name: "edit", Status: "completed", Title: "index.js"}}})
	require.NotNil(t, cmd, "the wizard asks for the next event")
	m, _ = update(t, m, agentEventMsg{run: run, ev: agent.Event{Kind: agent.EventFileEdited, File: "index.js"}})
	m, _ = update(t, m, agentEventMsg{run: run, ev: agent.Event{Kind: agent.EventFileEdited, File: "index.js"}})

	assert.Equal(t, []string{"index.js"}, m.agentFiles)
	assert.Contains(t, m.View(), "✓ edit: index.js")
}

func TestWizard_AgentPermission_YAllows(t *testing.T) {
	run := newFakeRun()
	m := startedAgent(t, agentAuto, run)
	req := agent.PermissionRequest{ID: "per1", Permission: "bash", Patterns: []string{"npm test"}}

	m, _ = update(t, m, agentEventMsg{run: run, ev: agent.Event{Kind: agent.EventPermission, Permission: req}})
	assert.Contains(t, m.View(), "OpenCode asks to use bash: npm test")

	m, cmd := update(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	require.NotNil(t, cmd)
	m, _ = update(t, m, cmd())

	assert.Nil(t, m.agentPermission)
	assert.Equal(t, []fakeReply{{id: "per1", allow: true}}, run.replies)
}

func TestWizard_AgentPermission_NRejects(t *testing.T) {
	run := newFakeRun()
	m := startedAgent(t, agentAuto, run)

	m, _ = update(t, m, agentEventMsg{run: run, ev: agent.Event{Kind: agent.EventPermission, Permission: agent.PermissionRequest{ID: "per1"}}})
	_, cmd := update(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")})
	cmd()

	assert.Equal(t, []fakeReply{{id: "per1", allow: false}}, run.replies)
}

func TestWizard_AgentDone_GoesToWaitForAppWithExports(t *testing.T) {
	run := newFakeRun()
	m := startedAgent(t, agentAuto, run)
	m, _ = update(t, m, agentEventMsg{run: run, ev: agent.Event{Kind: agent.EventFileEdited, File: "index.js"}})

	m, _ = update(t, m, agentEventMsg{run: run, ev: agent.Event{Kind: agent.EventDone}})

	assert.Equal(t, stepWaitForApp, m.step)
	assert.Equal(t, 1, run.closed)
	view := m.View()
	assert.Contains(t, view, "index.js")
	assert.Contains(t, view, "export LAUNCHDARKLY_SDK_KEY=sdk-123")

	m, cmd := update(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	assert.Equal(t, stepVerify, m.step)
	assert.NotNil(t, cmd)
}

func TestWizard_AgentDone_InstallFailed_GoesToDone(t *testing.T) {
	run := newFakeRun()
	m := startedAgent(t, agentAuto, run)
	m.installResult = &setup.InstallResult{Failed: true, Command: "npm install x"}

	m, _ = update(t, m, agentEventMsg{run: run, ev: agent.Event{Kind: agent.EventDone}})

	assert.Equal(t, stepDone, m.step)
}

func TestWizard_AgentError_NoEdits_AutoFallsBack(t *testing.T) {
	run := newFakeRun()
	m := startedAgent(t, agentAuto, run)

	m, cmd := update(t, m, agentEventMsg{run: run, ev: agent.Event{Kind: agent.EventError, Text: "no API key"}})

	assert.Equal(t, stepInit, m.step)
	assert.NotNil(t, cmd)
	assert.Contains(t, m.agentNote, "no API key")
	assert.Equal(t, 1, run.closed)
}

func TestWizard_AgentError_AfterEdits_DoesNotFallBack(t *testing.T) {
	run := newFakeRun()
	m := startedAgent(t, agentAuto, run)
	m, _ = update(t, m, agentEventMsg{run: run, ev: agent.Event{Kind: agent.EventFileEdited, File: "index.js"}})

	m, _ = update(t, m, agentEventMsg{run: run, ev: agent.Event{Kind: agent.EventError, Text: "boom"}})

	require.Error(t, m.err)
	assert.Equal(t, stepAgent, m.step)
}

func TestWizard_AgentEvent_FromOldRunIsIgnored(t *testing.T) {
	run := newFakeRun()
	m := startedAgent(t, agentAuto, run)

	m, cmd := update(t, m, agentEventMsg{run: newFakeRun(), ev: agent.Event{Kind: agent.EventDone}})

	assert.Equal(t, stepAgent, m.step)
	assert.Nil(t, cmd)
}

func TestWizard_Quit_ClosesAgent(t *testing.T) {
	run := newFakeRun()
	m := startedAgent(t, agentAuto, run)

	_, cmd := update(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})

	assert.True(t, quitsOn(cmd))
	assert.Equal(t, 1, run.closed)
}

func TestRunTracker_ClosesRunsAndLateStarts(t *testing.T) {
	tracker := &runTracker{}
	early := newFakeRun()
	tracker.add(early)

	tracker.closeAll()
	late := newFakeRun()
	tracker.add(late)

	assert.Equal(t, 1, early.closed)
	assert.Equal(t, 1, late.closed, "a run that starts after setup ends is closed at once")
}

func TestWizard_Plan_AgentMode_SaysAgent(t *testing.T) {
	m := agentWizard(agentAuto, &fakeRunner{})
	m.step = stepPlan
	m.selectedProject, m.selectedEnv = "proj", "prod"

	view := m.View()

	assert.Contains(t, view, "Use the setup agent to add initialization code")
	assert.Contains(t, view, "uses the template if OpenCode cannot run")
}

func TestStreamAgent_AnswersPermissionAndReturnsFiles(t *testing.T) {
	run := newFakeRun(
		agent.Event{Kind: agent.EventText, Text: "Working"},
		agent.Event{Kind: agent.EventPermission, Permission: agent.PermissionRequest{ID: "per1", Permission: "bash", Patterns: []string{"npm test"}}},
		agent.Event{Kind: agent.EventFileEdited, File: "b.js"},
		agent.Event{Kind: agent.EventFileEdited, File: "a.js"},
		agent.Event{Kind: agent.EventDone},
	)
	var out bytes.Buffer

	files, err := streamAgent(context.Background(), run, &out, bufio.NewReader(strings.NewReader("y\n")))

	require.NoError(t, err)
	assert.Equal(t, []string{"a.js", "b.js"}, files)
	assert.Equal(t, []fakeReply{{id: "per1", allow: true}}, run.replies)
	assert.Contains(t, out.String(), "OpenCode asks to use bash: npm test")
}

func TestStreamAgent_EmptyAnswerRejects(t *testing.T) {
	run := newFakeRun(
		agent.Event{Kind: agent.EventPermission, Permission: agent.PermissionRequest{ID: "per1"}},
		agent.Event{Kind: agent.EventDone},
	)

	_, err := streamAgent(context.Background(), run, &bytes.Buffer{}, bufio.NewReader(strings.NewReader("\n")))

	require.NoError(t, err)
	assert.Equal(t, []fakeReply{{id: "per1", allow: false}}, run.replies)
}

func TestStreamAgent_Error(t *testing.T) {
	run := newFakeRun(agent.Event{Kind: agent.EventError, Text: "no API key"})

	_, err := streamAgent(context.Background(), run, &bytes.Buffer{}, bufio.NewReader(strings.NewReader("")))

	require.Error(t, err)
	assert.Contains(t, err.Error(), "no API key")
}

func TestCredentialExports_SkipsEmptyValues(t *testing.T) {
	assert.Equal(t, []string{"export LAUNCHDARKLY_CLIENT_SIDE_ID=abc"}, credentialExports("", "abc", ""))
}
