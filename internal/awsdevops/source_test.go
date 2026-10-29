package awsdevops_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	agenttypes "github.com/aws/aws-sdk-go-v2/service/devopsagent/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/launchdarkly/ldcli/internal/awsdevops"
)

// testSkill and testAgentPrompt stand in for the files on ai-tooling main.
const (
	testSkill = "---\n" +
		"name: experiment-orchestration\n" +
		"description: Run an experiment from a goal to a report.\n" +
		"---\n\n" +
		"# Automated Experimentation\n\n" +
		"Read the flag with `get-flag`, then call `create-experiment`.\n"
	testAgentPrompt = "# Experiment orchestrator\n\nRun the experiment-orchestration skill.\n"
	testDescription = "Run an experiment from a goal to a report."
)

// fakeSources serves ai-tooling from memory. files maps "ref path" to text.
type fakeSources struct {
	files      map[string]string
	commits    map[string][]string
	fileErr    error
	commitsErr error
	calls      []string
}

func newFakeSources() *fakeSources {
	return &fakeSources{files: map[string]string{
		"main " + awsdevops.SkillSourcePath: testSkill,
		"main " + awsdevops.AgentSourcePath: testAgentPrompt,
	}}
}

func (f *fakeSources) File(_ context.Context, ref, path string) (string, error) {
	f.calls = append(f.calls, "File "+ref+" "+path)
	if f.fileErr != nil {
		return "", f.fileErr
	}
	text, ok := f.files[ref+" "+path]
	if !ok {
		return "", awsdevops.ErrSourceNotFound
	}

	return text, nil
}

func (f *fakeSources) Commits(_ context.Context, path string) ([]string, error) {
	f.calls = append(f.calls, "Commits "+path)
	if f.commitsErr != nil {
		return nil, f.commitsErr
	}

	return f.commits[path], nil
}

// addPast puts text in the history of path at sha.
func (f *fakeSources) addPast(path, sha, text string) {
	if f.commits == nil {
		f.commits = map[string][]string{}
	}
	f.commits[path] = append(f.commits[path], sha)
	f.files[sha+" "+path] = text
}

func sourcesOf(clients awsdevops.Clients) *fakeSources {
	return clients.Sources.(*fakeSources)
}

func TestUnknownSkillToolsListsToolsThatTheAllowlistMisses(t *testing.T) {
	text := "Call `get-flag`, then `delete-everything`, `get-flag`, and `run-magic`. " +
		"Plain words like get-flag-x or `not a tool` do not count."

	assert.Equal(t, []string{"delete-everything", "run-magic"}, awsdevops.UnknownSkillTools(text))
}

func TestUnknownSkillToolsIsEmptyWhenTheAllowlistHoldsEveryTool(t *testing.T) {
	assert.Empty(t, awsdevops.UnknownSkillTools(testSkill))
}

// Without GitHub, the plan skips the skill and the custom agent, and setup
// does the other steps.
func TestSetupSkipsSkillAndAgentWhenDownloadFails(t *testing.T) {
	agent := &fakeAgent{}
	clients := newTestClients(agent, newFakeIAM())
	sourcesOf(clients).fileErr = errors.New("unable to download SKILL.md: dial tcp: no route to host")

	plan, err := awsdevops.PlanSetup(context.Background(), clients, reuseOptions(), true)
	require.NoError(t, err)
	assert.Equal(t, awsdevops.SkillSkip, plan.Skill.Action)
	assert.Equal(t, awsdevops.AgentSkip, plan.Agent.Action)
	assert.Equal(t, "Skip skill experiment-orchestration: unable to download SKILL.md: dial tcp: no route to host",
		plan.Text().Skill)
	assert.Contains(t, plan.Text().Agent, "Skip custom agent experiment-orchestrator: unable to download SKILL.md")

	result, err := awsdevops.RunPlan(context.Background(), clients, reuseOptions(), plan)
	require.NoError(t, err)
	assert.Contains(t, agent.calls, "RegisterService")
	assert.Empty(t, agent.createAssets)
	assert.Empty(t, agent.createAgents)
	assert.Equal(t, awsdevops.SkillSkipped, result.SkillAction)
	assert.Contains(t, result.SkillError, "no route to host")
	assert.Equal(t, awsdevops.AgentSkipped, result.AgentAction)
	assert.Contains(t, result.AgentError, "no route to host")
}

// A new space has nothing to compare, but the steps still need the files.
func TestSetupSkipsSkillAndAgentInNewSpaceWhenDownloadFails(t *testing.T) {
	agent := &fakeAgent{}
	clients := newTestClients(agent, newFakeIAM())
	sourcesOf(clients).fileErr = errors.New("GitHub sent 503 Service Unavailable")

	result, err := awsdevops.Setup(context.Background(), clients, awsdevops.SetupOptions{
		AgentSpaceName: "launchdarkly",
		AuthFlow:       "iam",
	})
	require.NoError(t, err)

	assert.Equal(t, "space-1", result.AgentSpaceID)
	assert.Empty(t, agent.createAssets)
	assert.Empty(t, agent.createAgents)
	assert.Equal(t, awsdevops.SkillSkipped, result.SkillAction)
	assert.Equal(t, awsdevops.AgentSkipped, result.AgentAction)
	assert.Contains(t, result.SkillError, "503")
}

// A skill with an earlier version from ai-tooling main has no edits, so setup
// updates it without --replace-skill.
func TestSetupUpdatesSkillWithPastMainVersion(t *testing.T) {
	agent := &fakeAgent{
		assets:     []agenttypes.Asset{skillAsset("ki-ours", "experiment-orchestration")},
		skillFiles: map[string]string{"ki-ours": "# Older instructions"},
	}
	clients := newTestClients(agent, newFakeIAM())
	sources := sourcesOf(clients)
	sources.addPast(awsdevops.SkillSourcePath, "sha-new", testSkill)
	sources.addPast(awsdevops.SkillSourcePath, "sha-deleted", "")
	delete(sources.files, "sha-deleted "+awsdevops.SkillSourcePath)
	sources.addPast(awsdevops.SkillSourcePath, "sha-old", "# Older instructions")

	plan, err := awsdevops.PlanSetup(context.Background(), clients, reuseOptions(), true)
	require.NoError(t, err)
	assert.Equal(t, awsdevops.SkillUpdate, plan.Skill.Action)

	result, err := awsdevops.RunPlan(context.Background(), clients, reuseOptions(), plan)
	require.NoError(t, err)
	require.Len(t, agent.updateAssets, 1)
	assert.Equal(t, awsdevops.SkillUpdated, result.SkillAction)
}

func TestSetupUpdatesAgentWithPastMainVersion(t *testing.T) {
	agent := &fakeAgent{
		assets:     []agenttypes.Asset{agentAsset("ki-agent", "experiment-orchestrator", nil, nil)},
		agentFiles: map[string]string{"ki-agent": "# Older prompt"},
	}
	clients := newTestClients(agent, newFakeIAM())
	sourcesOf(clients).addPast(awsdevops.AgentSourcePath, "sha-old", "# Older prompt")

	result, err := awsdevops.Setup(context.Background(), clients, reuseOptions())
	require.NoError(t, err)

	require.Len(t, agent.updateAgents, 1, result.AgentError)
	assert.Equal(t, awsdevops.AgentUpdated, result.AgentAction)
}

// The history is read only for a text that matches nothing else.
func TestPlanSetupReadsNoHistoryForTheCurrentSkill(t *testing.T) {
	agent := &fakeAgent{
		assets:     []agenttypes.Asset{skillAsset("ki-ours", "experiment-orchestration")},
		skillFiles: map[string]string{"ki-ours": testSkill},
	}
	clients := newTestClients(agent, newFakeIAM())
	sourcesOf(clients).commitsErr = errors.New("history must not be read")

	plan, err := awsdevops.PlanSetup(context.Background(), clients, reuseOptions(), true)
	require.NoError(t, err)

	assert.Equal(t, awsdevops.SkillKeep, plan.Skill.Action)
	assert.NotContains(t, sourcesOf(clients).calls, "Commits "+awsdevops.SkillSourcePath)
}

// Without the history, setup cannot tell an old version from an edit, so it
// skips the step instead of calling it an edit.
func TestSetupSkipsSkillAndAgentWhenHistoryCannotBeRead(t *testing.T) {
	agent := &fakeAgent{
		assets: []agenttypes.Asset{
			skillAsset("ki-ours", "experiment-orchestration"),
			agentAsset("ki-agent", "experiment-orchestrator", nil, []string{"ki-ours"}),
		},
		skillFiles: map[string]string{"ki-ours": "# Edited or older"},
		agentFiles: map[string]string{"ki-agent": "# Edited or older"},
	}
	clients := newTestClients(agent, newFakeIAM())
	sourcesOf(clients).commitsErr = errors.New("unable to list the commits: GitHub sent 403 rate limit exceeded")

	plan, err := awsdevops.PlanSetup(context.Background(), clients, reuseOptions(), true)
	require.NoError(t, err)
	assert.Equal(t, awsdevops.SkillSkip, plan.Skill.Action)
	assert.Contains(t, plan.Text().Skill, "Skip skill experiment-orchestration: ")
	assert.Contains(t, plan.Text().Skill, "rate limit exceeded")
	assert.Equal(t, awsdevops.AgentSkip, plan.Agent.Action)

	result, err := awsdevops.RunPlan(context.Background(), clients, reuseOptions(), plan)
	require.NoError(t, err)
	assert.Empty(t, agent.updateAssets)
	assert.Empty(t, agent.updateAgents)
	assert.Equal(t, awsdevops.SkillSkipped, result.SkillAction)
	assert.Contains(t, result.SkillError, "rate limit exceeded")
	assert.Equal(t, awsdevops.AgentSkipped, result.AgentAction)
}

// A skill that names a tool outside the allowlist still goes in, with a
// warning in the plan and the result.
func TestSetupWarnsWhenSkillNamesUnknownTools(t *testing.T) {
	agent := &fakeAgent{}
	clients := newTestClients(agent, newFakeIAM())
	skill := testSkill + "Then call `launch-rocket` and `delete-everything`.\n"
	sourcesOf(clients).files["main "+awsdevops.SkillSourcePath] = skill

	plan, err := awsdevops.PlanSetup(context.Background(), clients, reuseOptions(), true)
	require.NoError(t, err)
	assert.Equal(t, awsdevops.SkillCreate, plan.Skill.Action)
	assert.Equal(t, "Create skill experiment-orchestration for every agent type in the space. "+
		"The skill names MCP tools that this ldcli version does not allow: delete-everything", plan.Text().Skill)

	result, err := awsdevops.RunPlan(context.Background(), clients, reuseOptions(), plan)
	require.NoError(t, err)
	assert.Equal(t, awsdevops.SkillCreated, result.SkillAction)
	assert.Equal(t, []string{"delete-everything"}, result.SkillUnknownTools)
	assert.Empty(t, result.SkillError)
}

func TestGitHubSourcesReadFilesAndCommits(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/launchdarkly/ai-tooling/main/"+awsdevops.SkillSourcePath:
			_, _ = w.Write([]byte(testSkill))
		case r.URL.Path == "/repos/launchdarkly/ai-tooling/commits" &&
			r.URL.Query().Get("sha") == "main" && r.URL.Query().Get("path") == awsdevops.SkillSourcePath:
			_, _ = w.Write([]byte(`[{"sha":"abc"},{"sha":"def"}]`))
		case r.URL.Path == "/launchdarkly/ai-tooling/main/"+awsdevops.AgentSourcePath:
			w.WriteHeader(http.StatusTooManyRequests)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	sources := awsdevops.GitHubSources{Client: server.Client(), RawURL: server.URL, APIURL: server.URL}

	text, err := sources.File(context.Background(), "main", awsdevops.SkillSourcePath)
	require.NoError(t, err)
	assert.Equal(t, testSkill, text)

	shas, err := sources.Commits(context.Background(), awsdevops.SkillSourcePath)
	require.NoError(t, err)
	assert.Equal(t, []string{"abc", "def"}, shas)

	_, err = sources.File(context.Background(), "main", awsdevops.AgentSourcePath)
	assert.ErrorContains(t, err, "GitHub sent 429 Too Many Requests")

	_, err = sources.File(context.Background(), "abc", awsdevops.AgentSourcePath)
	assert.ErrorIs(t, err, awsdevops.ErrSourceNotFound)
}
