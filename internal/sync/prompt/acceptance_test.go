package prompt_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
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
	syncprompt "github.com/launchdarkly/ldcli/internal/sync/prompt"
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
	// expandTools makes each read of the variation add the content of its
	// tools to model.parameters.tools, as LaunchDarkly does.
	expandTools bool
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
	if strings.HasSuffix(path, "/ai-configs/skills") && method == http.MethodPost {
		var skill versionedSkill
		if err := json.Unmarshal(body, &skill.Skill); err != nil {
			return nil, err
		}
		skill.Version = 1
		if api.skills == nil {
			api.skills = make(map[string]versionedSkill)
		}
		api.skills[skill.Key] = skill
		return json.Marshal(skill)
	}
	if strings.Contains(path, "/ai-configs/skills/") {
		key := path[strings.LastIndex(path, "/")+1:]
		skill, ok := api.skills[key]
		if !ok {
			return nil, fmt.Errorf(`{"code":"not_found","message":"skill not found","statusCode":404}`)
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
			served := *api.variation
			if api.expandTools && len(served.Tools) != 0 {
				served.Model = api.expandedModel(served)
			}
			variations = append(variations, directAPIVariation{
				Variation: served,
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

// expandedModel returns the model of the variation with the content of each
// of its tools in model.parameters.tools.
func (api *directAPI) expandedModel(variation syncdomain.Variation) map[string]any {
	model := maps.Clone(variation.Model)
	if model == nil {
		model = map[string]any{}
	}
	parameters, _ := model["parameters"].(map[string]any)
	parameters = maps.Clone(parameters)
	if parameters == nil {
		parameters = map[string]any{}
	}
	expanded := make([]any, 0, len(variation.Tools))
	for _, ref := range variation.Tools {
		tool := api.tools[ref.Key]
		description := ""
		if tool.Description != nil {
			description = *tool.Description
		}
		expanded = append(expanded, map[string]any{
			"type": "function", "name": tool.Key, "description": description, "parameters": tool.Schema,
		})
	}
	parameters["tools"] = expanded
	model["parameters"] = parameters
	return model
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

func TestPromptDiffExcludesExpandedToolsFromModelParameters(t *testing.T) {
	root := initRepository(t)
	description := "Search the documentation"
	tool := syncdomain.Tool{
		Key:         "search",
		Description: &description,
		Schema:      map[string]any{"type": "object"},
	}
	local := variation("Support")
	local.Instructions = "Use the local instructions."
	local.ModelConfigKey = "claude"
	local.ModelConfigVersion = 2
	local.Model = map[string]any{
		"modelName": "claude-sonnet-4",
		"parameters": map[string]any{
			"temperature": 0.2,
		},
	}
	local.Tools = []syncdomain.AttachmentRef{{Key: "search", Version: 3}}
	local.Attachments = []syncdomain.Attachment{toolAttachment(tool, 3)}
	writeVariation(t, root, local, false)
	writeManifest(t, root, local)

	server := local
	server.Instructions = "Use the LaunchDarkly instructions."
	server.Attachments = nil
	server.Model = map[string]any{
		"modelName": "claude-sonnet-4",
		"parameters": map[string]any{
			"temperature": 0.2,
			"tools": []any{map[string]any{
				"type":        "function",
				"name":        "search",
				"description": description,
				"parameters":  map[string]any{"type": "object"},
			}},
		},
	}
	api := &directAPI{
		variation: &server,
		tools: map[string]versionedTool{
			"search": {Tool: tool, Version: 3},
		},
	}

	stdout, _, err := runPrompt(t, root, api, "--dry-run", "--diff", "--output", "plaintext")

	require.NoError(t, err)
	assert.Contains(t, stdout, "instructions changed in LaunchDarkly")
	assert.NotContains(t, stdout, "model changed in LaunchDarkly")
	assert.NotContains(t, stdout, `"type": "function"`)
	assert.NotContains(t, stdout, `Tool "search"`)

	_, _, err = runPrompt(t, root, api, "--yes")
	require.NoError(t, err)
	resources, err := synclocal.CompileWorkspace(root)
	require.NoError(t, err)
	require.Len(t, resources, 1)
	assert.Equal(t, map[string]any{
		"modelName": "claude-sonnet-4",
		"parameters": map[string]any{
			"temperature": 0.2,
		},
	}, resources[0].Variation.Model)
	assert.Equal(t, []syncdomain.AttachmentRef{{Key: "search"}}, resources[0].Variation.Tools)
}

// A push of a local change also ignores the expanded tools that LaunchDarkly
// returns, sends only the local model, and leaves both sides in sync.
func TestPromptPushExcludesExpandedToolsFromModelParameters(t *testing.T) {
	root := initRepository(t)
	description := "Search the documentation"
	tool := syncdomain.Tool{Key: "search", Description: &description, Schema: map[string]any{"type": "object"}}
	base := variation("Support")
	base.Instructions = "Use the LaunchDarkly instructions."
	base.ModelConfigKey = "claude"
	base.ModelConfigVersion = 2
	base.Model = map[string]any{"modelName": "claude-sonnet-4", "parameters": map[string]any{"temperature": 0.2}}
	base.Tools = []syncdomain.AttachmentRef{{Key: "search", Version: 3}}
	base.Attachments = []syncdomain.Attachment{toolAttachment(tool, 3)}
	local := base
	local.Instructions = "Use the local instructions."
	writeVariation(t, root, local, false)
	writeManifest(t, root, base)

	server := base
	server.Attachments = nil
	api := &directAPI{variation: &server, tools: map[string]versionedTool{"search": {Tool: tool, Version: 3}}, expandTools: true}

	stdout, _, err := runPrompt(t, root, api, "--dry-run", "--diff", "--output", "plaintext")

	require.NoError(t, err)
	assert.Contains(t, stdout, "instructions changed")
	assert.NotContains(t, stdout, "model changed")
	assert.NotContains(t, stdout, `"type": "function"`)

	_, _, err = runPrompt(t, root, api, "--yes")
	require.NoError(t, err)
	assert.Equal(t, local.Instructions, api.variation.Instructions)
	assert.Equal(t, base.Model, api.variation.Model, "the push sends the local model without expanded tools")

	stdout, _, err = runPrompt(t, root, api, "--dry-run", "--output", "plaintext")
	require.NoError(t, err)
	assert.Contains(t, stdout, "No changes.")
}

// LaunchDarkly keeps model parameters and custom values only as objects, so
// sync rejects any other value before a write.
func TestPromptRejectsModelValuesThatAreNotObjects(t *testing.T) {
	for name, test := range map[string]struct {
		model string
		want  string
	}{
		"parameters":       {"  parameters: temperature=0.2\n", "model.parameters must be an object"},
		"empty parameters": {"  parameters:\n", "model.parameters must be an object"},
		"custom":           {"  custom: [retries]\n", "model.custom must be an object"},
	} {
		t.Run(name, func(t *testing.T) {
			root := initRepository(t)
			writeLocalFile(t, root, "configs/support/default.prompt.md", "---\nformatVersion: 1\nupsert: true\n"+
				"mode: agent\nkey: default\nname: Default\nmodel:\n  modelName: gpt-5\n"+test.model+"---\n\nHelp.\n")
			api := &directAPI{}

			_, _, err := runPrompt(t, root, api, "--yes")

			require.ErrorContains(t, err, test.want)
			requireOnlyReads(t, api.requests)
		})
	}
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
	require.Len(t, resources[0].Variation.Attachments, 1)
	assert.Equal(t, "search", resources[0].Variation.Attachments[0].Key())
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
	assert.Empty(t, resources[0].Variation.Attachments)
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

func TestPromptCreatesMissingSkillWhenUpsertIsEnabled(t *testing.T) {
	root := initRepository(t)
	baseline := variation("Baseline")
	writeVariation(t, root, baseline, true)
	writeManifest(t, root, baseline)
	withSkill := baseline
	withSkill.Skills = []syncdomain.AttachmentRef{{Key: "handoff"}}
	_, err := synclocal.NewStore(root).ReplaceVariations([]synclocal.VariationReplacement{{
		ProjectKey: "production", ConfigKey: "support", Variation: withSkill,
	}})
	require.NoError(t, err)
	skillFile := "---\nkey: handoff\ndescription: Hand off to a person\nname: Human handoff\nupsert: true\n---\n\n# Handoff\n"
	writeLocalFile(t, root, "skills/handoff.md", skillFile)
	api := &directAPI{variation: pointer(baseline)}

	_, _, err = runPrompt(t, root, api, "--yes")

	require.NoError(t, err)
	assert.Equal(t, syncdomain.Skill{
		Key: "handoff", Name: "Human handoff", Description: "Hand off to a person", Markdown: "# Handoff\n",
	}, api.skills["handoff"].Skill)
	assert.Equal(t, []syncdomain.AttachmentRef{{Key: "handoff", Version: 1}}, api.variation.Skills)
	content, err := os.ReadFile(filepath.Join(root, syncdomain.RootDir, "production", "skills", "handoff.md"))
	require.NoError(t, err)
	assert.Equal(t, skillFile, string(content))
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
	// A sync keeps the row of a tool that no variation uses. Only a detach
	// removes it.
	searchFingerprint, err := syncdomain.FingerprintAttachment("production", toolAttachment(tool, 2))
	require.NoError(t, err)
	assert.Equal(t, []syncmanifest.Resource{
		{ResourceKind: syncdomain.KindTool, ProjectKey: "production", LookupKey: "search", Fingerprint: searchFingerprint},
		{ResourceKind: syncdomain.KindVariation, ProjectKey: "production", LookupKey: "support/default", Fingerprint: fingerprint(t, detached)},
	}, readLock(t, root).Resources)
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

// A push of a variation that newly attaches a tracked tool must not publish
// the local copy over a change in LaunchDarkly. A dry run plans a pull of the
// tool and changes nothing. The sync then pulls the tool, and the variation
// pins the pulled version.
func TestPromptPushOfANewToolReferenceKeepsTheLaunchDarklyChange(t *testing.T) {
	root := initRepository(t)
	baseline := variation("Baseline")
	writeManifest(t, root, baseline)
	trackTool(t, root, searchTool("old"))
	local := baseline
	local.Tools = []syncdomain.AttachmentRef{{Key: "search"}}
	local.Attachments = []syncdomain.Attachment{toolAttachment(searchTool("old"), 0)}
	writeVariation(t, root, local, true)
	api := &directAPI{variation: pointer(baseline), tools: map[string]versionedTool{"search": {Tool: searchTool("new"), Version: 2}}}

	stdout, _, err := runPrompt(t, root, api, "--dry-run", "--output", "json")
	require.NoError(t, err)
	assert.Equal(t, []string{"tool search update_local", "variation support/default update_server"}, planRows(t, stdout))
	requireOnlyReads(t, api.requests)
	assert.Contains(t, readLocalFile(t, root, "tools/search.json"), `"description": "old"`)

	_, _, err = runPrompt(t, root, api, "--yes")

	require.NoError(t, err)
	assert.Zero(t, countRequests(api.requests, "PATCH", "/ai-tools/search"), "the push must not publish the old local copy")
	assert.Equal(t, "new", *api.tools["search"].Description)
	assert.Equal(t, []syncdomain.AttachmentRef{{Key: "search", Version: 2}}, api.variation.Tools)
	assert.Contains(t, readLocalFile(t, root, "tools/search.json"), `"description": "new"`)
}

// A pull of a variation that newly references a tracked tool must not write
// the LaunchDarkly copy over a local edit. A dry run plans a push of the tool
// and changes nothing. The sync then pushes the tool, and the file keeps the
// edit. The next sync only repins the variation.
func TestPromptPullOfANewToolReferenceKeepsTheLocalEdit(t *testing.T) {
	root := initRepository(t)
	baseline := variation("Baseline")
	writeVariation(t, root, baseline, true)
	writeManifest(t, root, baseline)
	trackTool(t, root, searchTool("old"))
	writeLocalFile(t, root, "tools/search.json",
		`{"formatVersion": 1, "key": "search", "description": "edited", "schema": {"type": "object"}}`)
	server := baseline
	server.Tools = []syncdomain.AttachmentRef{{Key: "search", Version: 1}}
	api := &directAPI{variation: &server, tools: map[string]versionedTool{"search": {Tool: searchTool("old"), Version: 1}}}

	stdout, _, err := runPrompt(t, root, api, "--dry-run", "--output", "json")
	require.NoError(t, err)
	assert.Equal(t, []string{"tool search update_server", "variation support/default update_local"}, planRows(t, stdout))
	requireOnlyReads(t, api.requests)
	assert.Contains(t, readLocalFile(t, root, "tools/search.json"), `"description": "edited"`)

	_, _, err = runPrompt(t, root, api, "--yes")

	require.NoError(t, err)
	assert.Contains(t, readLocalFile(t, root, "tools/search.json"), `"description": "edited"`, "the local edit stays")
	assert.Equal(t, "edited", *api.tools["search"].Description)
	assert.Equal(t, 2, api.tools["search"].Version)
	stdout, _, err = runPrompt(t, root, api, "--dry-run", "--output", "json")
	require.NoError(t, err)
	assert.Equal(t, []string{"variation support/default update_server"}, planRows(t, stdout))
	assert.Contains(t, stdout, `"attachment versions"`, "the variation only repins the tool")
}

// The same holds for a skill.
func TestPromptPullOfANewSkillReferenceKeepsTheLocalEdit(t *testing.T) {
	root := initRepository(t)
	baseline := variation("Baseline")
	writeVariation(t, root, baseline, true)
	writeManifest(t, root, baseline)
	summarize := syncdomain.Skill{Key: "summarize", Name: "Summarize", Description: "Summarize", Markdown: "Old.\n"}
	trackAttachment(t, root, syncdomain.Attachment{Kind: syncdomain.AttachmentSkill, Version: 1, Skill: &summarize})
	writeLocalFile(t, root, "skills/summarize.md", "---\nkey: summarize\ndescription: Summarize\n---\n\nEdited.\n")
	server := baseline
	server.Skills = []syncdomain.AttachmentRef{{Key: "summarize", Version: 1}}
	api := &directAPI{variation: &server, skills: map[string]versionedSkill{"summarize": {Skill: summarize, Version: 1}}}

	_, _, err := runPrompt(t, root, api, "--yes")

	require.NoError(t, err)
	assert.Contains(t, readLocalFile(t, root, "skills/summarize.md"), "Edited.", "the local edit stays")
	assert.Equal(t, "Edited.\n", api.skills["summarize"].Markdown)
}

// When both sides of a newly referenced tool changed, the tool row is a
// conflict, and one choice covers the tool and the variation.
func TestPromptNewToolReferenceWithTwoChangesIsAConflict(t *testing.T) {
	root := initRepository(t)
	baseline := variation("Baseline")
	writeManifest(t, root, baseline)
	trackTool(t, root, searchTool("old"))
	local := baseline
	local.Tools = []syncdomain.AttachmentRef{{Key: "search"}}
	local.Attachments = []syncdomain.Attachment{toolAttachment(searchTool("edited"), 0)}
	writeVariation(t, root, local, true)
	api := &directAPI{variation: pointer(baseline), tools: map[string]versionedTool{"search": {Tool: searchTool("new"), Version: 2}}}

	_, stderr, err := runPrompt(t, root, api, "--yes", "--no-input")

	require.ErrorContains(t, err, "conflict resolution requires --conflict or --resolve without interactive input")
	assert.Contains(t, stderr, "Conflict: tool production/search changed on both sides. "+
		"It shares a changed tool or skill with variation production/support/default, so one choice covers them all.")
	requireOnlyReads(t, api.requests)
}

// A tool that only the local version of a conflicted variation newly uses
// keeps its own row and action. A choice of LaunchDarkly, which does not use
// the tool, drops that row, so the choice does not pull the tool over its
// local edit.
func TestPromptConflictChoiceDoesNotPullAToolThatOnlyTheLocalVersionUses(t *testing.T) {
	root := initRepository(t)
	baseline := variation("Baseline")
	writeManifest(t, root, baseline)
	trackTool(t, root, searchTool("old"))
	local := variation("Local")
	local.Tools = []syncdomain.AttachmentRef{{Key: "search"}}
	local.Attachments = []syncdomain.Attachment{toolAttachment(searchTool("edited"), 0)}
	writeVariation(t, root, local, true)
	server := variation("LaunchDarkly")
	api := &directAPI{variation: &server, tools: map[string]versionedTool{"search": {Tool: searchTool("old"), Version: 1}}}

	stdout, _, err := runPrompt(t, root, api, "--dry-run", "--output", "json")
	require.ErrorContains(t, err, "cannot sync conflicted resource variation production/support/default")
	assert.Equal(t, []string{"tool search update_server", "variation support/default conflict"}, planRows(t, stdout))

	stdout, _, err = runPrompt(t, root, api, "--conflict", "launchdarkly", "--yes", "--no-input", "--output", "json")

	require.NoError(t, err)
	assert.Equal(t, []string{"variation support/default update_local"}, planRows(t, stdout), "the choice does not pull the tool")
	assert.Equal(t, "old", *api.tools["search"].Description)
	assert.Zero(t, countRequests(api.requests, "PATCH", "/ai-tools/search"))
}

// A choice of local for a conflicted variation that newly uses a tool must not
// publish the old local copy over a LaunchDarkly change of the tool. The tool
// row keeps its own pull, and the variation pins the pulled version.
func TestPromptConflictChoiceOfLocalKeepsTheLaunchDarklyChangeOfANewTool(t *testing.T) {
	root := initRepository(t)
	baseline := variation("Baseline")
	writeManifest(t, root, baseline)
	trackTool(t, root, searchTool("old"))
	local := variation("Local")
	local.Tools = []syncdomain.AttachmentRef{{Key: "search"}}
	local.Attachments = []syncdomain.Attachment{toolAttachment(searchTool("old"), 0)}
	writeVariation(t, root, local, true)
	server := variation("LaunchDarkly")
	api := &directAPI{variation: &server, tools: map[string]versionedTool{"search": {Tool: searchTool("new"), Version: 2}}}

	_, _, err := runPrompt(t, root, api, "--conflict", "local", "--yes", "--no-input")

	require.NoError(t, err)
	assert.Zero(t, countRequests(api.requests, "PATCH", "/ai-tools/search"), "the push must not publish the old local copy")
	assert.Equal(t, "new", *api.tools["search"].Description)
	assert.Equal(t, "Local", api.variation.Name)
	assert.Equal(t, []syncdomain.AttachmentRef{{Key: "search", Version: 2}}, api.variation.Tools)
	assert.Contains(t, readLocalFile(t, root, "tools/search.json"), `"description": "new"`)
}

// When a tool that a conflicted variation newly uses changed on both sides,
// the tool and the variation form one conflict with one choice.
func TestPromptConflictWithANewToolThatChangedOnBothSidesIsOneChoice(t *testing.T) {
	root := initRepository(t)
	baseline := variation("Baseline")
	writeManifest(t, root, baseline)
	trackTool(t, root, searchTool("old"))
	local := variation("Local")
	local.Tools = []syncdomain.AttachmentRef{{Key: "search"}}
	local.Attachments = []syncdomain.Attachment{toolAttachment(searchTool("edited"), 0)}
	writeVariation(t, root, local, true)
	server := variation("LaunchDarkly")
	api := &directAPI{variation: &server, tools: map[string]versionedTool{"search": {Tool: searchTool("new"), Version: 2}}}

	_, stderr, err := runPrompt(t, root, api, "--yes", "--no-input")
	require.ErrorContains(t, err, "conflict resolution requires --conflict or --resolve without interactive input")
	assert.Contains(t, stderr, "Conflict: tool production/search changed on both sides. "+
		"It shares a changed tool or skill with variation production/support/default, so one choice covers them all.")

	_, _, err = runPrompt(t, root, api, "--conflict", "local", "--yes", "--no-input")
	require.NoError(t, err)
	assert.Equal(t, "edited", *api.tools["search"].Description)
	assert.Equal(t, []syncdomain.AttachmentRef{{Key: "search", Version: 3}}, api.variation.Tools)
}

// A newly referenced tool whose two sides are equal cannot be overwritten, so
// the plan has no row for it.
func TestPromptNewToolReferenceWithEqualSidesPlansNoToolRow(t *testing.T) {
	root := initRepository(t)
	baseline := variation("Baseline")
	writeManifest(t, root, baseline)
	trackTool(t, root, searchTool("same"))
	local := baseline
	local.Tools = []syncdomain.AttachmentRef{{Key: "search"}}
	local.Attachments = []syncdomain.Attachment{toolAttachment(searchTool("same"), 0)}
	writeVariation(t, root, local, true)
	api := &directAPI{variation: pointer(baseline), tools: map[string]versionedTool{"search": {Tool: searchTool("same"), Version: 1}}}

	stdout, _, err := runPrompt(t, root, api, "--dry-run", "--output", "json")

	require.NoError(t, err)
	assert.Equal(t, []string{"variation support/default update_server"}, planRows(t, stdout))
}

func searchTool(description string) syncdomain.Tool {
	return syncdomain.Tool{Key: "search", Description: pointer(description), Schema: map[string]any{"type": "object"}}
}

// trackTool records the tool in sync.lock and in the remote manifest, as a
// sync that synced it does.
func trackTool(t *testing.T, root string, tool syncdomain.Tool) {
	t.Helper()
	trackAttachment(t, root, toolAttachment(tool, 1))
}

func trackAttachment(t *testing.T, root string, attachment syncdomain.Attachment) {
	t.Helper()
	lock := readLock(t, root)
	require.NoError(t, lock.SetAttachments("production", []syncdomain.Attachment{attachment}))
	require.NoError(t, syncmanifest.WriteLock(synclocal.NewStore(root), lock))
	fingerprint, err := syncdomain.FingerprintAttachment("production", attachment)
	require.NoError(t, err)
	manifestsByRoot[root].Items = append(manifestsByRoot[root].Items, syncapi.SyncManifestResource{
		ResourceKind: syncdomain.Kind(attachment.Kind), ResourceLookupKey: attachment.Key(), Fingerprint: fingerprint, Version: 1,
	})
}

func readLocalFile(t *testing.T, root, projectPath string) string {
	t.Helper()
	return string(must(os.ReadFile(filepath.Join(root, syncdomain.RootDir, "production", filepath.FromSlash(projectPath)))))
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
	actual := resources[0].Variation
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
	pulled := resources[0].Variation
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

// A working copy that is behind another working copy must pull the newer
// LaunchDarkly state. It must not write its older file back to LaunchDarkly.
func TestPromptStaleWorkingCopyPullsInsteadOfReverting(t *testing.T) {
	root := initRepository(t)
	baseline := variation("Baseline")
	writeVariation(t, root, baseline, false)
	writeManifest(t, root, baseline)
	api := &directAPI{variation: pointer(baseline)}
	_, _, err := runPrompt(t, root, api, "--yes")
	require.NoError(t, err)

	// Another working copy syncs a change. The shared remote manifest moves.
	other := variation("Other working copy")
	api.variation = pointer(other)
	writeRemoteManifest(t, root, other)
	manifestsByRoot[root].Items[0].Version = 2

	_, review, err := runPrompt(t, root, api, "--yes")

	require.NoError(t, err)
	assert.Equal(t, other.Name, api.variation.Name)
	assert.False(t, slices.ContainsFunc(api.requests, func(request string) bool {
		return strings.HasPrefix(request, "PATCH ") && strings.Contains(request, "/variations/")
	}), "sync wrote the older local file back to LaunchDarkly")
	assert.Contains(t, review, "Note: Another working copy synced variation production/support/default after your sync.lock.")
	resources, err := synclocal.CompileWorkspace(root)
	require.NoError(t, err)
	require.Len(t, resources, 1)
	assert.Equal(t, other.Name, resources[0].Variation.Name)
}

func TestPromptRestoresLinkedVariationWithItsLink(t *testing.T) {
	root := initRepository(t)
	api := syncLinkedVariation(t, root)
	deleteVariationFile(t, root)

	_, _, err := runPrompt(t, root, api, "--yes")

	require.NoError(t, err)
	resources, err := synclocal.CompileWorkspace(root)
	require.NoError(t, err)
	require.Len(t, resources, 1)
	require.NotNil(t, resources[0].Ref)
	assert.Equal(t, "prompts/default.md", resources[0].Ref.File)

	// An edit in the linked file still reaches LaunchDarkly.
	require.NoError(t, os.WriteFile(filepath.Join(root, "prompts", "default.md"), []byte("Edited prompt\n"), 0o644))
	_, _, err = runPrompt(t, root, api, "--yes")
	require.NoError(t, err)
	assert.Equal(t, "Edited prompt", api.variation.Instructions)
}

func TestPromptStopsRestoreWhenLinkedFileIsNotSafe(t *testing.T) {
	tests := map[string]struct {
		change    func(t *testing.T, linkedFile string)
		wantError string
	}{
		"linked file is missing": {
			change:    func(t *testing.T, linkedFile string) { require.NoError(t, os.Remove(linkedFile)) },
			wantError: `its linked file "prompts/default.md" is not available`,
		},
		"linked file has local edits": {
			change: func(t *testing.T, linkedFile string) {
				require.NoError(t, os.WriteFile(linkedFile, []byte("Local edit\n"), 0o644))
			},
			wantError: `its linked file "prompts/default.md" differs from LaunchDarkly`,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			root := initRepository(t)
			api := syncLinkedVariation(t, root)
			deleteVariationFile(t, root)
			test.change(t, filepath.Join(root, "prompts", "default.md"))

			_, _, err := runPrompt(t, root, api, "--yes")

			require.ErrorContains(t, err, test.wantError)
			assert.Equal(t, "Linked prompt", api.variation.Instructions)
			_, statErr := os.Stat(filepath.Join(root, syncdomain.RootDir, "production", "configs", "support", "default.prompt.md"))
			assert.ErrorIs(t, statErr, os.ErrNotExist)
		})
	}
}

func TestPromptDetachArchive(t *testing.T) {
	tests := map[string]struct {
		arguments []string
		wantError string
	}{
		"archive with --yes": {
			arguments: []string{"--archive", "--yes", "--no-input"},
		},
		"archive without --yes": {
			arguments: []string{"--archive", "--no-input"},
			wantError: "rerun with --yes",
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			root := initRepository(t)
			baseline := variation("Baseline")
			writeVariation(t, root, baseline, false)
			writeManifest(t, root, baseline)
			api := &directAPI{variation: pointer(baseline)}
			_, _, err := runPrompt(t, root, api, "--yes")
			require.NoError(t, err)

			_, _, err = runPrompt(t, root, api, append([]string{"detach", "production/support/default"}, test.arguments...)...)

			variationFile := filepath.Join(root, syncdomain.RootDir, "production", "configs", "support", "default.prompt.md")
			_, statErr := os.Stat(variationFile)
			if test.wantError != "" {
				require.ErrorContains(t, err, test.wantError)
				assert.NotEqual(t, "archived", api.variationState)
				assert.NoError(t, statErr)
				assert.NotEmpty(t, manifestsByRoot[root].Items)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, "archived", api.variationState)
			assert.ErrorIs(t, statErr, os.ErrNotExist)
			assert.Empty(t, manifestsByRoot[root].Items)
			// The lock had only this variation, so detach removes the file.
			_, statErr = os.Stat(filepath.Join(root, syncdomain.RootDir, "sync.lock"))
			assert.ErrorIs(t, statErr, os.ErrNotExist)
		})
	}
}

// syncLinkedVariation writes a linked variation that matches LaunchDarkly and
// runs one sync, so that sync.lock records the link.
func syncLinkedVariation(t *testing.T, root string) *directAPI {
	t.Helper()
	linked := variation("Linked")
	writeLinkedVariation(t, root, linked, "Linked prompt")
	linked.Instructions = "Linked prompt"
	api := &directAPI{variation: pointer(linked)}
	_, _, err := runPrompt(t, root, api, "--yes")
	require.NoError(t, err)
	return api
}

func deleteVariationFile(t *testing.T, root string) {
	t.Helper()
	_, err := synclocal.NewStore(root).DeleteVariations([]synclocal.VariationDeletion{{
		ProjectKey: "production", ConfigKey: "support", VariationKey: "default",
	}})
	require.NoError(t, err)
}

func TestPromptFilterSyncsOnlyTheNamedVariation(t *testing.T) {
	root := initRepository(t)
	baseline := variation("Baseline")
	writeVariation(t, root, variation("Local"), true)
	writeManifest(t, root, baseline)
	other := addOtherProjectVariation(t, root)
	lockBefore := readLock(t, root)
	api := &directAPI{variation: pointer(baseline)}

	_, _, err := runPrompt(t, root, api, "--project", "production", "--config-key", "support", "--key", "default", "--yes")

	require.NoError(t, err)
	assert.Equal(t, "Local", api.variation.Name)
	for _, request := range api.requests {
		assert.NotContains(t, request, "/projects/other/")
	}
	lockAfter := readLock(t, root)
	assert.Contains(t, lockAfter.Resources, other)
	assert.Contains(t, lockBefore.Resources, other)
	assert.NotEqual(t, lockBefore, lockAfter)
}

func TestPromptTargetFlagErrorsComeBeforeAnyRequest(t *testing.T) {
	tests := map[string]struct {
		arguments []string
		want      string
	}{
		"key without project": {[]string{"--key", "default"}, "--key needs --project"},
		"key without config": {
			[]string{"--project", "production", "--key", "default"},
			"--key needs --config-key. In sync prompts, --key is a variation key.",
		},
		"config without project": {[]string{"--config-key", "support"}, "--config-key needs --project"},
		"blank project":          {[]string{"--project", " "}, "--project cannot be blank"},
		"repeated key": {
			[]string{"--project", "production", "--config-key", "support", "--key", "default", "--key", "default"},
			"variation production/support/default was selected more than once",
		},
		"key that is not synced": {
			[]string{"--project", "production", "--config-key", "support", "--key", "serch"},
			"variation production/support/serch is not synced in this repository. " +
				"To start syncing it, run ldcli sync prompts add --project production --config-key support --key serch.",
		},
		"project that is not synced": {
			[]string{"--project", "demo"},
			`project "demo" has no synced resources in this repository. To start, run ldcli sync prompts add --project demo.`,
		},
		"config that is not synced": {
			[]string{"--project", "production", "--config-key", "chat"},
			`config "chat" in project "production" has no synced variations in this repository. ` +
				"To start, run ldcli sync prompts add --project production --config-key chat.",
		},
		"selectors and target flags": {
			[]string{"add", "production/support/other", "--project", "production"},
			"pass variations as --project, --config-key, and --key, or as project-key/config-key/variation-key arguments, not both",
		},
		"project that --to does not name": {
			[]string{"attach", "tool", "search", "--project", "production", "--to", "other/support/default"},
			`--to "other/support/default" names project "other", but --project is "production"`,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			root := initRepository(t)
			writeVariation(t, root, variation("Baseline"), true)
			writeManifest(t, root, variation("Baseline"))
			api := &directAPI{}

			_, _, err := runPrompt(t, root, api, append(test.arguments, "--no-input")...)

			require.ErrorContains(t, err, test.want)
			assert.Empty(t, api.requests)
		})
	}
}

func TestPromptAddsVariationsNamedByTargetFlags(t *testing.T) {
	root := initRepository(t)
	api := &directAPI{variation: pointer(variation("Remote"))}

	stdout, _, err := runPrompt(t, root, api, "add", "--project", "production", "--config-key", "support", "--key", "default", "--no-input")

	require.NoError(t, err)
	assert.Contains(t, stdout, "Bootstrapped 1 variation file")
	assertManifestFingerprint(t, root, variation("Remote"))
}

// Sync flags come only from the command line. A user's environment must not
// apply, filter, or resolve a sync.
func TestPromptIgnoresSyncFlagsFromTheEnvironment(t *testing.T) {
	root := initRepository(t)
	writeVariation(t, root, variation("Local"), true)
	writeManifest(t, root, variation("Baseline"))
	api := &directAPI{variation: pointer(variation("Baseline"))}
	t.Setenv("LD_YES", "true")
	t.Setenv("LD_CONFLICT", "bogus")
	t.Setenv("LD_PROJECT", "demo")
	t.Setenv("LD_KEY", "default")
	t.Setenv("LD_NO_INPUT", "true")

	_, _, err := runPrompt(t, root, api)
	require.ErrorContains(t, err, "interactive confirmation requires a terminal")
	requireOnlyReads(t, api.requests)

	_, _, err = runPrompt(t, root, api, "add")
	require.ErrorContains(t, err, "interactive prompt selection requires a terminal")
}

func TestPromptReadsTheAccessTokenFromTheEnvironment(t *testing.T) {
	root := initRepository(t)
	writeVariation(t, root, variation("Baseline"), true)
	writeManifest(t, root, variation("Baseline"))
	api := &directAPI{variation: pointer(variation("Baseline")), manifest: manifestsByRoot[root]}
	t.Chdir(root)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("LD_ACCESS_TOKEN", "token")

	stdout, _, err := cmd.CallCmdCapturingStderr(t, cmd.APIClients{ResourcesClient: api}, analytics.NoopClientFn{}.Tracker(),
		[]string{"sync", "prompts", "--base-uri", "https://example.test", "--dry-run", "--output", "plaintext"})

	require.NoError(t, err)
	assert.Equal(t, "No changes. 1 in sync.\n", string(stdout))
}

// A filtered sync does not see the variations outside the filter. A missing
// file of such a variation must not make its tool look unused, because a full
// sync restores the file.
func TestPromptFilterKeepsToolsOfVariationsOutsideTheFilter(t *testing.T) {
	tests := map[string]struct {
		local string
	}{
		"with a change":    {local: "Local"},
		"without a change": {local: "Baseline"},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			root := initRepository(t)
			baseline := variation("Baseline")
			writeVariation(t, root, variation(test.local), true)
			writeManifest(t, root, baseline)
			toolID := addOtherProjectVariationWithTool(t, root)
			toolPath := filepath.Join(root, syncdomain.RootDir, "other", "tools", "search.json")
			api := &directAPI{variation: pointer(baseline)}

			_, _, err := runPrompt(t, root, api, "--project", "production", "--yes")

			require.NoError(t, err)
			assert.Equal(t, test.local, api.variation.Name)
			assert.FileExists(t, toolPath)
			assert.True(t, slices.ContainsFunc(readLock(t, root).Resources, func(resource syncmanifest.Resource) bool {
				return resource.ID() == toolID
			}), "the sync.lock file must keep the tool of the other project")
			for _, request := range api.requests {
				assert.NotContains(t, request, "/projects/other/")
			}
		})
	}
}

// addOtherProjectVariationWithTool tracks a variation that uses the tool
// "search" in a second project. The tool file exists, but the variation file
// is missing, as after a branch switch. It returns the ID of the tool.
func addOtherProjectVariationWithTool(t *testing.T, root string) syncdomain.ResourceID {
	t.Helper()
	store := synclocal.NewStore(root)
	value := variation("Other")
	value.Tools = []syncdomain.AttachmentRef{{Key: "search", Version: 1}}
	value.Attachments = []syncdomain.Attachment{toolAttachment(syncdomain.Tool{Key: "search", Schema: map[string]any{"type": "object"}}, 1)}
	_, err := store.Add([]synclocal.VariationFile{{ProjectKey: "other", ConfigKey: "chat", Upsert: true, Variation: value}})
	require.NoError(t, err)

	lock := readLock(t, root)
	require.NoError(t, lock.SetAttachments("other", value.Attachments))
	id := syncdomain.VariationID("other", "chat", "default")
	fingerprint, err := syncdomain.FingerprintVariation(id.ProjectKey, id.LookupKey, value)
	require.NoError(t, err)
	lock.SetFingerprint(id, fingerprint)
	require.NoError(t, syncmanifest.WriteLock(store, lock))

	_, err = store.DeleteVariations([]synclocal.VariationDeletion{{ProjectKey: "other", ConfigKey: "chat", VariationKey: "default"}})
	require.NoError(t, err)
	require.FileExists(t, filepath.Join(root, syncdomain.RootDir, "other", "tools", "search.json"))
	return value.Attachments[0].ID("other")
}

// addOtherProjectVariation syncs a variation in a second project and returns
// its sync.lock entry.
func addOtherProjectVariation(t *testing.T, root string) syncmanifest.Resource {
	t.Helper()
	store := synclocal.NewStore(root)
	value := variation("Other")
	_, err := store.Add([]synclocal.VariationFile{{ProjectKey: "other", ConfigKey: "chat", Upsert: true, Variation: value}})
	require.NoError(t, err)
	id := syncdomain.VariationID("other", "chat", "default")
	fingerprint, err := syncdomain.FingerprintVariation(id.ProjectKey, id.LookupKey, value)
	require.NoError(t, err)

	lock := readLock(t, root)
	lock.SetFingerprint(id, fingerprint)
	require.NoError(t, syncmanifest.WriteLock(store, lock))
	return syncmanifest.Resource{ResourceKind: id.Kind, ProjectKey: id.ProjectKey, LookupKey: id.LookupKey, Fingerprint: fingerprint}
}

func readLock(t *testing.T, root string) syncmanifest.Manifest {
	t.Helper()
	lock, err := syncmanifest.ReadLock(synclocal.NewStore(root))
	require.NoError(t, err)
	return lock
}

func TestPromptPlansToolAndSkillRowsByScope(t *testing.T) {
	root, api := writeStandaloneAttachmentWorkspace(t)
	tools := []string{"tool lookup update_manifest", "tool refund create_server", "tool search in_sync"}
	skills := []string{"skill summarize in_sync", "skill triage update_manifest"}
	variations := []string{"variation support/default in_sync"}

	for scope, want := range map[syncdomain.Scope][]string{
		syncdomain.ScopePrompts: variations,
		syncdomain.ScopeTools:   tools,
		syncdomain.ScopeSkills:  skills,
		syncdomain.ScopeAll:     slices.Concat(skills, tools, variations),
	} {
		t.Run(string(scope), func(t *testing.T) {
			api.requests = nil

			stdout, err := runSyncAction(t, root, api, syncprompt.SyncAction{DryRun: true, Scope: scope}, false)

			require.NoError(t, err)
			assert.Equal(t, want, planRows(t, stdout))
			requireOnlyReads(t, api.requests)
			if scope == syncdomain.ScopeAll {
				assert.Equal(t, 1, countRequests(api.requests, "GET", "/ai-tools/search"),
					"the tool row and the variation share one read")
			}
		})
	}
}

func TestPromptFiltersToolRowsBeforeAnyRead(t *testing.T) {
	root, api := writeStandaloneAttachmentWorkspace(t)
	filter := syncdomain.Filter{Project: "production", Keys: []string{"lookup"}}

	stdout, err := runSyncAction(t, root, api, syncprompt.SyncAction{DryRun: true, Scope: syncdomain.ScopeTools, Filter: filter}, false)

	require.NoError(t, err)
	assert.Equal(t, []string{"tool lookup update_manifest"}, planRows(t, stdout))
	assert.Equal(t, []string{
		"GET https://example.test/api/v2/projects/production/configs/sync/manifests",
		"GET https://example.test/api/v2/projects/production/ai-tools/lookup",
	}, api.requests)

	for _, test := range []struct {
		filter syncdomain.Filter
		want   string
	}{
		{
			syncdomain.Filter{Project: "production", Keys: []string{"serch"}},
			"tool production/serch is not synced in this repository. " +
				"To start syncing it, run ldcli sync tools add --project production --key serch.",
		},
		{
			syncdomain.Filter{Project: "demo"},
			`project "demo" has no synced resources in this repository. To start, run ldcli sync tools add --project demo.`,
		},
	} {
		api.requests = nil
		_, err := runSyncAction(t, root, api, syncprompt.SyncAction{DryRun: true, Scope: syncdomain.ScopeTools, Filter: test.filter}, false)
		require.EqualError(t, err, test.want)
		assert.Empty(t, api.requests)
	}
}

func TestPromptSyncsStandaloneToolsAndSkills(t *testing.T) {
	root, api := writeStandaloneAttachmentWorkspace(t)
	writeLocalFile(t, root, "tools/search.json", `{"formatVersion": 1, "key": "search", "description": "Search", "schema": {"type": "object"}}`)
	all := syncprompt.SyncAction{Scope: syncdomain.ScopeAll}

	// One plan pushes the tool once, creates the new tool, records the rest,
	// and then pins the new tool version in the variation.
	stdout, err := runSyncAction(t, root, api, all, true)
	require.NoError(t, err)
	assert.Contains(t, stdout, `"lookupKey": "search",
      "action": "update_server",
      "status": "succeeded",
      "version": 2`)
	assert.Equal(t, 1, countRequests(api.requests, "PATCH", "/ai-tools/search"))
	assert.Equal(t, 1, countRequests(api.requests, "POST", "/ai-tools"))
	assert.Equal(t, 1, countRequests(api.requests, "PATCH", "/configs/sync/manifests"))
	assert.Less(t, slices.Index(api.requests, "PATCH https://example.test/api/v2/projects/production/ai-tools/search"),
		slices.Index(api.requests, "PATCH https://example.test/api/v2/projects/production/ai-configs/support/variations/default"))
	assert.Equal(t, 1, api.tools["refund"].Version)
	assert.Equal(t, []syncdomain.AttachmentRef{{Key: "search", Version: 2}}, api.variation.Tools)
	assert.Len(t, readLock(t, root).Resources, 6, "the lock keeps every tool and skill, also the ones that no variation uses")

	api.requests = nil
	stdout, err = runSyncAction(t, root, api, syncprompt.SyncAction{DryRun: true, Scope: syncdomain.ScopeAll}, false)
	require.NoError(t, err)
	for _, row := range planRows(t, stdout) {
		assert.True(t, strings.HasSuffix(row, " in_sync"), row)
	}

	// A change in LaunchDarkly pulls into the standalone file.
	lookup := api.tools["lookup"]
	lookup.Description = pointer("Look up an order")
	lookup.Version++
	api.tools["lookup"] = lookup
	_, err = runSyncAction(t, root, api, syncprompt.SyncAction{Scope: syncdomain.ScopeTools}, true)
	require.NoError(t, err)
	content, err := os.ReadFile(filepath.Join(root, syncdomain.RootDir, "production", "tools", "lookup.json"))
	require.NoError(t, err)
	assert.Contains(t, string(content), "Look up an order")

	// A hand-written skill file with upsert creates the skill.
	writeLocalFile(t, root, "skills/handoff.md", "---\nkey: handoff\ndescription: Hand off\nname: Human handoff\nupsert: true\n---\n\n# Handoff\n")
	_, err = runSyncAction(t, root, api, syncprompt.SyncAction{Scope: syncdomain.ScopeSkills}, true)
	require.NoError(t, err)
	assert.Equal(t, "Human handoff", api.skills["handoff"].Name)

	// LaunchDarkly owns the skill name after the create. A rename there is not
	// a change, and a push of a local edit does not send the local name.
	handoff := api.skills["handoff"]
	handoff.Name = "Renamed in LaunchDarkly"
	api.skills["handoff"] = handoff
	stdout, err = runSyncAction(t, root, api, syncprompt.SyncAction{DryRun: true, Scope: syncdomain.ScopeSkills}, false)
	require.NoError(t, err)
	assert.Contains(t, planRows(t, stdout), "skill handoff in_sync", "a rename in LaunchDarkly is not a change")
	writeLocalFile(t, root, "skills/handoff.md",
		"---\nkey: handoff\ndescription: Hand off to a person\nname: Human handoff\nupsert: true\n---\n\n# Handoff\n")
	api.requests = nil
	_, err = runSyncAction(t, root, api, syncprompt.SyncAction{Scope: syncdomain.ScopeSkills}, true)
	require.NoError(t, err)
	assert.Equal(t, 1, countRequests(api.requests, "PATCH", "/ai-configs/skills/handoff"))
	assert.Equal(t, "Hand off to a person", api.skills["handoff"].Description)
	assert.Equal(t, "Renamed in LaunchDarkly", api.skills["handoff"].Name, "the push must not send the local name")
	content, err = os.ReadFile(filepath.Join(root, syncdomain.RootDir, "production", "skills", "handoff.md"))
	require.NoError(t, err)
	assert.Contains(t, string(content), "name: Human handoff\n", "the file keeps its local name")
	assert.Contains(t, string(content), "upsert: true\n")

	// A deletion in LaunchDarkly deletes the standalone file and its baseline.
	delete(api.tools, "lookup")
	_, err = runSyncAction(t, root, api, syncprompt.SyncAction{Scope: syncdomain.ScopeTools}, true)
	require.NoError(t, err)
	_, err = os.Stat(filepath.Join(root, syncdomain.RootDir, "production", "tools", "lookup.json"))
	assert.ErrorIs(t, err, os.ErrNotExist)
	assert.NotContains(t, string(must(os.ReadFile(filepath.Join(root, syncdomain.RootDir, "sync.lock")))), "key: lookup")
}

func must[T any](value T, err error) T {
	if err != nil {
		panic(err)
	}
	return value
}

// writeStandaloneAttachmentWorkspace syncs a variation that uses the tool
// "search" and the skill "summarize". It also writes three tool and skill
// files that no variation uses: "lookup" and "triage", which LaunchDarkly
// has, and "refund", which LaunchDarkly does not have and which has upsert.
func writeStandaloneAttachmentWorkspace(t *testing.T) (string, *directAPI) {
	t.Helper()
	root := initRepository(t)
	search := syncdomain.Tool{Key: "search", Schema: map[string]any{"type": "object"}}
	summarize := syncdomain.Skill{Key: "summarize", Description: "Summarize a ticket", Markdown: "Write three bullet points.\n"}
	used := variation("Baseline")
	used.Tools = []syncdomain.AttachmentRef{{Key: "search"}}
	used.Skills = []syncdomain.AttachmentRef{{Key: "summarize"}}
	used.Attachments = []syncdomain.Attachment{
		toolAttachment(search, 0), {Kind: syncdomain.AttachmentSkill, Skill: &summarize},
	}
	writeVariation(t, root, used, true)
	writeManifest(t, root, used)

	lookup := syncdomain.Tool{Key: "lookup", Schema: map[string]any{"type": "object"}}
	triage := syncdomain.Skill{Key: "triage", Name: "Triage", Description: "Triage a ticket", Markdown: "Pick a queue.\n"}
	writeLocalFile(t, root, "tools/lookup.json", `{"formatVersion": 1, "key": "lookup", "schema": {"type": "object"}}`)
	writeLocalFile(t, root, "tools/refund.json", `{"formatVersion": 1, "upsert": true, "key": "refund", "schema": {"type": "object"}}`)
	writeLocalFile(t, root, "skills/triage.md", "---\nkey: triage\ndescription: Triage a ticket\n---\n\nPick a queue.\n")

	server := used
	server.Tools = []syncdomain.AttachmentRef{{Key: "search", Version: 1}}
	server.Skills = []syncdomain.AttachmentRef{{Key: "summarize", Version: 1}}
	server.Attachments = nil
	summarizeInLaunchDarkly := summarize
	summarizeInLaunchDarkly.Name = "Summarize"
	return root, &directAPI{
		variation: &server,
		tools:     map[string]versionedTool{"search": {Tool: search, Version: 1}, "lookup": {Tool: lookup, Version: 2}},
		skills: map[string]versionedSkill{
			"summarize": {Skill: summarizeInLaunchDarkly, Version: 1}, "triage": {Skill: triage, Version: 3},
		},
	}
}

// runSyncAction runs one sync through Runner.Run, because no command selects
// a scope yet. It returns the JSON output.
func runSyncAction(t *testing.T, root string, api *directAPI, action syncprompt.SyncAction, yes bool) (string, error) {
	t.Helper()
	api.manifest = manifestsByRoot[root]
	var output bytes.Buffer
	err := syncprompt.NewRunner(api).Run(syncprompt.Options{
		WorkingDirectory: root, AccessToken: "token", BaseURI: "https://example.test", OutputKind: "json",
		Action: action, Yes: yes, Input: strings.NewReader(""), Output: &output, ErrorOutput: io.Discard,
	})
	return output.String(), err
}

// planRows returns "kind lookup-key action" for each row of a JSON plan.
func planRows(t *testing.T, plan string) []string {
	t.Helper()
	var output struct {
		Resources []struct {
			ResourceKind string `json:"resourceKind"`
			LookupKey    string `json:"lookupKey"`
			Action       string `json:"action"`
		} `json:"resources"`
	}
	require.NoError(t, json.Unmarshal([]byte(plan), &output))
	rows := make([]string, 0, len(output.Resources))
	for _, resource := range output.Resources {
		rows = append(rows, resource.ResourceKind+" "+resource.LookupKey+" "+resource.Action)
	}
	return rows
}

func countRequests(requests []string, method, pathSuffix string) int {
	count := 0
	for _, request := range requests {
		if strings.HasPrefix(request, method+" ") && strings.HasSuffix(request, pathSuffix) {
			count++
		}
	}
	return count
}

func writeLocalFile(t *testing.T, root, projectPath, content string) {
	t.Helper()
	path := filepath.Join(root, syncdomain.RootDir, "production", filepath.FromSlash(projectPath))
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
}

func TestPromptRestoresTrackedMissingFileInsteadOfArchiving(t *testing.T) {
	root := initRepository(t)
	baseline := variation("Baseline")
	writeVariation(t, root, baseline, false)
	api := &directAPI{variation: pointer(baseline)}

	// The first sync adopts the file that matches LaunchDarkly and writes the lock.
	_, _, err := runPrompt(t, root, api, "--yes")
	require.NoError(t, err)
	_, err = os.Stat(filepath.Join(root, syncdomain.RootDir, "sync.lock"))
	require.NoError(t, err)

	_, err = synclocal.NewStore(root).DeleteVariations([]synclocal.VariationDeletion{{
		ProjectKey: "production", ConfigKey: "support", VariationKey: "default",
	}})
	require.NoError(t, err)

	_, _, err = runPrompt(t, root, api, "--yes")

	require.NoError(t, err)
	assert.NotEqual(t, "archived", api.variationState)
	resources, err := synclocal.CompileWorkspace(root)
	require.NoError(t, err)
	require.Len(t, resources, 1)
	assert.Equal(t, baseline.Name, resources[0].Variation.Name)
	assert.NotEmpty(t, manifestsByRoot[root].Items)
}

// The remote manifest is shared by every branch. A working copy without a
// sync.lock file must not restore, archive, or record a variation that only
// another branch syncs.
func TestPromptWithoutSyncLockIgnoresVariationsOfOtherBranches(t *testing.T) {
	root := initRepository(t)
	baseline := variation("Baseline")
	writeVariation(t, root, baseline, false)
	writeRemoteManifest(t, root, baseline)
	remote := manifestsByRoot[root]
	remote.Items = append(remote.Items, syncapi.SyncManifestResource{
		ResourceKind:      syncdomain.KindVariation,
		ResourceLookupKey: "billing/default",
		Fingerprint:       remote.Items[0].Fingerprint,
		Version:           1,
	})
	api := &directAPI{variation: pointer(baseline)}

	_, _, err := runPrompt(t, root, api, "--yes")

	require.NoError(t, err)
	assert.False(t, slices.ContainsFunc(api.requests, func(request string) bool {
		return strings.Contains(request, "/billing")
	}), "sync read a variation that this working copy never synced")
	_, statErr := os.Stat(filepath.Join(root, syncdomain.RootDir, "production", "configs", "billing"))
	assert.ErrorIs(t, statErr, os.ErrNotExist)
	assert.Len(t, manifestsByRoot[root].Items, 2)
	lock, err := os.ReadFile(filepath.Join(root, syncdomain.RootDir, "sync.lock"))
	require.NoError(t, err)
	assert.Contains(t, string(lock), "key: support/default")
	assert.NotContains(t, string(lock), "billing")
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
		"sync", "prompts",
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

// writeManifestResources records values as the baseline of a working copy
// that synced them. It writes the sync.lock file and the remote manifest.
func writeManifestResources(t *testing.T, root string, values ...syncdomain.Variation) {
	t.Helper()
	manifest := writeRemoteManifest(t, root, values...)
	require.NoError(t, os.MkdirAll(filepath.Join(root, syncdomain.RootDir), 0o755))
	require.NoError(t, syncmanifest.WriteLock(synclocal.NewStore(root), manifest))
}

// writeRemoteManifest writes only the remote manifest, as another working
// copy does when it syncs.
func writeRemoteManifest(t *testing.T, root string, values ...syncdomain.Variation) syncmanifest.Manifest {
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
	return manifest
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

	persisted := resources[0].Variation
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
