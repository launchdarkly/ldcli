package prompt_test

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/launchdarkly/ldcli/cmd"
	"github.com/launchdarkly/ldcli/internal/analytics"
	"github.com/launchdarkly/ldcli/internal/resources"
	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
	synclocal "github.com/launchdarkly/ldcli/internal/sync/local"
	syncmanifest "github.com/launchdarkly/ldcli/internal/sync/manifest"
	syncreference "github.com/launchdarkly/ldcli/internal/sync/reference"
)

type directAPI struct {
	variation                *syncdomain.Variation
	variationState           string
	canonicalizeCreatedModel bool
	requests                 []string
}

type directAPIVariation struct {
	syncdomain.Variation
	State string `json:"state,omitempty"`
}

var _ resources.Client = &directAPI{}

func (api *directAPI) MakeRequest(
	_ string,
	method string,
	path string,
	_ string,
	_ url.Values,
	body []byte,
	_ bool,
) ([]byte, error) {
	api.requests = append(api.requests, method+" "+path)

	switch method {
	case "GET":
		variations := []directAPIVariation{}
		if api.variation != nil {
			variations = append(variations, directAPIVariation{
				Variation: *api.variation,
				State:     api.variationState,
			})
		}
		return json.Marshal(map[string]any{
			"key": "support", "name": "Support", "mode": "agent", "variations": variations,
		})
	case "POST":
		var variation syncdomain.Variation
		if err := json.Unmarshal(body, &variation); err != nil {
			return nil, err
		}
		variation.Mode = syncdomain.VariationModeAgent
		if api.canonicalizeCreatedModel {
			variation.Model = map[string]any{
				"modelName": variation.ModelConfigKey, "parameters": map[string]any{}, "custom": map[string]any{},
			}
		}
		api.variation = &variation
		api.variationState = "published"
	case "PATCH":
		var state struct {
			State string `json:"state"`
		}
		if err := json.Unmarshal(body, &state); err != nil {
			return nil, err
		}
		if state.State == "archived" {
			api.variationState = "archived"
			break
		}

		var update struct {
			Name               string         `json:"name"`
			Instructions       string         `json:"instructions"`
			ModelConfigKey     string         `json:"modelConfigKey"`
			ModelConfigVersion int            `json:"modelConfigVersion"`
			Model              map[string]any `json:"model"`
		}
		if err := json.Unmarshal(body, &update); err != nil {
			return nil, err
		}
		key := "default"
		if api.variation != nil {
			key = api.variation.Key
		}
		api.variation = &syncdomain.Variation{
			Mode: syncdomain.VariationModeAgent, Key: key, Name: update.Name,
			Instructions: update.Instructions, ModelConfigKey: update.ModelConfigKey,
			ModelConfigVersion: update.ModelConfigVersion, Model: update.Model,
		}
		api.variationState = "published"
	}
	return []byte(`{}`), nil
}

func (*directAPI) MakeUnauthenticatedRequest(string, string, []byte) ([]byte, error) {
	return nil, nil
}

func TestPromptDryRunUsesOnlyExistingReadAPI(t *testing.T) {
	root := initRepository(t)
	baseline := variation("Baseline")
	writeVariation(t, root, variation("Local"), true)
	writeManifest(t, root, baseline)
	api := &directAPI{variation: pointer(baseline)}

	stdout, _, err := runPrompt(t, root, api, "--dry-run", "--output", "json")

	require.NoError(t, err)
	assert.Contains(t, stdout, `"action": "update_server"`)
	require.NotEmpty(t, api.requests)
	for _, request := range api.requests {
		assert.True(t, strings.HasPrefix(request, "GET "), request)
		assert.NotContains(t, request, "/sync/")
	}
}

func TestPromptFirstSyncAdoptsMatchingStateWithoutMutation(t *testing.T) {
	root := initRepository(t)
	local := variation("Matching")
	writeVariation(t, root, local, false)
	api := &directAPI{variation: pointer(local)}

	_, _, err := runPrompt(t, root, api, "--yes")

	require.NoError(t, err)
	assertManifestFingerprint(t, root, local)
	for _, request := range api.requests {
		assert.True(t, strings.HasPrefix(request, "GET "), request)
	}
}

func TestPromptFirstSyncCreatesUpsertVariation(t *testing.T) {
	root := initRepository(t)
	local := variation("New")
	writeVariation(t, root, local, true)
	api := &directAPI{}

	_, _, err := runPrompt(t, root, api, "--yes")

	require.NoError(t, err)
	require.NotNil(t, api.variation)
	assert.Equal(t, local.Name, api.variation.Name)
	assertManifestFingerprint(t, root, local)
}

func TestPromptCreateAcceptsSuccessfulServerCanonicalization(t *testing.T) {
	root := initRepository(t)
	local := variation("New")
	local.ModelConfigKey = "gemini"
	writeVariation(t, root, local, true)
	api := &directAPI{canonicalizeCreatedModel: true}

	_, _, err := runPrompt(t, root, api, "--yes")

	require.NoError(t, err)
	require.NotNil(t, api.variation)
	assert.NotEmpty(t, api.variation.Model)
	assertManifestFingerprint(t, root, local)
}

func TestPromptPushesLocalChangeAndAdvancesManifest(t *testing.T) {
	root := initRepository(t)
	baseline := variation("Baseline")
	local := variation("Local")
	writeVariation(t, root, local, true)
	writeManifest(t, root, baseline)
	api := &directAPI{variation: pointer(baseline)}

	stdout, _, err := runPrompt(t, root, api, "--yes", "--output", "json")

	require.NoError(t, err)
	assert.Contains(t, stdout, `"status": "succeeded"`)
	require.NotNil(t, api.variation)
	assert.Equal(t, local.Name, api.variation.Name)
	assertManifestFingerprint(t, root, local)
}

func TestPromptPullsServerChangeAndAdvancesManifest(t *testing.T) {
	root := initRepository(t)
	baseline := variation("Baseline")
	server := variation("Server")
	writeVariation(t, root, baseline, false)
	writeManifest(t, root, baseline)
	api := &directAPI{variation: pointer(server)}

	_, _, err := runPrompt(t, root, api, "--yes")

	require.NoError(t, err)
	resources, err := synclocal.Compile(os.DirFS(root))
	require.NoError(t, err)
	require.Len(t, resources, 1)
	var actual syncdomain.Variation
	require.NoError(t, json.Unmarshal(resources[0].Payload, &actual))
	assert.Equal(t, server.Name, actual.Name)
	assertManifestFingerprint(t, root, server)
}

func TestPromptPushesLinkedFileContent(t *testing.T) {
	root := initRepository(t)
	local := variation("Linked")
	writeLinkedVariation(t, root, local, "Local linked prompt")
	api := &directAPI{}

	_, _, err := runPrompt(t, root, api, "--yes")

	require.NoError(t, err)
	require.NotNil(t, api.variation)
	assert.Equal(t, "Local linked prompt", api.variation.Instructions)
	local.Instructions = "Local linked prompt"
	assertManifestFingerprint(t, root, local)
}

func TestPromptPullsServerContentIntoLinkedFile(t *testing.T) {
	root := initRepository(t)
	baseline := variation("Baseline")
	baseline.Instructions = "Old prompt"
	server := variation("Server")
	server.Instructions = "Server prompt"
	writeLinkedVariation(t, root, baseline, "Old prompt")
	writeManifest(t, root, baseline)
	api := &directAPI{variation: pointer(server)}

	_, _, err := runPrompt(t, root, api, "--yes")

	require.NoError(t, err)
	content, err := os.ReadFile(filepath.Join(root, "prompts", "default.md"))
	require.NoError(t, err)
	assert.Equal(t, "Server prompt\n", string(content))
	assertManifestFingerprint(t, root, server)
}

func TestPromptServerDeletionLeavesReferencedFile(t *testing.T) {
	root := initRepository(t)
	baseline := variation("Baseline")
	baseline.Instructions = "Keep this file"
	writeLinkedVariation(t, root, baseline, "Keep this file")
	writeManifest(t, root, baseline)
	api := &directAPI{}

	_, _, err := runPrompt(t, root, api, "--yes")

	require.NoError(t, err)
	content, err := os.ReadFile(filepath.Join(root, "prompts", "default.md"))
	require.NoError(t, err)
	assert.Equal(t, "Keep this file\n", string(content))
	resources, err := synclocal.CompileWorkspace(root)
	require.NoError(t, err)
	assert.Empty(t, resources)
}

func TestPromptPropagatesTrackedLocalDeletion(t *testing.T) {
	root := initRepository(t)
	baseline := variation("Baseline")
	writeVariation(t, root, baseline, false)
	writeManifest(t, root, baseline)
	_, err := synclocal.NewStore(root).DeleteVariations([]synclocal.VariationDeletion{{
		ProjectKey: "production", ConfigKey: "support", VariationKey: "default",
	}})
	require.NoError(t, err)
	api := &directAPI{variation: pointer(baseline)}

	_, _, err = runPrompt(t, root, api, "--yes")

	require.NoError(t, err)
	require.NotNil(t, api.variation)
	assert.Equal(t, "archived", api.variationState)
	manifest, exists, err := syncmanifest.NewStore(root).Load()
	require.NoError(t, err)
	require.True(t, exists)
	assert.Empty(t, manifest.Resources)
}

func TestPromptPropagatesTrackedServerDeletion(t *testing.T) {
	root := initRepository(t)
	baseline := variation("Baseline")
	writeVariation(t, root, baseline, false)
	writeManifest(t, root, baseline)
	api := &directAPI{}

	_, _, err := runPrompt(t, root, api, "--yes")

	require.NoError(t, err)
	resources, err := synclocal.Compile(os.DirFS(root))
	require.NoError(t, err)
	assert.Empty(t, resources)
	manifest, exists, err := syncmanifest.NewStore(root).Load()
	require.NoError(t, err)
	require.True(t, exists)
	assert.Empty(t, manifest.Resources)
}

func TestPromptRejectsDivergentChangesWithoutMutation(t *testing.T) {
	root := initRepository(t)
	baseline := variation("Baseline")
	writeVariation(t, root, variation("Local"), true)
	writeManifest(t, root, baseline)
	api := &directAPI{variation: pointer(variation("Server"))}

	_, _, err := runPrompt(t, root, api, "--yes")

	require.ErrorContains(t, err, "interactive conflict resolution requires a terminal")
	for _, request := range api.requests {
		assert.True(t, strings.HasPrefix(request, "GET "), request)
	}
}

func TestPromptRevalidatesBeforeWriting(t *testing.T) {
	root := initRepository(t)
	baseline := variation("Baseline")
	writeVariation(t, root, variation("Local"), true)
	writeManifest(t, root, baseline)
	api := &changingReadAPI{directAPI: directAPI{variation: pointer(baseline)}}

	_, _, err := runPrompt(t, root, api, "--yes")

	require.ErrorContains(t, err, "sync state changed after review")
	for _, request := range api.requests {
		assert.True(t, strings.HasPrefix(request, "GET "), request)
	}
}

func TestPromptAcceptsConfirmedWriteAfterAmbiguousError(t *testing.T) {
	root := initRepository(t)
	baseline, local := variation("Baseline"), variation("Local")
	writeVariation(t, root, local, true)
	writeManifest(t, root, baseline)
	api := &ambiguousWriteAPI{directAPI: directAPI{variation: pointer(baseline)}}

	_, _, err := runPrompt(t, root, api, "--yes")

	require.NoError(t, err)
	assert.Equal(t, local.Name, api.variation.Name)
	assertManifestFingerprint(t, root, local)
}

type ambiguousWriteAPI struct {
	directAPI
}

func (api *ambiguousWriteAPI) MakeRequest(
	accessToken string,
	method string,
	path string,
	contentType string,
	query url.Values,
	body []byte,
	beta bool,
) ([]byte, error) {
	response, err := api.directAPI.MakeRequest(accessToken, method, path, contentType, query, body, beta)
	if err == nil && (method == "POST" || method == "PATCH") {
		return nil, fmt.Errorf("connection closed after write")
	}
	return response, err
}

type changingReadAPI struct {
	directAPI
	reads int
}

func (api *changingReadAPI) MakeRequest(
	accessToken string,
	method string,
	path string,
	contentType string,
	query url.Values,
	body []byte,
	beta bool,
) ([]byte, error) {
	if method == "GET" {
		api.reads++
		if api.reads == 2 {
			api.variation = pointer(variation("Concurrent"))
		}
	}
	return api.directAPI.MakeRequest(accessToken, method, path, contentType, query, body, beta)
}

func TestPromptRecordsPartialSuccessAndConvergesOnNextRun(t *testing.T) {
	root := initRepository(t)
	firstBaseline, secondBaseline := variationWithKey("first", "First baseline"), variationWithKey("second", "Second baseline")
	firstLocal, secondLocal := variationWithKey("first", "First local"), variationWithKey("second", "Second local")
	_, err := synclocal.NewStore(root).Add([]synclocal.VariationFile{
		{ProjectKey: "production", ConfigKey: "support", Upsert: true, Variation: firstLocal},
		{ProjectKey: "production", ConfigKey: "support", Upsert: true, Variation: secondLocal},
	})
	require.NoError(t, err)
	writeManifestResources(t, root, firstBaseline, secondBaseline)
	api := &multiDirectAPI{
		variations: map[string]syncdomain.Variation{"first": firstBaseline, "second": secondBaseline},
		failKey:    "second",
	}

	stdout, _, err := runPrompt(t, root, api, "--yes", "--output", "json")

	require.ErrorContains(t, err, "second")
	assert.Contains(t, stdout, `"status": "succeeded"`)
	assert.Contains(t, stdout, `"status": "failed"`)
	manifest, _, loadErr := syncmanifest.NewStore(root).Load()
	require.NoError(t, loadErr)
	require.Len(t, manifest.Resources, 2)
	assert.Equal(t, fingerprint(t, firstLocal), manifest.Resources[0].Fingerprint)
	assert.Equal(t, fingerprint(t, secondBaseline), manifest.Resources[1].Fingerprint)

	api.failKey = ""
	_, _, err = runPrompt(t, root, api, "--yes")

	require.NoError(t, err)
	assertManifestFingerprints(t, root, firstLocal, secondLocal)
}

type multiDirectAPI struct {
	variations map[string]syncdomain.Variation
	failKey    string
	requests   []string
}

func (api *multiDirectAPI) MakeRequest(
	_ string,
	method string,
	path string,
	_ string,
	_ url.Values,
	body []byte,
	_ bool,
) ([]byte, error) {
	api.requests = append(api.requests, method+" "+path)
	if method == "GET" {
		variations := make([]syncdomain.Variation, 0, len(api.variations))
		for _, variation := range api.variations {
			variations = append(variations, variation)
		}
		return json.Marshal(map[string]any{
			"key": "support", "name": "Support", "mode": "agent", "variations": variations,
		})
	}

	key := path[strings.LastIndex(path, "/")+1:]
	if key == api.failKey {
		return nil, fmt.Errorf("write failed for %s", key)
	}
	switch method {
	case "PATCH":
		var update struct {
			Name               string         `json:"name"`
			Instructions       string         `json:"instructions"`
			ModelConfigKey     string         `json:"modelConfigKey"`
			ModelConfigVersion int            `json:"modelConfigVersion"`
			Model              map[string]any `json:"model"`
		}
		if err := json.Unmarshal(body, &update); err != nil {
			return nil, err
		}
		api.variations[key] = syncdomain.Variation{
			Mode: syncdomain.VariationModeAgent, Key: key, Name: update.Name,
			Instructions: update.Instructions, ModelConfigKey: update.ModelConfigKey,
			ModelConfigVersion: update.ModelConfigVersion, Model: update.Model,
		}
	}
	return []byte(`{}`), nil
}

func (*multiDirectAPI) MakeUnauthenticatedRequest(string, string, []byte) ([]byte, error) {
	return nil, nil
}

func runPrompt(t *testing.T, root string, client resources.Client, arguments ...string) (string, string, error) {
	t.Helper()
	t.Chdir(root)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	args := []string{"sync", "prompt", "--access-token", "token", "--base-uri", "https://example.test"}
	args = append(args, arguments...)
	stdout, stderr, err := cmd.CallCmdCapturingStderr(
		t,
		cmd.APIClients{ResourcesClient: client},
		analytics.NoopClientFn{}.Tracker(),
		args,
	)
	return string(stdout), string(stderr), err
}

func initRepository(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	command := exec.Command("git", "init", "--quiet")
	command.Dir = root
	output, err := command.CombinedOutput()
	require.NoError(t, err, "%s", output)
	return root
}

func writeVariation(t *testing.T, root string, value syncdomain.Variation, upsert bool) {
	t.Helper()
	_, err := synclocal.NewStore(root).Add([]synclocal.VariationFile{{
		ProjectKey: "production", ConfigKey: "support", Upsert: upsert, Variation: value,
	}})
	require.NoError(t, err)
}

func writeLinkedVariation(t *testing.T, root string, value syncdomain.Variation, content string) {
	t.Helper()
	referencePath := filepath.Join(root, "prompts", value.Key+".md")
	require.NoError(t, os.MkdirAll(filepath.Dir(referencePath), 0o755))
	require.NoError(t, os.WriteFile(referencePath, []byte(content+"\n"), 0o644))
	value.Instructions = strings.TrimSpace(content)
	_, err := synclocal.NewStore(root).Add([]synclocal.VariationFile{{
		ProjectKey: "production",
		ConfigKey:  "support",
		Upsert:     true,
		Ref: &synclocal.Reference{
			File: "prompts/" + value.Key + ".md", Format: syncreference.PlainMarkdown,
		},
		Variation: value,
	}})
	require.NoError(t, err)
}

func writeManifest(t *testing.T, root string, value syncdomain.Variation) {
	t.Helper()
	writeManifestResources(t, root, value)
}

func writeManifestResources(t *testing.T, root string, values ...syncdomain.Variation) {
	t.Helper()
	resources := make([]syncmanifest.Resource, 0, len(values))
	for _, value := range values {
		resources = append(resources, syncmanifest.Resource{
			ResourceKind: syncdomain.KindVariation,
			ProjectKey:   "production",
			LookupKey:    "support/" + value.Key,
			Fingerprint:  fingerprint(t, value),
		})
	}
	require.NoError(t, syncmanifest.NewStore(root).Write(syncmanifest.Manifest{
		FormatVersion: syncmanifest.FormatVersion,
		Resources:     resources,
	}))
}

func assertManifestFingerprint(t *testing.T, root string, value syncdomain.Variation) {
	assertManifestFingerprints(t, root, value)
}

func assertManifestFingerprints(t *testing.T, root string, values ...syncdomain.Variation) {
	t.Helper()
	manifest, exists, err := syncmanifest.NewStore(root).Load()
	require.NoError(t, err)
	require.True(t, exists)
	require.Len(t, manifest.Resources, len(values))
	for index, value := range values {
		assert.Equal(t, fingerprint(t, value), manifest.Resources[index].Fingerprint)
	}
}

func fingerprint(t *testing.T, value syncdomain.Variation) string {
	t.Helper()
	result, err := syncdomain.FingerprintVariation("production", "support/"+value.Key, value)
	require.NoError(t, err)
	return result
}

func variation(name string) syncdomain.Variation {
	return variationWithKey("default", name)
}

func variationWithKey(key, name string) syncdomain.Variation {
	return syncdomain.Variation{
		Mode: syncdomain.VariationModeAgent, Key: key, Name: name, Instructions: "Help",
	}
}

func pointer[T any](value T) *T {
	return &value
}
