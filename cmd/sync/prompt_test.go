package sync_test

import (
	"encoding/json"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/launchdarkly/ldcli/cmd"
	"github.com/launchdarkly/ldcli/internal/analytics"
	"github.com/launchdarkly/ldcli/internal/resources"
)

type recordedRequest struct {
	Method      string
	Path        string
	ContentType string
	Body        []byte
}

type recordingClient struct {
	Requests  []recordedRequest
	Responses [][]byte
}

var _ resources.Client = &recordingClient{}

func (c *recordingClient) MakeRequest(
	_ string,
	method string,
	path string,
	contentType string,
	_ url.Values,
	body []byte,
	_ bool,
) ([]byte, error) {
	c.Requests = append(c.Requests, recordedRequest{
		Method:      method,
		Path:        path,
		ContentType: contentType,
		Body:        append([]byte(nil), body...),
	})

	response := c.Responses[len(c.Requests)-1]
	return response, nil
}

func (*recordingClient) MakeUnauthenticatedRequest(string, string, []byte) ([]byte, error) {
	return nil, nil
}

func TestPromptStatus(t *testing.T) {
	repo := initRepo(t)
	writePrompt(t, repo, "proj", "config", "hello", true)
	t.Chdir(repo)

	client := &recordingClient{
		Responses: [][]byte{[]byte(`[
			{
				"resourceKind": "variation",
				"lookupKey": "config/hello",
				"status": "local_changed",
				"syncDirection": "code_canonical",
				"serverFingerprint": "sha256.server",
				"error": null
			}
		]`)},
	}

	stdout, stderr, err := cmd.CallCmdCapturingStderr(
		t,
		cmd.APIClients{ResourcesClient: client},
		analytics.NoopClientFn{}.Tracker(),
		[]string{
			"sync", "prompt",
			"--access-token", "token",
			"--base-uri", "https://example.com",
			"--debug",
		},
	)
	require.NoError(t, err)
	require.Len(t, client.Requests, 1)

	request := client.Requests[0]
	assert.Equal(t, "POST", request.Method)
	assert.Equal(t, "https://example.com/api/v2/projects/proj/ai-configs/sync/status", request.Path)
	assert.Equal(t, "application/json", request.ContentType)

	var body struct {
		RepoIdentifier string `json:"repoIdentifier"`
		Resources      []struct {
			Fingerprint  string `json:"fingerprint"`
			ResourceKind string `json:"resourceKind"`
			LookupKey    string `json:"lookupKey"`
			Upsert       bool   `json:"upsert"`
		} `json:"resources"`
	}
	require.NoError(t, json.Unmarshal(request.Body, &body))
	assert.Equal(t, "launchdarkly/ldcli", body.RepoIdentifier)
	require.Len(t, body.Resources, 1)
	assert.Regexp(t, `^sha256\.[0-9a-f]{64}$`, body.Resources[0].Fingerprint)
	assert.Equal(t, "variation", body.Resources[0].ResourceKind)
	assert.Equal(t, "config/hello", body.Resources[0].LookupKey)
	assert.True(t, body.Resources[0].Upsert)
	assert.NotContains(t, string(request.Body), "createIfMissing")

	assert.Empty(t, stdout)

	assert.Contains(t, string(stderr), "Method: POST")
	assert.Contains(t, string(stderr), "Path: /api/v2/projects/proj/ai-configs/sync/status")
	assert.Contains(t, string(stderr), `"repoIdentifier": "launchdarkly/ldcli"`)
	assert.NotContains(t, string(stderr), "token")
}

func TestPromptStatus_DoesNotPrintStatuses(t *testing.T) {
	repo := initRepo(t)
	writePrompt(t, repo, "proj", "config", "hello", false)
	t.Chdir(repo)

	client := &recordingClient{
		Responses: [][]byte{[]byte(`[
			{
				"resourceKind": "variation",
				"lookupKey": "config/hello",
				"status": "in_sync",
				"syncDirection": "code_canonical"
			}
		]`)},
	}

	stdout, stderr, err := cmd.CallCmdCapturingStderr(
		t,
		cmd.APIClients{ResourcesClient: client},
		analytics.NoopClientFn{}.Tracker(),
		[]string{
			"sync", "prompt",
			"--access-token", "token",
			"--base-uri", "https://example.com",
			"--output", "json",
		},
	)
	require.NoError(t, err)
	assert.Empty(t, stdout)
	assert.Empty(t, stderr)
}

func TestPromptStatus_GroupsRequestsByProject(t *testing.T) {
	repo := initRepo(t)
	writePrompt(t, repo, "alpha", "config", "first", false)
	writePrompt(t, repo, "zeta", "config", "second", false)
	t.Chdir(repo)

	client := &recordingClient{
		Responses: [][]byte{
			[]byte(`[{"resourceKind":"variation","lookupKey":"config/first","status":"in_sync","syncDirection":"code_canonical"}]`),
			[]byte(`[{"resourceKind":"variation","lookupKey":"config/second","status":"in_sync","syncDirection":"code_canonical"}]`),
		},
	}

	_, _, err := cmd.CallCmdCapturingStderr(
		t,
		cmd.APIClients{ResourcesClient: client},
		analytics.NoopClientFn{}.Tracker(),
		[]string{
			"sync", "prompt",
			"--access-token", "token",
			"--base-uri", "https://example.com",
		},
	)
	require.NoError(t, err)
	require.Len(t, client.Requests, 2)
	assert.Contains(t, client.Requests[0].Path, "/projects/alpha/")
	assert.Contains(t, client.Requests[1].Path, "/projects/zeta/")
}

func TestPromptStatus_NoResources(t *testing.T) {
	repo := initRepo(t)
	require.NoError(t, os.MkdirAll(filepath.Join(repo, ".launchdarkly", "proj"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(repo, ".launchdarkly", "proj", ".keep"), nil, 0o644))
	t.Chdir(repo)

	client := &recordingClient{}
	stdout, _, err := cmd.CallCmdCapturingStderr(
		t,
		cmd.APIClients{ResourcesClient: client},
		analytics.NoopClientFn{}.Tracker(),
		[]string{
			"sync", "prompt",
			"--access-token", "token",
			"--output", "json",
		},
	)
	require.NoError(t, err)
	assert.Empty(t, stdout)
	assert.Empty(t, client.Requests)
}

func initRepo(t *testing.T) string {
	t.Helper()

	root := t.TempDir()
	runGit(t, root, "init", "--quiet")
	runGit(t, root, "remote", "add", "origin", "git@github.com:launchdarkly/ldcli.git")

	return root
}

func writePrompt(t *testing.T, root, project, config, key string, upsert bool) {
	t.Helper()

	dir := filepath.Join(root, ".launchdarkly", project, "configs", config)
	require.NoError(t, os.MkdirAll(dir, 0o755))

	contents := []byte(`---
formatVersion: 1
upsert: ` + strconv.FormatBool(upsert) + `
mode: completion
key: ` + key + `
name: Test prompt
---
Say hello.
`)
	require.NoError(t, os.WriteFile(filepath.Join(dir, key+".prompt.md"), contents, 0o644))
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()

	command := exec.Command("git", args...)
	command.Dir = dir
	output, err := command.CombinedOutput()
	require.NoError(t, err, string(output))
}
