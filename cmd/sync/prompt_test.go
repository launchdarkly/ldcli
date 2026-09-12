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

func (client *recordingClient) MakeRequest(
	_ string,
	method string,
	path string,
	contentType string,
	_ url.Values,
	body []byte,
	_ bool,
) ([]byte, error) {
	client.Requests = append(client.Requests, recordedRequest{
		Method:      method,
		Path:        path,
		ContentType: contentType,
		Body:        append([]byte(nil), body...),
	})

	return client.Responses[len(client.Requests)-1], nil
}

func (*recordingClient) MakeUnauthenticatedRequest(string, string, []byte) ([]byte, error) {
	return nil, nil
}

func TestPromptPreview(t *testing.T) {
	repository := initRepository(t)
	writePrompt(t, repository, "project", "support", "default", true)
	t.Chdir(repository)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	client := &recordingClient{
		Responses: [][]byte{[]byte(`{
			"resources": [{
				"resourceKind": "variation",
				"lookupKey": "support/default",
				"status": "local_changed",
				"syncDirection": "code_canonical",
				"diff": {"name": {"before": "Old", "after": "Default"}}
			}]
		}`)},
	}

	stdout, _, err := cmd.CallCmdCapturingStderr(
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
	require.Len(t, client.Requests, 1)

	request := client.Requests[0]
	assert.Equal(t, "POST", request.Method)
	assert.Equal(
		t,
		"https://example.com/api/v2/projects/project/ai-configs/sync/plan",
		request.Path,
	)
	assert.Equal(t, "application/json", request.ContentType)

	var body struct {
		Source struct {
			Type       string `json:"type"`
			Identifier string `json:"identifier"`
		} `json:"source"`
		DryRun    bool `json:"dryRun"`
		Resources []struct {
			ResourceKind string          `json:"resourceKind"`
			LookupKey    string          `json:"lookupKey"`
			Upsert       bool            `json:"upsert"`
			Payload      json.RawMessage `json:"payload"`
		} `json:"resources"`
	}
	require.NoError(t, json.Unmarshal(request.Body, &body))
	assert.Equal(t, "git", body.Source.Type)
	assert.Equal(t, "github.com/launchdarkly/example", body.Source.Identifier)
	assert.True(t, body.DryRun)
	require.Len(t, body.Resources, 1)
	assert.Equal(t, "variation", body.Resources[0].ResourceKind)
	assert.Equal(t, "support/default", body.Resources[0].LookupKey)
	assert.True(t, body.Resources[0].Upsert)
	assert.JSONEq(t, `{
		"mode": "completion",
		"key": "default",
		"name": "Default",
		"messages": [{"role": "system", "content": "Say hello."}]
	}`, string(body.Resources[0].Payload))
	assert.NotContains(t, string(request.Body), "fingerprint")

	var output []map[string]any
	require.NoError(t, json.Unmarshal([]byte(stdout), &output))
	require.Len(t, output, 1)
	assert.Equal(t, "project", output[0]["projectKey"])
	assert.Equal(t, "local_changed", output[0]["status"])
	assert.NotContains(t, output[0], "action")
	assert.NotNil(t, output[0]["diff"])
}

func TestPromptPreviewUsesLocalSourceOutsideGit(t *testing.T) {
	workspace := t.TempDir()
	writePrompt(t, workspace, "project", "support", "default", false)
	t.Chdir(workspace)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	client := &recordingClient{
		Responses: [][]byte{[]byte(`{
			"resources": [{
				"resourceKind": "variation",
				"lookupKey": "support/default",
				"status": "in_sync",
				"syncDirection": "code_canonical"
			}]
		}`)},
	}

	_, _, err := cmd.CallCmdCapturingStderr(
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
	require.Len(t, client.Requests, 1)

	var body struct {
		Source struct {
			Type       string `json:"type"`
			Identifier string `json:"identifier"`
		} `json:"source"`
	}
	require.NoError(t, json.Unmarshal(client.Requests[0].Body, &body))
	assert.Equal(t, "local", body.Source.Type)
	assert.Regexp(t, `^sha256\.[0-9a-f]{64}$`, body.Source.Identifier)
	assert.NotContains(t, string(client.Requests[0].Body), workspace)
}

func TestPromptPreviewGroupsRequestsByProject(t *testing.T) {
	repository := initRepository(t)
	writePrompt(t, repository, "alpha", "support", "first", false)
	writePrompt(t, repository, "zeta", "support", "second", false)
	t.Chdir(repository)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	client := &recordingClient{
		Responses: [][]byte{
			[]byte(`{"resources":[{"resourceKind":"variation","lookupKey":"support/first","status":"local_changed","syncDirection":"code_canonical","diff":{"name":{"before":"Old","after":"Default"}}}]}`),
			[]byte(`{"resources":[{"resourceKind":"variation","lookupKey":"support/second","status":"server_changed","syncDirection":"server_canonical"}]}`),
		},
	}

	stdout, _, err := cmd.CallCmdCapturingStderr(
		t,
		cmd.APIClients{ResourcesClient: client},
		analytics.NoopClientFn{}.Tracker(),
		[]string{
			"sync", "prompt",
			"--access-token", "token",
			"--base-uri", "https://example.com",
			"--output", "plaintext",
		},
	)

	require.NoError(t, err)
	require.Len(t, client.Requests, 2)
	assert.Contains(t, client.Requests[0].Path, "/projects/alpha/")
	assert.Contains(t, client.Requests[1].Path, "/projects/zeta/")
	assert.Contains(t, string(stdout), "server_changed")
	assert.NotContains(t, string(stdout), "action=")
	assert.Contains(t, string(stdout), "diff=")
}

func TestPromptPreviewWithoutResourcesMakesNoRequest(t *testing.T) {
	repository := initRepository(t)
	require.NoError(t, os.MkdirAll(
		filepath.Join(repository, ".launchdarkly", "project"),
		0o755,
	))
	t.Chdir(repository)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

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
	assert.JSONEq(t, `[]`, string(stdout))
	assert.Empty(t, client.Requests)
}

func initRepository(t *testing.T) string {
	t.Helper()

	root := t.TempDir()
	runGit(t, root, "init", "--quiet")
	runGit(t, root, "remote", "add", "origin", "git@github.com:launchdarkly/example.git")

	return root
}

func writePrompt(
	t *testing.T,
	root string,
	projectKey string,
	configKey string,
	variationKey string,
	upsert bool,
) {
	t.Helper()

	dir := filepath.Join(root, ".launchdarkly", projectKey, "configs", configKey)
	require.NoError(t, os.MkdirAll(dir, 0o755))

	contents := `---
formatVersion: 1
upsert: ` + strconv.FormatBool(upsert) + `
mode: completion
key: ` + variationKey + `
name: Default
---
Say hello.
`
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, variationKey+".prompt.md"),
		[]byte(contents),
		0o644,
	))
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()

	command := exec.Command("git", args...)
	command.Dir = dir
	output, err := command.CombinedOutput()
	require.NoError(t, err, string(output))
}
