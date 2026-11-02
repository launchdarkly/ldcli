package awsdevops_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/launchdarkly/ldcli/internal/awsdevops"
)

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

type fakeSources struct {
	files   map[string]string
	fileErr error
}

func newFakeSources() *fakeSources {
	return &fakeSources{files: map[string]string{
		awsdevops.SkillSourcePath: testSkill,
		awsdevops.AgentSourcePath: testAgentPrompt,
	}}
}

func (f *fakeSources) File(_ context.Context, path string) (string, error) {
	if f.fileErr != nil {
		return "", f.fileErr
	}
	text, ok := f.files[path]
	if !ok {
		return "", errors.New("GitHub sent 404 Not Found")
	}

	return text, nil
}

func sourcesOf(clients awsdevops.Clients) *fakeSources {
	return clients.Sources.(*fakeSources)
}

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

func TestGitHubSourcesReadFiles(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/launchdarkly/ai-tooling/main/"+awsdevops.SkillSourcePath:
			_, _ = w.Write([]byte(testSkill))
		case r.URL.Path == "/launchdarkly/ai-tooling/main/"+awsdevops.AgentSourcePath:
			w.WriteHeader(http.StatusTooManyRequests)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	sources := awsdevops.GitHubSources{Client: server.Client(), RawURL: server.URL}

	text, err := sources.File(context.Background(), awsdevops.SkillSourcePath)
	require.NoError(t, err)
	assert.Equal(t, testSkill, text)

	_, err = sources.File(context.Background(), awsdevops.AgentSourcePath)
	assert.ErrorContains(t, err, "GitHub sent 429 Too Many Requests")
}
