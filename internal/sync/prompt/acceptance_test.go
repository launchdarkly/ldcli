package prompt_test

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
			"--dry-run",
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
	assert.NotContains(t, output[0], "planId")
	assert.NotContains(t, output[0], "expiresAt")
	resources, ok := output[0]["resources"].([]any)
	require.True(t, ok)
	require.Len(t, resources, 1)
	resource, ok := resources[0].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "local_changed", resource["status"])
	assert.NotContains(t, resource, "action")
	assert.NotNil(t, resource["diff"])
}

func TestPromptPlansWithoutDryRunByDefault(t *testing.T) {
	repository := initRepository(t)
	writePrompt(t, repository, "project", "support", "default", true)
	t.Chdir(repository)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	client := &recordingClient{
		Responses: [][]byte{
			[]byte(`{"resources":[{
				"resourceKind":"variation",
				"lookupKey":"support/default",
				"status":"in_sync",
				"syncDirection":"both",
				"manifestUpdateRequired":true
			}]}`),
			[]byte(`{
				"planId": "617c83f1-cd9a-4865-8f37-bb11f88e2147",
				"expiresAt": "2026-12-14T12:00:00Z",
				"resources": [{
					"resourceKind": "variation",
					"lookupKey": "support/default",
					"status": "in_sync",
					"syncDirection": "both",
					"manifestUpdateRequired": true
				}]
			}`),
			[]byte(`{
				"planId": "617c83f1-cd9a-4865-8f37-bb11f88e2147",
				"status": "applied",
				"resources": []
			}`),
		},
	}

	stdout, _, err := cmd.CallCmdCapturingStderr(
		t,
		cmd.APIClients{ResourcesClient: client},
		analytics.NoopClientFn{}.Tracker(),
		[]string{
			"sync", "prompt",
			"--yes",
			"--access-token", "token",
			"--base-uri", "https://example.com",
			"--output", "json",
		},
	)

	require.NoError(t, err)
	require.Len(t, client.Requests, 3)

	var body struct {
		DryRun bool `json:"dryRun"`
	}
	require.NoError(t, json.Unmarshal(client.Requests[0].Body, &body))
	assert.True(t, body.DryRun)
	require.NoError(t, json.Unmarshal(client.Requests[1].Body, &body))
	assert.False(t, body.DryRun)
	assert.Equal(
		t,
		"https://example.com/api/v2/projects/project/ai-configs/sync/apply",
		client.Requests[2].Path,
	)
	var output struct {
		Plans   []map[string]any `json:"plans"`
		Applies []map[string]any `json:"applies"`
	}
	require.NoError(t, json.Unmarshal([]byte(stdout), &output))
	require.Len(t, output.Plans, 1)
	require.Len(t, output.Applies, 1)
	assert.Equal(
		t,
		"617c83f1-cd9a-4865-8f37-bb11f88e2147",
		output.Applies[0]["planId"],
	)
}

func TestPromptPullsServerChangedVariationThenReplansAndApplies(t *testing.T) {
	repository := initRepository(t)
	writePrompt(t, repository, "project", "support", "default", true)
	writePrompt(t, repository, "project", "support", "local", true)
	t.Chdir(repository)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	planID := "617c83f1-cd9a-4865-8f37-bb11f88e2147"
	client := &recordingClient{
		Responses: [][]byte{
			[]byte(`{
				"resources": [{
					"resourceKind": "variation",
					"lookupKey": "support/default",
					"status": "server_changed",
					"syncDirection": "both",
					"diff": {"name": {"before": "From server", "after": "Default"}}
				}, {
					"resourceKind": "variation",
					"lookupKey": "support/local",
					"status": "local_changed",
					"syncDirection": "both",
					"diff": {"name": {"before": "Old local", "after": "Default"}}
				}]
			}`),
			[]byte(`{
				"key": "support",
				"name": "Support",
				"mode": "completion",
				"variations": [{
					"key": "default",
					"name": "From server",
					"modelConfigKey": "claude",
					"modelConfigVersion": 3,
					"messages": [{"role": "system", "content": "Use server state."}]
				}, {
					"key": "local",
					"name": "Old local",
					"messages": [{"role": "system", "content": "Old state."}]
				}]
			}`),
			[]byte(`{
				"planId": "` + planID + `",
				"expiresAt": "2026-12-14T12:00:00Z",
				"resources": [{
					"resourceKind": "variation",
					"lookupKey": "support/default",
					"status": "in_sync",
					"syncDirection": "both"
				}, {
					"resourceKind": "variation",
					"lookupKey": "support/local",
					"status": "local_changed",
					"syncDirection": "both",
					"diff": {"name": {"before": "Old local", "after": "Default"}}
				}]
			}`),
			[]byte(`{
				"planId": "` + planID + `",
				"status": "applied",
				"resources": [{
					"resourceKind": "variation",
					"lookupKey": "support/default",
					"status": "in_sync",
					"syncDirection": "both",
					"outcome": "applied"
				}, {
					"resourceKind": "variation",
					"lookupKey": "support/local",
					"status": "local_changed",
					"syncDirection": "both",
					"outcome": "applied"
				}]
			}`),
		},
	}

	stdout, _, err := cmd.CallCmdCapturingStderr(
		t,
		cmd.APIClients{ResourcesClient: client},
		analytics.NoopClientFn{}.Tracker(),
		[]string{
			"sync", "prompt",
			"--yes",
			"--access-token", "token",
			"--base-uri", "https://example.com",
			"--output", "json",
		},
	)

	require.NoError(t, err)
	require.Len(t, client.Requests, 4)
	assert.Contains(t, client.Requests[0].Path, "/sync/plan")
	assert.Equal(
		t,
		"https://example.com/api/v2/projects/project/ai-configs/support",
		client.Requests[1].Path,
	)
	assert.Contains(t, client.Requests[2].Path, "/sync/plan")
	assert.Contains(t, client.Requests[3].Path, "/sync/apply")

	var durableRequest struct {
		DryRun    bool `json:"dryRun"`
		Resources []struct {
			Payload json.RawMessage `json:"payload"`
		} `json:"resources"`
	}
	require.NoError(t, json.Unmarshal(client.Requests[2].Body, &durableRequest))
	assert.False(t, durableRequest.DryRun)
	require.Len(t, durableRequest.Resources, 2)
	assert.JSONEq(t, `{
		"mode": "completion",
		"key": "default",
		"name": "From server",
		"modelConfigKey": "claude",
		"modelConfigVersion": 3,
		"messages": [{"role": "system", "content": "Use server state."}]
	}`, string(durableRequest.Resources[0].Payload))

	promptPath := filepath.Join(
		repository,
		".launchdarkly",
		"project",
		"configs",
		"support",
		"default.prompt.md",
	)
	content, err := os.ReadFile(promptPath)
	require.NoError(t, err)
	assert.Contains(t, string(content), "upsert: true")
	assert.Contains(t, string(content), "name: From server")
	assert.Contains(t, string(content), "Use server state.")

	var output struct {
		Pulls []struct {
			ProjectKey string `json:"projectKey"`
			LookupKey  string `json:"lookupKey"`
			Path       string `json:"path"`
		} `json:"pulls"`
	}
	require.NoError(t, json.Unmarshal([]byte(stdout), &output))
	require.Len(t, output.Pulls, 1)
	assert.Equal(t, "project", output.Pulls[0].ProjectKey)
	assert.Equal(t, "support/default", output.Pulls[0].LookupKey)
	assert.Equal(
		t,
		"project/configs/support/default.prompt.md",
		output.Pulls[0].Path,
	)
}

func TestPromptDeletesLocalFileForServerDeletionThenRemovesManifest(t *testing.T) {
	repository := initRepository(t)
	writePrompt(t, repository, "project", "support", "default", true)
	t.Chdir(repository)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	planID := "617c83f1-cd9a-4865-8f37-bb11f88e2147"
	client := &recordingClient{
		Responses: [][]byte{
			[]byte(`{
				"resources": [{
					"resourceKind": "variation",
					"lookupKey": "support/default",
					"status": "server_changed",
					"syncDirection": "both",
					"serverDeleted": true,
					"diff": {"name": {"after": "Default"}}
				}]
			}`),
			[]byte(`{
				"planId": "` + planID + `",
				"expiresAt": "2026-12-14T12:00:00Z",
				"resources": [{
					"resourceKind": "variation",
					"lookupKey": "support/default",
					"status": "in_sync",
					"syncDirection": "both",
					"manifestUpdateRequired": true,
					"localDeleted": true,
					"serverDeleted": true
				}]
			}`),
			[]byte(`{
				"planId": "` + planID + `",
				"status": "applied",
				"resources": [{
					"resourceKind": "variation",
					"lookupKey": "support/default",
					"status": "in_sync",
					"syncDirection": "both",
					"outcome": "applied"
				}]
			}`),
		},
	}

	stdout, _, err := cmd.CallCmdCapturingStderr(
		t,
		cmd.APIClients{ResourcesClient: client},
		analytics.NoopClientFn{}.Tracker(),
		[]string{
			"sync", "prompt",
			"--yes",
			"--access-token", "token",
			"--base-uri", "https://example.com",
			"--output", "json",
		},
	)

	require.NoError(t, err)
	require.Len(t, client.Requests, 3)
	assert.Contains(t, client.Requests[0].Path, "/sync/plan")
	assert.Contains(t, client.Requests[1].Path, "/sync/plan")
	assert.Contains(t, client.Requests[2].Path, "/sync/apply")
	_, statErr := os.Stat(filepath.Join(repository, ".launchdarkly"))
	require.ErrorIs(t, statErr, os.ErrNotExist)

	var output struct {
		Pulls []struct {
			Deleted bool `json:"deleted"`
		} `json:"pulls"`
	}
	require.NoError(t, json.Unmarshal([]byte(stdout), &output))
	require.Len(t, output.Pulls, 1)
	assert.True(t, output.Pulls[0].Deleted)
}

func TestPromptDeletesServerVariationForLocalFileDeletion(t *testing.T) {
	repository := initRepository(t)
	writePrompt(t, repository, "project", "support", "default", true)
	require.NoError(t, os.Remove(filepath.Join(
		repository,
		".launchdarkly",
		"project",
		"configs",
		"support",
		"default.prompt.md",
	)))
	t.Chdir(repository)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	planID := "617c83f1-cd9a-4865-8f37-bb11f88e2147"
	deletedPlan := `{
		"resourceKind": "variation",
		"lookupKey": "support/default",
		"status": "local_changed",
		"syncDirection": "both",
		"localDeleted": true,
		"diff": {"name": {"before": "Default"}}
	}`
	client := &recordingClient{
		Responses: [][]byte{
			[]byte(`{"resources": [` + deletedPlan + `]}`),
			[]byte(`{
				"planId": "` + planID + `",
				"expiresAt": "2026-12-14T12:00:00Z",
				"resources": [` + deletedPlan + `]
			}`),
			[]byte(`{
				"planId": "` + planID + `",
				"status": "applied",
				"resources": [{
					"resourceKind": "variation",
					"lookupKey": "support/default",
					"status": "local_changed",
					"syncDirection": "both",
					"outcome": "applied"
				}]
			}`),
		},
	}

	_, _, err := cmd.CallCmdCapturingStderr(
		t,
		cmd.APIClients{ResourcesClient: client},
		analytics.NoopClientFn{}.Tracker(),
		[]string{
			"sync", "prompt",
			"--yes",
			"--access-token", "token",
			"--base-uri", "https://example.com",
			"--output", "json",
		},
	)

	require.NoError(t, err)
	require.Len(t, client.Requests, 3)
	assert.Contains(t, client.Requests[0].Path, "/sync/plan")
	assert.Contains(t, client.Requests[1].Path, "/sync/plan")
	assert.Contains(t, client.Requests[2].Path, "/sync/apply")
	assert.Contains(t, string(client.Requests[0].Body), `"resources": []`)
	_, statErr := os.Stat(filepath.Join(repository, ".launchdarkly"))
	require.ErrorIs(t, statErr, os.ErrNotExist)
}

func TestPromptRestoresServerCanonicalFileDeletedLocally(t *testing.T) {
	repository := initRepository(t)
	writePrompt(t, repository, "project", "support", "default", true)
	promptPath := filepath.Join(
		repository,
		".launchdarkly",
		"project",
		"configs",
		"support",
		"default.prompt.md",
	)
	require.NoError(t, os.Remove(promptPath))
	t.Chdir(repository)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	planID := "617c83f1-cd9a-4865-8f37-bb11f88e2147"
	client := &recordingClient{
		Responses: [][]byte{
			[]byte(`{"resources": [{
				"resourceKind": "variation",
				"lookupKey": "support/default",
				"status": "local_changed",
				"syncDirection": "server_canonical",
				"localDeleted": true,
				"diff": {"name": {"before": "From server"}}
			}]}`),
			[]byte(`{
				"key": "support",
				"name": "Support",
				"mode": "completion",
				"variations": [{
					"key": "default",
					"name": "From server",
					"messages": [{"role": "system", "content": "Restore me."}]
				}]
			}`),
			[]byte(`{
				"planId": "` + planID + `",
				"expiresAt": "2026-12-14T12:00:00Z",
				"resources": [{
					"resourceKind": "variation",
					"lookupKey": "support/default",
					"status": "in_sync",
					"syncDirection": "server_canonical"
				}]
			}`),
			[]byte(`{
				"planId": "` + planID + `",
				"status": "applied",
				"resources": []
			}`),
		},
	}

	_, _, err := cmd.CallCmdCapturingStderr(
		t,
		cmd.APIClients{ResourcesClient: client},
		analytics.NoopClientFn{}.Tracker(),
		[]string{
			"sync", "prompt",
			"--yes",
			"--access-token", "token",
			"--base-uri", "https://example.com",
			"--output", "json",
		},
	)

	require.NoError(t, err)
	require.Len(t, client.Requests, 4)
	content, readErr := os.ReadFile(promptPath)
	require.NoError(t, readErr)
	assert.Contains(t, string(content), "upsert: false")
	assert.Contains(t, string(content), "name: From server")
	assert.Contains(t, string(content), "Restore me.")
}

func TestPromptDoesNotApplyWhenServerChangesDuringPull(t *testing.T) {
	repository := initRepository(t)
	writePrompt(t, repository, "project", "support", "default", true)
	t.Chdir(repository)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	client := &recordingClient{
		Responses: [][]byte{
			[]byte(`{
				"resources": [{
					"resourceKind": "variation",
					"lookupKey": "support/default",
					"status": "server_changed",
					"syncDirection": "both"
				}]
			}`),
			[]byte(`{
				"key": "support",
				"mode": "completion",
				"variations": [{
					"key": "default",
					"name": "Fetched server value",
					"messages": [{"role": "system", "content": "Fetched state."}]
				}]
			}`),
			[]byte(`{
				"planId": "617c83f1-cd9a-4865-8f37-bb11f88e2147",
				"expiresAt": "2026-12-14T12:00:00Z",
				"resources": [{
					"resourceKind": "variation",
					"lookupKey": "support/default",
					"status": "server_changed",
					"syncDirection": "both"
				}]
			}`),
		},
	}

	stdout, _, err := cmd.CallCmdCapturingStderr(
		t,
		cmd.APIClients{ResourcesClient: client},
		analytics.NoopClientFn{}.Tracker(),
		[]string{
			"sync", "prompt",
			"--yes",
			"--access-token", "token",
			"--base-uri", "https://example.com",
			"--output", "json",
		},
	)

	require.ErrorContains(t, err, "server state changed while pulling")
	require.Len(t, client.Requests, 3)
	assert.NotContains(t, client.Requests[2].Path, "/sync/apply")

	var output struct {
		Pulls []map[string]any `json:"pulls"`
		Plans []map[string]any `json:"plans"`
	}
	require.NoError(t, json.Unmarshal([]byte(stdout), &output))
	require.Len(t, output.Pulls, 1)
	require.Len(t, output.Plans, 1)
}

func TestPromptAppliesExistingPlanWithInferredProject(t *testing.T) {
	repository := initRepository(t)
	writePrompt(t, repository, "project", "support", "default", true)
	t.Chdir(repository)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	planID := "617c83f1-cd9a-4865-8f37-bb11f88e2147"
	client := &recordingClient{
		Responses: [][]byte{[]byte(`{
			"planId": "` + planID + `",
			"status": "applied",
			"resources": []
		}`)},
	}

	stdout, _, err := cmd.CallCmdCapturingStderr(
		t,
		cmd.APIClients{ResourcesClient: client},
		analytics.NoopClientFn{}.Tracker(),
		[]string{
			"sync", "prompt",
			"--apply", planID,
			"--access-token", "token",
			"--base-uri", "https://example.com",
			"--output", "json",
		},
	)

	require.NoError(t, err)
	require.Len(t, client.Requests, 1)
	assert.Equal(
		t,
		"https://example.com/api/v2/projects/project/ai-configs/sync/apply",
		client.Requests[0].Path,
	)
	assert.NotContains(t, client.Requests[0].Path, "/sync/plan")
	var body struct {
		PlanID string `json:"planId"`
	}
	require.NoError(t, json.Unmarshal(client.Requests[0].Body, &body))
	assert.Equal(t, planID, body.PlanID)
	var output struct {
		Applies []map[string]any `json:"applies"`
	}
	require.NoError(t, json.Unmarshal([]byte(stdout), &output))
	require.Len(t, output.Applies, 1)
}

func TestPromptApplyRequiresProjectForMultipleWorkspaceProjects(t *testing.T) {
	repository := initRepository(t)
	writePrompt(t, repository, "alpha", "support", "first", true)
	writePrompt(t, repository, "zeta", "support", "second", true)
	t.Chdir(repository)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	client := &recordingClient{}

	_, _, err := cmd.CallCmdCapturingStderr(
		t,
		cmd.APIClients{ResourcesClient: client},
		analytics.NoopClientFn{}.Tracker(),
		[]string{
			"sync", "prompt",
			"--apply", "617c83f1-cd9a-4865-8f37-bb11f88e2147",
			"--access-token", "token",
		},
	)

	require.ErrorContains(t, err, "--project is required")
	assert.Empty(t, client.Requests)
}

func TestPromptApplyUsesExplicitProjectWithoutWorkspace(t *testing.T) {
	workspace := initRepository(t)
	t.Chdir(workspace)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	planID := "617c83f1-cd9a-4865-8f37-bb11f88e2147"
	client := &recordingClient{
		Responses: [][]byte{[]byte(`{
			"planId": "` + planID + `",
			"status": "applied",
			"resources": []
		}`)},
	}

	_, _, err := cmd.CallCmdCapturingStderr(
		t,
		cmd.APIClients{ResourcesClient: client},
		analytics.NoopClientFn{}.Tracker(),
		[]string{
			"sync", "prompt",
			"--apply", planID,
			"--project", "explicit-project",
			"--access-token", "token",
			"--base-uri", "https://example.com",
		},
	)

	require.NoError(t, err)
	require.Len(t, client.Requests, 1)
	assert.Contains(t, client.Requests[0].Path, "/projects/explicit-project/")
}

func TestPromptApplyRejectsIncompatibleFlags(t *testing.T) {
	repository := initRepository(t)
	writePrompt(t, repository, "project", "support", "default", true)
	t.Chdir(repository)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	client := &recordingClient{}

	_, _, err := cmd.CallCmdCapturingStderr(
		t,
		cmd.APIClients{ResourcesClient: client},
		analytics.NoopClientFn{}.Tracker(),
		[]string{
			"sync", "prompt",
			"--apply", "617c83f1-cd9a-4865-8f37-bb11f88e2147",
			"--dry-run",
			"--access-token", "token",
		},
	)

	require.ErrorContains(t, err, "--apply cannot be used")
	assert.Empty(t, client.Requests)
}

func TestPromptRequiresYesForNonInteractiveApply(t *testing.T) {
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
				"syncDirection": "both"
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
		},
	)

	require.ErrorContains(t, err, "rerun with --yes")
	require.Len(t, client.Requests, 1)
	assert.Contains(t, client.Requests[0].Path, "/sync/plan")
}

func TestPromptSkipsPlanAndApplyWhenEverythingIsTrackedAndInSync(t *testing.T) {
	repository := initRepository(t)
	writePrompt(t, repository, "project", "support", "default", true)
	t.Chdir(repository)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	client := &recordingClient{
		Responses: [][]byte{[]byte(`{
			"resources": [{
				"resourceKind": "variation",
				"lookupKey": "support/default",
				"status": "in_sync",
				"syncDirection": "both",
				"manifestUpdateRequired": false
			}]
		}`)},
	}

	_, stderr, err := cmd.CallCmdCapturingStderr(
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
	require.Len(t, client.Requests, 1)
	assert.NotContains(t, stderr, "Sync these changes?")
	assert.NotContains(t, stderr, "Apply:")
}

func TestPromptAppliesWithoutConfirmationWhenManifestUpdateIsRequired(t *testing.T) {
	repository := initRepository(t)
	writePrompt(t, repository, "project", "support", "default", true)
	t.Chdir(repository)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	planID := "617c83f1-cd9a-4865-8f37-bb11f88e2147"
	client := &recordingClient{
		Responses: [][]byte{
			[]byte(`{
				"resources": [{
					"resourceKind": "variation",
					"lookupKey": "support/default",
					"status": "in_sync",
					"syncDirection": "both",
					"manifestUpdateRequired": true
				}]
			}`),
			[]byte(`{
				"planId": "` + planID + `",
				"expiresAt": "2026-12-14T12:00:00Z",
				"resources": [{
					"resourceKind": "variation",
					"lookupKey": "support/default",
					"status": "in_sync",
					"syncDirection": "both",
					"manifestUpdateRequired": true
				}]
			}`),
			[]byte(`{
				"planId": "` + planID + `",
				"status": "applied",
				"resources": [{
					"resourceKind": "variation",
					"lookupKey": "support/default",
					"status": "in_sync",
					"syncDirection": "both",
					"outcome": "applied"
				}]
			}`),
		},
	}

	_, stderr, err := cmd.CallCmdCapturingStderr(
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
	require.Len(t, client.Requests, 3)
	assert.NotContains(t, stderr, "Sync these changes?")
	assert.Contains(t, client.Requests[2].Path, "/sync/apply")
}

func TestPromptReportsAllResourceErrorsWithoutDisplayingPlan(t *testing.T) {
	repository := initRepository(t)
	writePrompt(t, repository, "project", "support", "first", false)
	writePrompt(t, repository, "project", "support", "second", false)
	t.Chdir(repository)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	client := &recordingClient{
		Responses: [][]byte{[]byte(`{
			"resources": [{
				"resourceKind": "variation",
				"lookupKey": "support/first",
				"status": "local_changed",
				"syncDirection": "both",
				"error": {
					"code": "resource_not_found",
					"message": "resource does not exist and upsert is false"
				}
			}, {
				"resourceKind": "variation",
				"lookupKey": "support/second",
				"status": "local_changed",
				"syncDirection": "both",
				"error": {
					"code": "invalid_resource",
					"message": "resource is invalid"
				}
			}]
		}`)},
	}

	stdout, stderr, err := cmd.CallCmdCapturingStderr(
		t,
		cmd.APIClients{ResourcesClient: client},
		analytics.NoopClientFn{}.Tracker(),
		[]string{
			"sync", "prompt",
			"--access-token", "token",
			"--base-uri", "https://example.com",
		},
	)

	require.ErrorContains(t, err, "cannot sync:")
	require.ErrorContains(t, err, "project/support/first: resource_not_found")
	require.ErrorContains(t, err, "project/support/second: invalid_resource")
	require.Len(t, client.Requests, 1)
	assert.Empty(t, stdout)
	assert.NotContains(t, stderr, "Project:")
	assert.NotContains(t, stderr, "Status:")
}

func TestPromptDoesNotApplyUnresolvedConflict(t *testing.T) {
	repository := initRepository(t)
	writePrompt(t, repository, "project", "support", "default", true)
	t.Chdir(repository)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	client := &recordingClient{
		Responses: [][]byte{[]byte(`{
			"resources": [{
				"resourceKind": "variation",
				"lookupKey": "support/default",
				"status": "conflict",
				"syncDirection": "both"
			}]
		}`)},
	}

	_, _, err := cmd.CallCmdCapturingStderr(
		t,
		cmd.APIClients{ResourcesClient: client},
		analytics.NoopClientFn{}.Tracker(),
		[]string{
			"sync", "prompt",
			"--yes",
			"--access-token", "token",
			"--base-uri", "https://example.com",
		},
	)

	require.ErrorContains(t, err, "cannot sync conflicted resource")
	require.Len(t, client.Requests, 1)
}

func TestPromptAppliesEachProjectPlan(t *testing.T) {
	repository := initRepository(t)
	writePrompt(t, repository, "alpha", "support", "first", true)
	writePrompt(t, repository, "zeta", "support", "second", true)
	t.Chdir(repository)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	alphaPlanID := "617c83f1-cd9a-4865-8f37-bb11f88e2147"
	zetaPlanID := "91929a37-79de-4eba-bc73-07c10fa87f2f"
	client := &recordingClient{
		Responses: [][]byte{
			[]byte(`{"resources":[{"resourceKind":"variation","lookupKey":"support/first","status":"in_sync","syncDirection":"both","manifestUpdateRequired":true}]}`),
			[]byte(`{"resources":[{"resourceKind":"variation","lookupKey":"support/second","status":"in_sync","syncDirection":"both","manifestUpdateRequired":true}]}`),
			[]byte(`{"planId":"` + alphaPlanID + `","expiresAt":"2026-12-14T12:00:00Z","resources":[{"resourceKind":"variation","lookupKey":"support/first","status":"in_sync","syncDirection":"both","manifestUpdateRequired":true}]}`),
			[]byte(`{"planId":"` + zetaPlanID + `","expiresAt":"2026-12-14T12:00:00Z","resources":[{"resourceKind":"variation","lookupKey":"support/second","status":"in_sync","syncDirection":"both","manifestUpdateRequired":true}]}`),
			[]byte(`{"planId":"` + alphaPlanID + `","status":"applied","resources":[]}`),
			[]byte(`{"planId":"` + zetaPlanID + `","status":"applied","resources":[]}`),
		},
	}

	_, _, err := cmd.CallCmdCapturingStderr(
		t,
		cmd.APIClients{ResourcesClient: client},
		analytics.NoopClientFn{}.Tracker(),
		[]string{
			"sync", "prompt",
			"--yes",
			"--access-token", "token",
			"--base-uri", "https://example.com",
			"--output", "json",
		},
	)

	require.NoError(t, err)
	require.Len(t, client.Requests, 6)
	assert.Contains(t, client.Requests[0].Path, "/projects/alpha/")
	assert.Contains(t, client.Requests[1].Path, "/projects/zeta/")
	assert.Contains(t, client.Requests[2].Path, "/projects/alpha/")
	assert.Contains(t, client.Requests[3].Path, "/projects/zeta/")
	assert.Contains(t, client.Requests[4].Path, "/projects/alpha/")
	assert.Contains(t, client.Requests[5].Path, "/projects/zeta/")
	assert.Contains(t, string(client.Requests[4].Body), alphaPlanID)
	assert.Contains(t, string(client.Requests[5].Body), zetaPlanID)
}

func TestPromptPreviewRequiresGitRepository(t *testing.T) {
	workspace := t.TempDir()
	writePrompt(t, workspace, "project", "support", "default", false)
	t.Chdir(workspace)

	client := &recordingClient{}

	_, _, err := cmd.CallCmdCapturingStderr(
		t,
		cmd.APIClients{ResourcesClient: client},
		analytics.NoopClientFn{}.Tracker(),
		[]string{
			"sync", "prompt",
			"--dry-run",
			"--access-token", "token",
			"--base-uri", "https://example.com",
			"--output", "json",
		},
	)

	require.ErrorContains(t, err, "git is required to use sync")
	assert.Empty(t, client.Requests)
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
			"--dry-run",
			"--access-token", "token",
			"--base-uri", "https://example.com",
			"--output", "plaintext",
		},
	)

	require.NoError(t, err)
	require.Len(t, client.Requests, 2)
	assert.Contains(t, client.Requests[0].Path, "/projects/alpha/")
	assert.Contains(t, client.Requests[1].Path, "/projects/zeta/")
	assert.Contains(t, string(stdout), "LaunchDarkly changes detected")
	assert.Contains(t, string(stdout), "Update the local file from LaunchDarkly")
	assert.NotContains(t, string(stdout), "Direction:")
	assert.NotContains(t, string(stdout), "server_changed")
	assert.Contains(t, string(stdout), "--- LaunchDarkly now")
	assert.Contains(
		t,
		string(stdout),
		"+++ LaunchDarkly after sync (from local file)",
	)
}

func TestPromptPreviewSendsEmptyProjectInventory(t *testing.T) {
	repository := initRepository(t)
	require.NoError(t, os.MkdirAll(
		filepath.Join(repository, ".launchdarkly", "project"),
		0o755,
	))
	t.Chdir(repository)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	client := &recordingClient{
		Responses: [][]byte{[]byte(`{"resources":[]}`)},
	}
	stdout, _, err := cmd.CallCmdCapturingStderr(
		t,
		cmd.APIClients{ResourcesClient: client},
		analytics.NoopClientFn{}.Tracker(),
		[]string{
			"sync", "prompt",
			"--dry-run",
			"--access-token", "token",
			"--output", "json",
		},
	)

	require.NoError(t, err)
	assert.JSONEq(t, `[{"projectKey":"project","resources":[]}]`, string(stdout))
	require.Len(t, client.Requests, 1)
	assert.Contains(t, client.Requests[0].Path, "/projects/project/")
	assert.Contains(t, string(client.Requests[0].Body), `"fullInventory": true`)
	assert.Contains(t, string(client.Requests[0].Body), `"resources": []`)
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
