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

type fakeAnswer struct {
	id      string
	answers [][]string
}

type fakeRun struct {
	events chan agent.Event

	mu      sync.Mutex
	replies []fakeReply
	answers []fakeAnswer
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

func (r *fakeRun) Answer(_ context.Context, req agent.QuestionRequest, answers [][]string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.answers = append(r.answers, fakeAnswer{id: req.ID, answers: answers})
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

func agentWizard(runner agent.Runner) wizardModel {
	return wizardModel{
		step:         stepCreateFlag,
		width:        80,
		height:       30,
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
func startedAgent(t *testing.T, run *fakeRun) wizardModel {
	t.Helper()
	runner := &fakeRunner{run: run}
	m, cmd := update(t, agentWizard(runner), flagCreatedMsg{key: "my-new-flag"})
	require.Equal(t, stepAgent, m.step)
	require.NotNil(t, cmd)
	started, ok := cmd().(agentStartedMsg)
	require.True(t, ok)
	m, _ = update(t, m, started)
	require.Len(t, runner.tasks, 1)
	return m
}

func TestWizard_StartsAgentWithTheTask(t *testing.T) {
	runner := &fakeRunner{run: newFakeRun()}
	m, cmd := update(t, agentWizard(runner), flagCreatedMsg{key: "my-new-flag"})
	require.Equal(t, stepAgent, m.step)

	msg := cmd()

	require.IsType(t, agentStartedMsg{}, msg)
	require.Len(t, runner.tasks, 1)
	assert.Equal(t, "node-server", runner.tasks[0].SDKID)
	assert.Equal(t, "index.js", runner.tasks[0].EntryPoint)
	assert.Equal(t, "my-new-flag", runner.tasks[0].FlagKey)
	assert.NotEmpty(t, runner.tasks[0].Dir)
	assert.Contains(t, m.View(), "Starting the agent")
}

func TestWizard_AgentUnavailable_StopsWithError(t *testing.T) {
	m, cmd := update(t, agentWizard(&fakeRunner{checkErr: agent.ErrNotInstalled}), flagCreatedMsg{key: "f"})

	m, _ = update(t, m, cmd())

	require.Error(t, m.err)
	assert.Contains(t, m.View(), "OpenCode is not installed")
}

func TestWizard_AgentEvents_LogToolsAndFiles(t *testing.T) {
	run := newFakeRun()
	m := startedAgent(t, run)

	m, cmd := update(t, m, agentEventMsg{run: run, ev: agent.Event{Kind: agent.EventTool, Tool: agent.ToolCall{Name: "edit", Status: "completed", Title: "index.js"}}})
	require.NotNil(t, cmd, "the wizard asks for the next event")
	m, _ = update(t, m, agentEventMsg{run: run, ev: agent.Event{Kind: agent.EventFileEdited, File: "index.js"}})
	m, _ = update(t, m, agentEventMsg{run: run, ev: agent.Event{Kind: agent.EventFileEdited, File: "index.js"}})

	assert.Equal(t, []string{"index.js"}, m.agentFiles)
	assert.Contains(t, m.View(), "✓ edit: index.js")
}

func TestWizard_AgentPermission_YAllows(t *testing.T) {
	run := newFakeRun()
	m := startedAgent(t, run)
	req := agent.PermissionRequest{ID: "per1", Permission: "bash", Patterns: []string{"npm test"}}

	m, _ = update(t, m, agentEventMsg{run: run, ev: agent.Event{Kind: agent.EventPermission, Permission: req}})
	assert.Contains(t, m.View(), "The agent asks to use bash: npm test")

	m, cmd := update(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	require.NotNil(t, cmd)
	m, _ = update(t, m, cmd())

	assert.Empty(t, m.agentPermissions)
	assert.Equal(t, []fakeReply{{id: "per1", allow: true}}, run.replies)
}

func TestWizard_AgentPermission_NRejects(t *testing.T) {
	run := newFakeRun()
	m := startedAgent(t, run)

	m, _ = update(t, m, agentEventMsg{run: run, ev: agent.Event{Kind: agent.EventPermission, Permission: agent.PermissionRequest{ID: "per1"}}})
	_, cmd := update(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")})
	cmd()

	assert.Equal(t, []fakeReply{{id: "per1", allow: false}}, run.replies)
}

func TestWizard_AgentDone_GoesToWaitForAppWithExports(t *testing.T) {
	run := newFakeRun()
	m := startedAgent(t, run)
	m, _ = update(t, m, agentEventMsg{run: run, ev: agent.Event{Kind: agent.EventFileEdited, File: "index.js"}})

	m, _ = update(t, m, agentEventMsg{run: run, ev: agent.Event{Kind: agent.EventDone}})

	assert.Equal(t, stepWaitForApp, m.step)
	assert.Equal(t, 1, run.closed)
	view := m.View()
	assert.Contains(t, view, "index.js")
	assert.Contains(t, view, "export LAUNCHDARKLY_SDK_KEY=sdk-123")
	assert.NotContains(t, view, "LAUNCHDARKLY_MOBILE_KEY", "only the SDK's own credential is shown")

	m, cmd := update(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	assert.Equal(t, stepVerify, m.step)
	assert.NotNil(t, cmd)
}

func TestWizard_AgentDone_InstallFailed_GoesToDone(t *testing.T) {
	run := newFakeRun()
	m := startedAgent(t, run)
	m.installResult = &setup.InstallResult{Failed: true, Command: "npm install x"}

	m, _ = update(t, m, agentEventMsg{run: run, ev: agent.Event{Kind: agent.EventDone}})

	assert.Equal(t, stepDone, m.step)
}

func TestWizard_AgentError_StopsWithError(t *testing.T) {
	run := newFakeRun()
	m := startedAgent(t, run)

	m, cmd := update(t, m, agentEventMsg{run: run, ev: agent.Event{Kind: agent.EventError, Text: "no API key"}})

	require.Error(t, m.err)
	assert.Contains(t, m.err.Error(), "no API key")
	assert.Equal(t, stepAgent, m.step)
	assert.Nil(t, cmd)
	assert.Equal(t, 1, run.closed)
}

func TestWizard_AgentError_AfterEdits_DoesNotFallBack(t *testing.T) {
	run := newFakeRun()
	m := startedAgent(t, run)
	m, _ = update(t, m, agentEventMsg{run: run, ev: agent.Event{Kind: agent.EventFileEdited, File: "index.js"}})

	m, _ = update(t, m, agentEventMsg{run: run, ev: agent.Event{Kind: agent.EventError, Text: "boom"}})

	require.Error(t, m.err)
	assert.Equal(t, stepAgent, m.step)
}

func TestWizard_AgentEvent_FromOldRunIsIgnored(t *testing.T) {
	run := newFakeRun()
	m := startedAgent(t, run)

	m, cmd := update(t, m, agentEventMsg{run: newFakeRun(), ev: agent.Event{Kind: agent.EventDone}})

	assert.Equal(t, stepAgent, m.step)
	assert.Nil(t, cmd)
}

func TestWizard_Quit_ClosesAgent(t *testing.T) {
	run := newFakeRun()
	m := startedAgent(t, run)

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

func TestWizard_Plan_SaysAgent(t *testing.T) {
	m := agentWizard(&fakeRunner{})
	m.step = stepPlan
	m.selectedProject, m.selectedEnv = "proj", "prod"

	view := m.View()

	assert.Contains(t, view, "Use the setup agent to add initialization code")
	assert.NotContains(t, view, "template")
}

func TestCredentialExport_ShowsOnlyTheSDKsCredential(t *testing.T) {
	assert.Equal(t, "export LAUNCHDARKLY_SDK_KEY=sdk-1", credentialExport("python-server-sdk", "sdk-1", "cid", "mob-1"))
	assert.Equal(t, "export LAUNCHDARKLY_CLIENT_SIDE_ID=cid", credentialExport("react-client-sdk", "sdk-1", "cid", "mob-1"))
	assert.Equal(t, "export LAUNCHDARKLY_MOBILE_KEY=mob-1", credentialExport("android", "sdk-1", "cid", "mob-1"))
	assert.Equal(t, "", credentialExport("node-server", "", "cid", "mob-1"), "no value, no line")
}

// OpenCode can ask for several tool calls at once. Each one must get its own
// answer, or OpenCode waits forever.
func TestWizard_AgentPermissions_AreQueued(t *testing.T) {
	run := newFakeRun()
	m := startedAgent(t, run)
	for _, id := range []string{"per1", "per2", "per3"} {
		m, _ = update(t, m, agentEventMsg{run: run, ev: agent.Event{Kind: agent.EventPermission, Permission: agent.PermissionRequest{ID: id, Permission: "bash", Patterns: []string{id}}}})
	}
	view := m.View()
	assert.Contains(t, view, "bash: per1", "the oldest request is shown first")
	assert.Contains(t, view, "2 more waiting")

	for _, key := range []string{"y", "n", "y"} {
		var cmd tea.Cmd
		m, cmd = update(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)})
		require.NotNil(t, cmd)
		cmd()
	}

	assert.Equal(t, []fakeReply{{id: "per1", allow: true}, {id: "per2", allow: false}, {id: "per3", allow: true}}, run.replies)
	assert.Empty(t, m.agentPermissions)
	assert.Contains(t, m.View(), "The agent is working")
}

func secretQuestion() agent.QuestionRequest {
	return agent.QuestionRequest{ID: "que1", Questions: []agent.Question{{
		Header: "Secret storage",
		Text:   "Where should LAUNCHDARKLY_SDK_KEY live?",
		Options: []agent.QuestionOption{
			{Label: ".env (Recommended)", Description: "Added to .env."},
			{Label: "Shell profile", Description: "You export it yourself."},
		},
	}}}
}

func TestWizard_AgentQuestion_NumberAnswers(t *testing.T) {
	run := newFakeRun()
	m := startedAgent(t, run)
	m, _ = update(t, m, agentEventMsg{run: run, ev: agent.Event{Kind: agent.EventQuestion, Question: secretQuestion()}})
	view := m.View()
	assert.Contains(t, view, "Where should LAUNCHDARKLY_SDK_KEY live?")
	assert.Contains(t, view, "1. .env (Recommended)")
	assert.Contains(t, view, "1-2 choose · s skip")

	_, none := update(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("3")})
	assert.Nil(t, none, "a number without an option does nothing")

	m, cmd := update(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("2")})
	require.NotNil(t, cmd)
	cmd()

	assert.Equal(t, []fakeAnswer{{id: "que1", answers: [][]string{{"Shell profile"}}}}, run.answers)
	assert.Empty(t, m.agentQuestions)
}

func TestWizard_AgentQuestion_SSkips(t *testing.T) {
	run := newFakeRun()
	m := startedAgent(t, run)
	m, _ = update(t, m, agentEventMsg{run: run, ev: agent.Event{Kind: agent.EventQuestion, Question: secretQuestion()}})

	_, cmd := update(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("s")})
	require.NotNil(t, cmd)
	cmd()

	assert.Equal(t, []fakeAnswer{{id: "que1", answers: nil}}, run.answers)
}

func TestWizard_AgentQuestion_SeveralQuestionsAnsweredInOrder(t *testing.T) {
	run := newFakeRun()
	m := startedAgent(t, run)
	req := secretQuestion()
	req.Questions = append(req.Questions, agent.Question{Header: "Flag", Text: "Log the value?", Options: []agent.QuestionOption{{Label: "Yes"}, {Label: "No"}}})
	m, _ = update(t, m, agentEventMsg{run: run, ev: agent.Event{Kind: agent.EventQuestion, Question: req}})

	m, cmd := update(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("1")})
	assert.Nil(t, cmd, "the wizard waits for the second answer")
	assert.Contains(t, m.View(), "question 2 of 2")
	_, cmd = update(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("2")})
	require.NotNil(t, cmd)
	cmd()

	assert.Equal(t, []fakeAnswer{{id: "que1", answers: [][]string{{".env (Recommended)"}, {"No"}}}}, run.answers)
}

func presetWizard(project, env string) wizardModel {
	m := agentWizard(&fakeRunner{})
	m.step = stepSelectProject
	m.presetProject, m.presetEnv = project, env
	return m
}

func TestWizard_PresetProjectAndEnvironment_SkipTheScreens(t *testing.T) {
	m := presetWizard("proj", "prod")

	m, cmd := update(t, m, projectsFetchedMsg{projects: []projectItem{{key: "other"}, {key: "proj"}}})
	require.Equal(t, stepSelectEnvironment, m.step)
	assert.Equal(t, "proj", m.selectedProject)
	assert.NotNil(t, cmd, "setup fetches the environments of the preset project")

	m, cmd = update(t, m, envsFetchedMsg{project: "proj", environments: []envItem{{key: "test"}, {key: "prod"}}})
	assert.Equal(t, "prod", m.selectedEnv)
	assert.NotNil(t, cmd, "setup fetches the keys of the preset environment")
	assert.Empty(t, m.presetProject)
	assert.Empty(t, m.presetEnv)
}

func TestWizard_PresetProjectOnly_ShowsTheEnvironmentScreen(t *testing.T) {
	m := presetWizard("proj", "")
	m, _ = update(t, m, projectsFetchedMsg{projects: []projectItem{{key: "proj"}}})

	m, _ = update(t, m, envsFetchedMsg{project: "proj", environments: []envItem{{key: "prod"}}})

	assert.Equal(t, stepSelectEnvironment, m.step)
	assert.Empty(t, m.selectedEnv)
}

func TestWizard_UnknownPreset_StopsWithError(t *testing.T) {
	m, _ := update(t, presetWizard("nope", ""), projectsFetchedMsg{projects: []projectItem{{key: "proj"}}})
	require.Error(t, m.err)
	assert.Contains(t, m.err.Error(), `"nope"`)

	m = presetWizard("proj", "nope")
	m, _ = update(t, m, projectsFetchedMsg{projects: []projectItem{{key: "proj"}}})
	m, _ = update(t, m, envsFetchedMsg{project: "proj", environments: []envItem{{key: "prod"}}})
	require.Error(t, m.err)
	assert.Contains(t, m.err.Error(), "--environment")
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
	assert.Contains(t, out.String(), "The agent asks to use bash: npm test")
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

func TestStreamAgent_AnswersQuestion(t *testing.T) {
	run := newFakeRun(
		agent.Event{Kind: agent.EventQuestion, Question: secretQuestion()},
		agent.Event{Kind: agent.EventDone},
	)
	var out bytes.Buffer

	_, err := streamAgent(context.Background(), run, &out, bufio.NewReader(strings.NewReader("9\n1\n")))

	require.NoError(t, err)
	assert.Equal(t, []fakeAnswer{{id: "que1", answers: [][]string{{".env (Recommended)"}}}}, run.answers, "an invalid number asks again")
	assert.Contains(t, out.String(), "The agent asks: Where should LAUNCHDARKLY_SDK_KEY live?")
}
