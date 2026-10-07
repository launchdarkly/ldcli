package prompt_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/launchdarkly/ldcli/cmd"
	"github.com/launchdarkly/ldcli/internal/analytics"
	lderrors "github.com/launchdarkly/ldcli/internal/errors"
	"github.com/launchdarkly/ldcli/internal/resources"
	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
	syncapi "github.com/launchdarkly/ldcli/internal/sync/api"
	synclocal "github.com/launchdarkly/ldcli/internal/sync/local"
	syncmanifest "github.com/launchdarkly/ldcli/internal/sync/manifest"
	syncreference "github.com/launchdarkly/ldcli/internal/sync/reference"
)

type directAPI struct {
	variation      *syncdomain.Variation
	variationState string
	modelConfigs   []syncapi.ModelConfig
	tools          map[string]versionedTool
	skills         map[string]versionedSkill
	manifest       *syncapi.SyncManifest
	requests       []string
}

type notFoundAPI struct{}

func (notFoundAPI) MakeRequest(string, string, string, string, url.Values, []byte, bool) ([]byte, error) {
	return nil, lderrors.NewError(
		`{"code":"not_found","message":"AI config not found","statusCode":404,` +
			`"suggestion":"Resource not found. Verify the selected project."}`,
	)
}

func (notFoundAPI) MakeUnauthenticatedRequest(string, string, []byte) ([]byte, error) {
	return nil, nil
}

type versionedTool struct {
	syncdomain.Tool
	Version int `json:"version"`
}

type versionedSkill struct {
	syncdomain.Skill
	Version int `json:"version"`
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
	query url.Values,
	body []byte,
	_ bool,
) ([]byte, error) {
	api.requests = append(api.requests, method+" "+path)
	if strings.HasSuffix(path, "/configs/sync/manifests") {
		return handleManifestRequest(api.manifest, method, query, body)
	}
	if strings.HasSuffix(path, "/ai-tools") && method == http.MethodPost {
		var tool versionedTool
		if err := json.Unmarshal(body, &tool.Tool); err != nil {
			return nil, err
		}
		tool.Version = 1
		if api.tools == nil {
			api.tools = make(map[string]versionedTool)
		}
		api.tools[tool.Key] = tool
		return json.Marshal(tool)
	}
	if strings.Contains(path, "/ai-tools/") {
		key := path[strings.LastIndex(path, "/")+1:]
		tool, ok := api.tools[key]
		if !ok {
			return nil, fmt.Errorf(`{"code":"not_found","message":"AI tool not found","statusCode":404}`)
		}
		if method == "PATCH" {
			if err := json.Unmarshal(body, &tool.Tool); err != nil {
				return nil, err
			}
			tool.Key = key
			tool.Version++
			api.tools[key] = tool
		}
		return json.Marshal(tool)
	}
	if strings.Contains(path, "/ai-configs/skills/") {
		key := path[strings.LastIndex(path, "/")+1:]
		skill, ok := api.skills[key]
		if !ok {
			return nil, fmt.Errorf("skill not found")
		}
		if method == "PATCH" {
			if err := json.Unmarshal(body, &skill.Skill); err != nil {
				return nil, err
			}
			skill.Key = key
			skill.Version++
			api.skills[key] = skill
		}
		return json.Marshal(skill)
	}
	if method == "GET" && strings.Contains(path, "/model-configs/") {
		for _, modelConfig := range api.modelConfigs {
			if strings.HasSuffix(path, "/"+modelConfig.Key) {
				return json.Marshal(modelConfig)
			}
		}
		return nil, fmt.Errorf("model config not found")
	}

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
			Name               string                      `json:"name"`
			Instructions       string                      `json:"instructions"`
			ModelConfigKey     string                      `json:"modelConfigKey"`
			ModelConfigVersion int                         `json:"modelConfigVersion"`
			Model              map[string]any              `json:"model"`
			OutputFormat       map[string]any              `json:"outputFormat"`
			Tools              *[]syncdomain.AttachmentRef `json:"tools"`
			Skills             *[]syncdomain.AttachmentRef `json:"skills"`
		}
		if err := json.Unmarshal(body, &update); err != nil {
			return nil, err
		}
		key := "default"
		if api.variation != nil {
			key = api.variation.Key
		}
		tools, skills := []syncdomain.AttachmentRef(nil), []syncdomain.AttachmentRef(nil)
		if api.variation != nil {
			tools, skills = api.variation.Tools, api.variation.Skills
		}
		if update.Tools != nil {
			tools = *update.Tools
		}
		if update.Skills != nil {
			skills = *update.Skills
		}
		api.variation = &syncdomain.Variation{
			Mode: syncdomain.VariationModeAgent, Key: key, Name: update.Name,
			Instructions: update.Instructions, ModelConfigKey: update.ModelConfigKey,
			ModelConfigVersion: update.ModelConfigVersion, Model: update.Model,
			OutputFormat: update.OutputFormat, Tools: tools, Skills: skills,
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
	}
}

func TestPromptAddReportsContextForNotFoundConfig(t *testing.T) {
	root := initRepository(t)

	_, _, err := runPrompt(
		t,
		root,
		notFoundAPI{},
		"add",
		"default/this-is-an-agent/this-variation",
		"--output=plaintext",
	)

	require.Error(t, err)
	assert.ErrorContains(t, err, `get config "this-is-an-agent" in project "default"`)
	assert.NotContains(t, err.Error(), "AI config")
	assert.ErrorContains(t, err, "(code: not_found)")
	assert.ErrorContains(t, err, `Suggestion: Verify the resource key and that it belongs to project "default".`)
	assert.NotContains(t, err.Error(), "unknown error occurred")
}

func TestPromptFirstSyncAdoptsMatchingStateWithoutMutation(t *testing.T) {
	root := initRepository(t)
	local := variation("Matching")
	writeVariation(t, root, local, false)
	api := &directAPI{variation: pointer(local)}

	_, _, err := runPrompt(t, root, api, "--yes")

	require.NoError(t, err)
	assertManifestFingerprint(t, root, local)
	requireOnlyReads(t, api.requests)
}

func TestPromptFirstSyncCreatesUpsertVariation(t *testing.T) {
	root := initRepository(t)
	local := variation("New")
	local.ModelConfigKey = "gemini"
	local.Model = map[string]any{"modelName": "gemini"}
	writeVariation(t, root, local, true)
	api := &directAPI{modelConfigs: []syncapi.ModelConfig{{Key: "gemini", ID: "gemini", Version: 4}}}

	_, _, err := runPrompt(t, root, api, "--yes")

	require.NoError(t, err)
	require.NotNil(t, api.variation)
	assert.Equal(t, local.Name, api.variation.Name)
	assert.Equal(t, 4, api.variation.ModelConfigVersion)

	local.ModelConfigVersion = 4
	assertManifestFingerprint(t, root, local)
	requireLocalModelConfigVersion(t, root, 0)

	api.requests = nil
	_, _, err = runPrompt(t, root, api, "--yes")
	require.NoError(t, err)
	requireOnlyReads(t, api.requests)
}

func TestPromptAttachesLatestToolToManagedVariation(t *testing.T) {
	root := initRepository(t)
	baseline := variation("Baseline")
	writeVariation(t, root, baseline, true)
	writeManifest(t, root, baseline)
	tool := syncdomain.Tool{
		Key: "search", Description: pointer("Search documentation"),
		Schema: map[string]any{"type": "object"},
	}
	api := &directAPI{
		variation: pointer(baseline),
		tools: map[string]versionedTool{
			"search": {Tool: tool, Version: 4},
		},
	}

	_, _, err := runPrompt(
		t,
		root,
		api,
		"attach",
		"tool",
		"search",
		"--to=production/support/default",
		"--yes",
	)

	require.NoError(t, err)
	require.Equal(t, []syncdomain.AttachmentRef{{Key: "search", Version: 4}}, api.variation.Tools)

	resources, err := synclocal.CompileWorkspace(root)
	require.NoError(t, err)
	require.Len(t, resources, 1)
	require.Len(t, resources[0].Attachments, 1)
	assert.Equal(t, "search", resources[0].Attachments[0].Key())
	_, err = os.Stat(filepath.Join(root, ".launchdarkly", "production", "tools", "search.json"))
	require.NoError(t, err)

	expected := baseline
	expected.Tools = []syncdomain.AttachmentRef{{Key: "search"}}
	expected.Attachments = []syncdomain.Attachment{toolAttachment(tool, 0)}
	assertManifestFingerprint(t, root, expected)
}

func TestPromptAttachesLatestSkillAsMarkdownFile(t *testing.T) {
	root := initRepository(t)
	baseline := variation("Baseline")
	writeVariation(t, root, baseline, true)
	writeManifest(t, root, baseline)
	description := "Customer support guidance"
	markdown := "Follow the support process.\n"
	skill := syncdomain.Skill{Key: "support", Name: "Support", Description: description, Markdown: markdown}
	api := &directAPI{
		variation: pointer(baseline),
		skills: map[string]versionedSkill{
			"support": {Skill: skill, Version: 3},
		},
	}

	_, _, err := runPrompt(
		t,
		root,
		api,
		"attach",
		"skill",
		"support",
		"--to=production/support/default",
		"--yes",
	)

	require.NoError(t, err)
	require.Equal(t, []syncdomain.AttachmentRef{{Key: "support", Version: 3}}, api.variation.Skills)
	content, err := os.ReadFile(filepath.Join(
		root, ".launchdarkly", "production", "skills", "support.md",
	))
	require.NoError(t, err)
	assert.Equal(t, "---\nkey: support\ndescription: Customer support guidance\n---\n\nFollow the support process.\n", string(content))
}

func TestPromptRejectsSkillAttachmentForCompletionVariation(t *testing.T) {
	root := initRepository(t)
	completion := variation("Completion")
	completion.Mode = syncdomain.VariationModeCompletion
	completion.Instructions = ""
	completion.Messages = []syncdomain.Message{{Role: "system", Content: "Help"}}
	writeVariation(t, root, completion, true)
	skill := syncdomain.Skill{Key: "support", Name: "Support", Markdown: "# Support\n"}
	api := &directAPI{
		skills: map[string]versionedSkill{
			"support": {Skill: skill, Version: 3},
		},
	}

	_, _, err := runPrompt(
		t,
		root,
		api,
		"attach",
		"skill",
		"support",
		"--to=production/support/default",
		"--yes",
	)

	require.ErrorContains(t, err, "skills can only be attached to agent-mode configs")
	assert.Empty(t, api.requests)
	resources, compileErr := synclocal.CompileWorkspace(root)
	require.NoError(t, compileErr)
	require.Len(t, resources, 1)
	assert.Empty(t, resources[0].Attachments)
}

func TestPromptCreatesMissingToolWhenUpsertIsEnabled(t *testing.T) {
	root := initRepository(t)
	baseline := variation("Baseline")
	writeVariation(t, root, baseline, true)
	writeManifest(t, root, baseline)
	attachLocalTool(t, root, baseline, true)
	api := &directAPI{variation: pointer(baseline), tools: map[string]versionedTool{}}

	_, _, err := runPrompt(t, root, api, "--yes")

	require.NoError(t, err)
	require.Contains(t, api.tools, "my-second-tool")
	assert.Equal(t, 1, api.tools["my-second-tool"].Version)
	require.Equal(t, []syncdomain.AttachmentRef{{Key: "my-second-tool", Version: 1}}, api.variation.Tools)
}

func TestPromptExplainsUpsertForMissingTool(t *testing.T) {
	root := initRepository(t)
	baseline := variation("Baseline")
	writeVariation(t, root, baseline, true)
	writeManifest(t, root, baseline)
	attachLocalTool(t, root, baseline, false)
	api := &directAPI{variation: pointer(baseline), tools: map[string]versionedTool{}}

	output, _, err := runPrompt(t, root, api, "--yes")

	require.ErrorContains(t, err, `add "upsert": true`)
	assert.Contains(t, output, `add "upsert": true`)
	assert.NotContains(t, err.Error(), "unknown error")
}

func TestPromptVersionsChangedToolBeforeUpdatingVariation(t *testing.T) {
	root := initRepository(t)
	originalDescription := "Search documentation"
	updatedDescription := "Search all documentation"
	tool := syncdomain.Tool{
		Key: "search", Description: &originalDescription,
		Schema: map[string]any{"type": "object"},
	}
	baseline := variation("Baseline")
	baseline.Tools = []syncdomain.AttachmentRef{{Key: "search", Version: 2}}
	baseline.Attachments = []syncdomain.Attachment{toolAttachment(tool, 2)}
	writeVariation(t, root, baseline, true)
	writeManifest(t, root, baseline)
	api := &directAPI{
		variation: pointer(baseline),
		tools: map[string]versionedTool{
			"search": {Tool: tool, Version: 2},
		},
	}
	toolPath := filepath.Join(root, ".launchdarkly", "production", "tools", "search.json")
	require.NoError(t, os.WriteFile(toolPath, []byte(`{
  "formatVersion": 1,
  "key": "search",
  "description": "Search all documentation",
  "schema": {"type": "object"}
}
`), 0o644))

	_, _, err := runPrompt(t, root, api, "--yes")

	require.NoError(t, err)
	assert.Equal(t, 3, api.tools["search"].Version)
	assert.Equal(t, updatedDescription, *api.tools["search"].Description)
	require.Equal(t, []syncdomain.AttachmentRef{{Key: "search", Version: 3}}, api.variation.Tools)

	expected := baseline
	expected.Attachments[0].Tool.Description = &updatedDescription
	assertManifestFingerprint(t, root, expected)
}

func TestPromptPullsLatestToolAndAdvancesVariationPin(t *testing.T) {
	root := initRepository(t)
	oldDescription := "Search documentation"
	newDescription := "Search all documentation"
	oldTool := syncdomain.Tool{
		Key: "search", Description: &oldDescription,
		Schema: map[string]any{"type": "object"},
	}
	latestTool := oldTool
	latestTool.Description = &newDescription
	baseline := variation("Baseline")
	baseline.Tools = []syncdomain.AttachmentRef{{Key: "search", Version: 2}}
	baseline.Attachments = []syncdomain.Attachment{toolAttachment(oldTool, 2)}
	writeVariation(t, root, baseline, true)
	writeManifest(t, root, baseline)
	serverVariation := baseline
	serverVariation.Attachments = nil
	api := &directAPI{
		variation: &serverVariation,
		tools: map[string]versionedTool{
			"search": {Tool: latestTool, Version: 3},
		},
	}

	_, _, err := runPrompt(t, root, api, "--yes")

	require.NoError(t, err)
	require.Equal(t, []syncdomain.AttachmentRef{{Key: "search", Version: 3}}, api.variation.Tools)
	content, err := os.ReadFile(filepath.Join(root, ".launchdarkly", "production", "tools", "search.json"))
	require.NoError(t, err)
	assert.Contains(t, string(content), "Search all documentation")

	expected := baseline
	expected.Attachments[0] = toolAttachment(latestTool, 3)
	assertManifestFingerprint(t, root, expected)
}

func TestPromptDeletesUnreferencedLocalToolWithYes(t *testing.T) {
	root := initRepository(t)
	tool := syncdomain.Tool{Key: "search", Schema: map[string]any{"type": "object"}}
	baseline := variation("Baseline")
	baseline.Tools = []syncdomain.AttachmentRef{{Key: "search", Version: 2}}
	baseline.Attachments = []syncdomain.Attachment{toolAttachment(tool, 2)}
	writeVariation(t, root, baseline, true)
	writeManifest(t, root, baseline)
	serverVariation := baseline
	serverVariation.Attachments = nil
	api := &directAPI{
		variation: &serverVariation,
		tools: map[string]versionedTool{
			"search": {Tool: tool, Version: 2},
		},
	}
	detached := variation("Baseline")
	_, err := synclocal.NewStore(root).ReplaceVariations([]synclocal.VariationReplacement{{
		ProjectKey: "production", ConfigKey: "support", Variation: detached,
	}})
	require.NoError(t, err)

	_, _, err = runPrompt(t, root, api, "--yes")

	require.NoError(t, err)
	assert.Empty(t, api.variation.Tools)
	assert.Contains(t, api.tools, "search")
	_, err = os.Stat(filepath.Join(root, ".launchdarkly", "production", "tools", "search.json"))
	require.ErrorIs(t, err, os.ErrNotExist)
	assertManifestFingerprint(t, root, detached)
}

func TestPromptUpdateResolvesOmittedModelConfigVersionToLatest(t *testing.T) {
	root := initRepository(t)
	baseline := variation("Matching")
	baseline.ModelConfigKey = "gemini"
	baseline.ModelConfigVersion = 2
	baseline.Model = map[string]any{"modelName": "gemini"}

	local := baseline
	local.ModelConfigVersion = 0
	writeVariation(t, root, local, true)
	writeManifest(t, root, baseline)
	api := &directAPI{
		variation:    pointer(baseline),
		modelConfigs: []syncapi.ModelConfig{{Key: "gemini", ID: "gemini", Version: 4}},
	}

	_, _, err := runPrompt(t, root, api, "--yes")

	require.NoError(t, err)
	require.NotNil(t, api.variation)
	assert.Equal(t, 4, api.variation.ModelConfigVersion)

	local.ModelConfigVersion = 4
	assertManifestFingerprint(t, root, local)
	requireLocalModelConfigVersion(t, root, 0)

	api.requests = nil
	_, _, err = runPrompt(t, root, api, "--yes")
	require.NoError(t, err)
	requireOnlyReads(t, api.requests)
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

func TestPromptRoundTripsOutputFormat(t *testing.T) {
	root := initRepository(t)
	baseline := variation("Baseline")
	server := variation("Server")
	server.OutputFormat = map[string]any{"type": "object"}
	writeVariation(t, root, baseline, false)
	writeManifest(t, root, baseline)
	api := &directAPI{variation: pointer(server)}

	_, _, err := runPrompt(t, root, api, "--yes")
	require.NoError(t, err)

	resources, err := synclocal.Compile(os.DirFS(root))
	require.NoError(t, err)
	require.Len(t, resources, 1)
	var pulled syncdomain.Variation
	require.NoError(t, json.Unmarshal(resources[0].Payload, &pulled))
	assert.Equal(t, server.OutputFormat, pulled.OutputFormat)

	local := server
	local.OutputFormat = map[string]any{"type": "array"}
	_, err = synclocal.NewStore(root).ReplaceVariations([]synclocal.VariationReplacement{{
		ProjectKey: "production", ConfigKey: "support", Variation: local,
	}})
	require.NoError(t, err)

	_, _, err = runPrompt(t, root, api, "--yes")
	require.NoError(t, err)
	assert.Equal(t, local.OutputFormat, api.variation.OutputFormat)

	local.OutputFormat = nil
	_, err = synclocal.NewStore(root).ReplaceVariations([]synclocal.VariationReplacement{{
		ProjectKey: "production", ConfigKey: "support", Variation: local,
	}})
	require.NoError(t, err)

	_, _, err = runPrompt(t, root, api, "--yes")
	require.NoError(t, err)
	assert.Empty(t, api.variation.OutputFormat)
}

func TestPromptPullPreservesLocalModelOverridesAcrossVersions(t *testing.T) {
	root := initRepository(t)
	baseline := variation("Baseline")
	baseline.ModelConfigKey = "gemini"
	baseline.ModelConfigVersion = 4
	baseline.Model = map[string]any{
		"modelName":  "gemini-1",
		"parameters": map[string]any{"temperature": 0.2},
		"custom":     map[string]any{"tone": "friendly"},
	}

	local := baseline
	local.ModelConfigVersion = 0
	local.Model = map[string]any{"custom": map[string]any{"tone": "friendly"}}
	writeVariation(t, root, local, false)
	writeManifest(t, root, baseline)

	server := baseline
	server.Name = "Server"
	server.Model = map[string]any{
		"modelName":  "gemini-1",
		"parameters": map[string]any{"temperature": 0.2},
		"custom":     map[string]any{"tone": "formal"},
	}
	api := &directAPI{
		variation: pointer(server),
		modelConfigs: []syncapi.ModelConfig{{
			Key: "gemini", ID: "gemini-1", Version: 4,
			Params: map[string]any{"temperature": 0.2},
		}},
	}

	_, _, err := runPrompt(t, root, api, "--yes")

	require.NoError(t, err)
	requireLocalModelConfigVersion(t, root, 0)
	assertManifestFingerprint(t, root, server)

	api.modelConfigs[0] = syncapi.ModelConfig{
		Key: "gemini", ID: "gemini-2", Version: 5,
		Params: map[string]any{"temperature": 0.4},
	}
	_, _, err = runPrompt(t, root, api, "--yes")
	require.NoError(t, err)
	require.NotNil(t, api.variation)
	assert.Equal(t, 5, api.variation.ModelConfigVersion)
	assert.Equal(t, map[string]any{
		"modelName":  "gemini-2",
		"parameters": map[string]any{"temperature": 0.4},
		"custom":     map[string]any{"tone": "formal"},
	}, api.variation.Model)
	requireLocalModelConfigVersion(t, root, 0)

	api.requests = nil
	_, _, err = runPrompt(t, root, api, "--yes")
	require.NoError(t, err)
	requireOnlyReads(t, api.requests)
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
	if errors.Is(err, synclocal.ErrNoDirectory) {
		resources = nil
		err = nil
	}
	require.NoError(t, err)
	assert.Empty(t, resources)
}

func TestPromptPropagatesTrackedLocalDeletion(t *testing.T) {
	root := initRepository(t)
	baseline := variation("Baseline")
	writeVariation(t, root, baseline, false)
	writeManifest(t, root, baseline)
	command := exec.Command("git", "add", ".launchdarkly")
	command.Dir = root
	output, err := command.CombinedOutput()
	require.NoError(t, err, "%s", output)
	_, err = synclocal.NewStore(root).DeleteVariations([]synclocal.VariationDeletion{{
		ProjectKey: "production", ConfigKey: "support", VariationKey: "default",
	}})
	require.NoError(t, err)
	api := &directAPI{variation: pointer(baseline)}

	_, _, err = runPrompt(t, root, api, "--yes")

	require.NoError(t, err)
	require.NotNil(t, api.variation)
	assert.Equal(t, "archived", api.variationState)
	assert.Empty(t, manifestsByRoot[root].Items)
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
	if errors.Is(err, synclocal.ErrNoDirectory) {
		resources = nil
		err = nil
	}
	require.NoError(t, err)
	assert.Empty(t, resources)
	assert.Empty(t, manifestsByRoot[root].Items)
}

func TestPromptRejectsDivergentChangesWithoutMutation(t *testing.T) {
	root := initRepository(t)
	baseline := variation("Baseline")
	writeVariation(t, root, variation("Local"), true)
	writeManifest(t, root, baseline)
	api := &directAPI{variation: pointer(variation("Server"))}

	_, _, err := runPrompt(t, root, api, "--yes")

	require.ErrorContains(t, err, "requires --conflict or --resolve")
	requireOnlyReads(t, api.requests)
}

func TestPromptRevalidatesBeforeWriting(t *testing.T) {
	root := initRepository(t)
	baseline := variation("Baseline")
	writeVariation(t, root, variation("Local"), true)
	writeManifest(t, root, baseline)
	api := &changingReadAPI{directAPI: directAPI{variation: pointer(baseline)}}

	_, _, err := runPrompt(t, root, api, "--yes")

	require.ErrorContains(t, err, "sync state changed after review")
	requireOnlyReads(t, api.requests)
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
	if method == "GET" && !strings.Contains(path, "/configs/sync/manifests") {
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
	manifest := manifestsByRoot[root]
	require.Len(t, manifest.Items, 2)
	assert.Equal(t, fingerprint(t, firstLocal), manifest.Items[0].Fingerprint)
	assert.Equal(t, fingerprint(t, secondBaseline), manifest.Items[1].Fingerprint)

	api.failKey = ""
	_, _, err = runPrompt(t, root, api, "--yes")

	require.NoError(t, err)
	assertManifestFingerprints(t, root, firstLocal, secondLocal)
}

type multiDirectAPI struct {
	variations map[string]syncdomain.Variation
	failKey    string
	manifest   *syncapi.SyncManifest
	requests   []string
}

func (api *multiDirectAPI) MakeRequest(
	_ string,
	method string,
	path string,
	_ string,
	query url.Values,
	body []byte,
	_ bool,
) ([]byte, error) {
	api.requests = append(api.requests, method+" "+path)
	if strings.HasSuffix(path, "/configs/sync/manifests") {
		return handleManifestRequest(api.manifest, method, query, body)
	}
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

var manifestsByRoot = map[string]*syncapi.SyncManifest{}

func runPrompt(t *testing.T, root string, client resources.Client, arguments ...string) (string, string, error) {
	t.Helper()
	t.Chdir(root)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	manifest := manifestsByRoot[root]
	if manifest == nil {
		manifest = &syncapi.SyncManifest{Source: "git:example/repo", Items: []syncapi.SyncManifestResource{}}
		manifestsByRoot[root] = manifest
	}
	switch api := client.(type) {
	case *directAPI:
		api.manifest = manifest
	case *multiDirectAPI:
		api.manifest = manifest
	case *ambiguousWriteAPI:
		api.manifest = manifest
	case *changingReadAPI:
		api.manifest = manifest
	}
	args := []string{
		"sync", "prompt",
		"--access-token", "token",
		"--base-uri", "https://example.test",
	}
	args = append(args, arguments...)
	stdout, stderr, err := cmd.CallCmdCapturingStderr(
		t,
		cmd.APIClients{ResourcesClient: client},
		analytics.NoopClientFn{}.Tracker(),
		args,
	)
	return string(stdout), string(stderr), err
}

func handleManifestRequest(
	manifest *syncapi.SyncManifest,
	method string,
	query url.Values,
	body []byte,
) ([]byte, error) {
	if method == http.MethodGet {
		if manifest.Source == "" {
			manifest.Source = query.Get("source")
		}
		return json.Marshal(manifest)
	}

	var request struct {
		Source    string                         `json:"source"`
		Upserts   []syncapi.SyncManifestUpsert   `json:"upserts"`
		Deletions []syncapi.SyncManifestDeletion `json:"deletions"`
	}
	if err := json.Unmarshal(body, &request); err != nil {
		return nil, err
	}
	items := make(map[syncdomain.ResourceID]syncapi.SyncManifestResource, len(manifest.Items))
	for _, item := range manifest.Items {
		items[syncdomain.ResourceID{Kind: item.ResourceKind, LookupKey: item.ResourceLookupKey}] = item
	}
	for _, upsert := range request.Upserts {
		id := syncdomain.ResourceID{Kind: upsert.ResourceKind, LookupKey: upsert.ResourceLookupKey}
		version := 1
		if current, ok := items[id]; ok {
			if current.Version != upsert.Version {
				return nil, fmt.Errorf(`{"code":"conflict","statusCode":409}`)
			}
			version = current.Version + 1
		} else if upsert.Version != 0 {
			return nil, fmt.Errorf(`{"code":"conflict","statusCode":409}`)
		}
		items[id] = syncapi.SyncManifestResource{
			ResourceKind:      upsert.ResourceKind,
			ResourceLookupKey: upsert.ResourceLookupKey,
			Fingerprint:       upsert.Fingerprint,
			Version:           version,
		}
	}
	for _, deletion := range request.Deletions {
		id := syncdomain.ResourceID{Kind: deletion.ResourceKind, LookupKey: deletion.ResourceLookupKey}
		current, ok := items[id]
		if !ok || current.Version != deletion.Version {
			return nil, fmt.Errorf(`{"code":"conflict","statusCode":409}`)
		}
		delete(items, id)
	}

	manifest.Source = request.Source
	manifest.Items = manifest.Items[:0]
	for _, item := range items {
		manifest.Items = append(manifest.Items, item)
	}
	slices.SortFunc(manifest.Items, func(left, right syncapi.SyncManifestResource) int {
		return syncdomain.CompareResourceIDs(
			syncdomain.ResourceID{Kind: left.ResourceKind, LookupKey: left.ResourceLookupKey},
			syncdomain.ResourceID{Kind: right.ResourceKind, LookupKey: right.ResourceLookupKey},
		)
	})
	return json.Marshal(manifest)
}

func initRepository(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for _, args := range [][]string{
		{"init", "--quiet"},
		{"remote", "add", "origin", "git@example:repo.git"},
	} {
		command := exec.Command("git", args...)
		command.Dir = root
		output, err := command.CombinedOutput()
		require.NoError(t, err, "%s", output)
	}
	return root
}

func writeVariation(t *testing.T, root string, value syncdomain.Variation, upsert bool) {
	t.Helper()
	_, err := synclocal.NewStore(root).Add([]synclocal.VariationFile{{
		ProjectKey: "production", ConfigKey: "support", Upsert: upsert, Variation: value,
	}})
	require.NoError(t, err)
}

func attachLocalTool(t *testing.T, root string, variation syncdomain.Variation, upsert bool) {
	t.Helper()
	variation.Tools = []syncdomain.AttachmentRef{{Key: "my-second-tool"}}
	_, err := synclocal.NewStore(root).ReplaceVariations([]synclocal.VariationReplacement{{
		ProjectKey: "production", ConfigKey: "support", Variation: variation,
	}})
	require.NoError(t, err)

	toolPath := filepath.Join(root, ".launchdarkly", "production", "tools", "my-second-tool.json")
	require.NoError(t, os.MkdirAll(filepath.Dir(toolPath), 0o755))
	content := fmt.Sprintf(`{
  "formatVersion": 1,
  "upsert": %t,
  "key": "my-second-tool",
  "description": "This is a test",
  "schema": {"type": "object"}
}
`, upsert)
	require.NoError(t, os.WriteFile(toolPath, []byte(content), 0o644))
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
	manifest := syncmanifest.New()
	for _, value := range values {
		require.NoError(t, manifest.SetAttachments("production", value.Attachments))
		manifest.SetFingerprint(syncdomain.ResourceID{
			Kind: syncdomain.KindVariation, ProjectKey: "production", LookupKey: "support/" + value.Key,
		}, fingerprint(t, value))
	}
	remote := &syncapi.SyncManifest{Source: "git:example/repo"}
	for _, resource := range manifest.Resources {
		remote.Items = append(remote.Items, syncapi.SyncManifestResource{
			ResourceKind:      resource.ResourceKind,
			ResourceLookupKey: resource.LookupKey,
			Fingerprint:       resource.Fingerprint,
			Version:           1,
		})
	}
	manifestsByRoot[root] = remote
}

func assertManifestFingerprint(t *testing.T, root string, value syncdomain.Variation) {
	assertManifestFingerprints(t, root, value)
}

func assertManifestFingerprints(t *testing.T, root string, values ...syncdomain.Variation) {
	t.Helper()
	manifest := manifestsByRoot[root]
	require.NotNil(t, manifest)
	variations := make(map[string]string)
	attachments := make(map[syncdomain.ResourceID]string)
	for _, resource := range manifest.Items {
		if resource.ResourceKind == syncdomain.KindVariation {
			variations[resource.ResourceLookupKey] = resource.Fingerprint
		} else {
			attachments[syncdomain.ResourceID{
				Kind:       resource.ResourceKind,
				ProjectKey: "production",
				LookupKey:  resource.ResourceLookupKey,
			}] = resource.Fingerprint
		}
	}
	require.Len(t, variations, len(values))
	expectedAttachments := make(map[syncdomain.ResourceID]string)
	var err error
	for _, value := range values {
		assert.Equal(t, fingerprint(t, value), variations["support/"+value.Key])
		for _, attachment := range value.Attachments {
			id := syncdomain.ResourceID{
				Kind: syncdomain.Kind(attachment.Kind), ProjectKey: "production", LookupKey: attachment.Key(),
			}
			expectedAttachments[id], err = syncdomain.FingerprintAttachment("production", attachment)
			require.NoError(t, err)
		}
	}
	assert.Equal(t, expectedAttachments, attachments)
}

func requireLocalModelConfigVersion(t *testing.T, root string, expected int) {
	t.Helper()
	resources, err := synclocal.CompileWorkspace(root)
	require.NoError(t, err)
	require.Len(t, resources, 1)

	var persisted syncdomain.Variation
	require.NoError(t, json.Unmarshal(resources[0].Payload, &persisted))
	require.Equal(t, expected, persisted.ModelConfigVersion)
}

func requireOnlyReads(t *testing.T, requests []string) {
	t.Helper()
	require.NotEmpty(t, requests)
	for _, request := range requests {
		require.True(
			t,
			strings.HasPrefix(request, "GET ") ||
				(strings.HasPrefix(request, "PATCH ") && strings.HasSuffix(request, "/configs/sync/manifests")),
			request,
		)
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

func toolAttachment(tool syncdomain.Tool, version int) syncdomain.Attachment {
	return syncdomain.Attachment{Kind: syncdomain.AttachmentTool, Version: version, Tool: &tool}
}

func pointer[T any](value T) *T {
	return &value
}
