package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/launchdarkly/ldcli/internal/resources"
	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
)

func TestClaimForCodeUpsertsCodeCanonicalRules(t *testing.T) {
	transport := &recordingClient{}
	client := NewClient(transport, "token", "https://example.com")

	err := client.ClaimForCode("project", []syncdomain.ResourceID{
		syncdomain.VariationID("project", "support", "default"),
		syncdomain.VariationID("project", "support", "concise"),
	})

	require.NoError(t, err)
	require.Len(t, transport.Requests, 1)
	request := transport.Requests[0]
	assert.Equal(t, "token", request.AccessToken)
	assert.Equal(t, http.MethodPatch, request.Method)
	assert.Equal(t, "https://example.com/api/v2/projects/project/configs/sync/settings", request.Path)
	assert.Equal(t, "application/json", request.ContentType)
	assert.Empty(t, request.Query)
	assert.False(t, request.IsBeta)
	assert.JSONEq(t, `{
		"upserts": [
			{"resourceKind": "variation", "resourcePattern": "support/default", "direction": "code_canonical"},
			{"resourceKind": "variation", "resourcePattern": "support/concise", "direction": "code_canonical"}
		],
		"deletions": []
	}`, string(request.Body))
}

func TestReturnToLaunchDarklyUpsertsServerCanonicalRules(t *testing.T) {
	transport := &recordingClient{}
	client := NewClient(transport, "token", "https://example.com")

	err := client.ReturnToLaunchDarkly("project", []syncdomain.ResourceID{
		syncdomain.VariationID("project", "support", "default"),
	})

	require.NoError(t, err)
	require.Len(t, transport.Requests, 1)
	request := transport.Requests[0]
	assert.Equal(t, http.MethodPatch, request.Method)
	assert.Equal(t, "https://example.com/api/v2/projects/project/configs/sync/settings", request.Path)
	assert.JSONEq(t, `{
		"upserts": [
			{"resourceKind": "variation", "resourcePattern": "support/default", "direction": "server_canonical"}
		],
		"deletions": []
	}`, string(request.Body))
}

func TestClaimForCodeSendsAtMostOneHundredRulesPerRequest(t *testing.T) {
	transport := &recordingClient{}
	client := NewClient(transport, "token", "https://example.com")
	ids := make([]syncdomain.ResourceID, 0, 150)
	for index := range 150 {
		ids = append(ids, syncdomain.VariationID("project", "support", fmt.Sprintf("variation-%03d", index)))
	}

	require.NoError(t, client.ClaimForCode("project", ids))

	require.Len(t, transport.Requests, 2)
	var patterns []string
	for index, size := range []int{100, 50} {
		var body struct {
			Upserts []struct {
				ResourcePattern string `json:"resourcePattern"`
			} `json:"upserts"`
		}
		require.NoError(t, json.Unmarshal(transport.Requests[index].Body, &body))
		require.Len(t, body.Upserts, size)
		for _, rule := range body.Upserts {
			patterns = append(patterns, rule.ResourcePattern)
		}
	}
	expected := make([]string, 0, len(ids))
	for _, id := range ids {
		expected = append(expected, id.LookupKey)
	}
	assert.Equal(t, expected, patterns, "every rule is sent once, in order")
}

// The rule pattern holds only the lookup key, so a resource of another
// project would get a rule in the wrong project.
func TestClaimForCodeRejectsResourceOfAnotherProject(t *testing.T) {
	transport := &recordingClient{}
	client := NewClient(transport, "token", "https://example.com")

	err := client.ClaimForCode("project", []syncdomain.ResourceID{
		syncdomain.VariationID("project", "support", "default"),
		syncdomain.VariationID("other", "support", "default"),
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), `"project"`)
	assert.Empty(t, transport.Requests)
}

// A 403 names the missing permission. Any other failure must not say that a
// permission is missing.
func TestClaimForCodeExplainsForbidden403(t *testing.T) {
	ids := []syncdomain.ResourceID{syncdomain.VariationID("project", "support", "default")}

	t.Run("403", func(t *testing.T) {
		transport := &recordingClient{Err: errors.New(`{"code":"forbidden","statusCode":403}`)}

		err := NewClient(transport, "token", "https://example.com").ClaimForCode("project", ids)

		require.Error(t, err)
		assert.ErrorIs(t, err, ErrSyncSettingsForbidden)
		assert.True(t, IsForbidden(err), "IsForbidden(%v)", err)
		assert.Contains(t, err.Error(), "updateSyncSettings")
		assert.Contains(t, err.Error(), "proj/project:ai-code-sync/*")
		assert.Contains(t, strings.ToLower(err.Error()), "ask a launchdarkly admin")
	})
	t.Run("500", func(t *testing.T) {
		transport := &recordingClient{Err: errors.New(`{"code":"internal_server_error","statusCode":500}`)}

		err := NewClient(transport, "token", "https://example.com").ClaimForCode("project", ids)

		require.Error(t, err)
		assert.NotErrorIs(t, err, ErrSyncSettingsForbidden)
		assert.False(t, IsForbidden(err))
		assert.NotContains(t, err.Error(), "updateSyncSettings")
	})
}

// The server identifies sync writes by the ldcli User-Agent, so the settings
// write must go through the shared resources client. The test server stands
// in for the LaunchDarkly API, which can't run locally.
func TestSyncSettingsWritesSendCLIUserAgent(t *testing.T) {
	const version = "9.8.7-test"
	var method, path, userAgent string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method, path, userAgent = r.Method, r.URL.Path, r.Header.Get("User-Agent")
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(server.Close)
	client := NewClient(resources.NewClient(version), "api-token", server.URL)

	require.NoError(t, client.ClaimForCode("project", []syncdomain.ResourceID{
		syncdomain.VariationID("project", "support", "default"),
	}))

	assert.Equal(t, http.MethodPatch, method)
	assert.Equal(t, "/api/v2/projects/project/configs/sync/settings", path)
	assert.Equal(t, "launchdarkly-cli/v"+version, userAgent)
}

// A key from a repo file reaches the rule pattern as is, so a pattern
// character such as * would write a wildcard rule. Every key must be a
// LaunchDarkly key, and one bad key sends nothing for the whole batch.
func TestSetSyncDirectionRejectsAKeyThatIsNotALaunchDarklyKey(t *testing.T) {
	for name, bad := range map[string]struct {
		projectKey string
		id         syncdomain.ResourceID
		key        string
	}{
		"config key":    {"project", syncdomain.VariationID("project", "*", "default"), "*"},
		"variation key": {"project", syncdomain.VariationID("project", "support", "de*"), "de*"},
		"project key":   {"proj*", syncdomain.VariationID("proj*", "support", "default"), "proj*"},
	} {
		for direction, write := range map[string]func(Client, string, []syncdomain.ResourceID) error{
			"claim":  Client.ClaimForCode,
			"return": Client.ReturnToLaunchDarkly,
		} {
			t.Run(name+" "+direction, func(t *testing.T) {
				transport := &recordingClient{}
				client := NewClient(transport, "token", "https://example.com")
				ids := []syncdomain.ResourceID{bad.id}
				if bad.projectKey == "project" {
					ids = append([]syncdomain.ResourceID{syncdomain.VariationID("project", "support", "concise")}, ids...)
				}

				err := write(client, bad.projectKey, ids)

				require.Error(t, err)
				assert.Contains(t, err.Error(), fmt.Sprintf("%q", bad.key))
				assert.Empty(t, transport.Requests)
			})
		}
	}
}
