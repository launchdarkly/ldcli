package prompt_test

import (
	"bytes"
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
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
)

type directAPI struct {
	variation      *syncdomain.Variation
	variationState string
	// configMode is the mode of the config "support". The empty mode is agent.
	configMode   syncdomain.VariationMode
	modelConfigs []syncapi.ModelConfig
	tools        map[string]versionedTool
	skills       map[string]versionedSkill
	manifest     *syncapi.SyncManifest
	directions   map[string]syncdomain.SyncDirection
	requests     []string
	// expandTools makes each read of the variation add the content of its
	// tools to model.parameters.tools, as LaunchDarkly does.
	expandTools bool
	// createErr fails each create of a tool, a skill, or a variation after
	// onCreate runs.
	// onCreate can add the resource, as another working copy does.
	createErr error
	onCreate  func()
	// onRequest runs before each request, so that it can change LaunchDarkly
	// between two reads, as another working copy does.
	onRequest func(method, path string)
	// manifestWriteErr fails each write of the remote manifest.
	manifestWriteErr error
	// toolWriteErr fails each update of a tool.
	toolWriteErr error
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
	if api.onRequest != nil {
		api.onRequest(method, path)
	}
	if strings.HasSuffix(path, "/configs/sync/manifests") {
		if api.manifestWriteErr != nil && method != http.MethodGet {
			return nil, api.manifestWriteErr
		}
		return handleManifestRequest(api.manifest, method, query, body)
	}
	if strings.HasSuffix(path, "/configs/sync/settings") && method == http.MethodGet {
		items := make([]map[string]any, 0, len(query["resources"]))
		for _, resource := range query["resources"] {
			kind, lookupKey, _ := strings.Cut(resource, ":")
			direction := syncdomain.SyncDirectionBoth
			if configured := api.directions[resource]; configured != "" {
				direction = configured
			}
			items = append(items, map[string]any{
				"resourceKind": kind, "resourceLookupKey": lookupKey, "direction": direction,
			})
		}
		return json.Marshal(map[string]any{"items": items})
	}
	isCreate := method == http.MethodPost &&
		(strings.HasSuffix(path, "/ai-tools") || strings.HasSuffix(path, "/ai-configs/skills") || strings.HasSuffix(path, "/variations"))
	if isCreate && api.createErr != nil {
		if api.onCreate != nil {
			api.onCreate()
		}
		return nil, api.createErr
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
			if api.toolWriteErr != nil {
				return nil, api.toolWriteErr
			}
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
	if method == "GET" && strings.HasSuffix(path, "/model-configs") {
		return json.Marshal(api.modelConfigs)
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
			"key": "support", "name": "Support", "mode": cmp.Or(api.configMode, syncdomain.VariationModeAgent), "variations": variations,
		})
	case "POST":
		var variation syncdomain.Variation
		if err := json.Unmarshal(body, &variation); err != nil {
			return nil, err
		}
		variation.Mode = cmp.Or(api.configMode, syncdomain.VariationModeAgent)
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
			Messages           *[]syncdomain.Message       `json:"messages"`
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
		mode := cmp.Or(api.configMode, syncdomain.VariationModeAgent)
		api.variation = &syncdomain.Variation{
			Mode: mode, Key: key, Name: update.Name,
			Instructions: update.Instructions, ModelConfigKey: update.ModelConfigKey,
			ModelConfigVersion: update.ModelConfigVersion, Model: update.Model,
			OutputFormat: update.OutputFormat, Tools: tools, Skills: skills,
		}
		if update.Messages != nil {
			api.variation.Messages = *update.Messages
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

	fix := `To create it, add "upsert": true to its file and run ldcli sync tools --project production --key my-second-tool.`
	require.ErrorContains(t, err, fix)
	assert.Contains(t, output, fix)
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

// A tool file that no variation uses syncs on its own, so a sync keeps the
// file and both of its rows.
func TestPromptKeepsUnreferencedLocalToolWithYes(t *testing.T) {
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
	require.NoError(t, err, "the tool file stays")
	searchFingerprint, err := syncdomain.FingerprintAttachment("production", toolAttachment(tool, 2))
	require.NoError(t, err)
	assert.Equal(t, []syncmanifest.Resource{
		{ResourceKind: syncdomain.KindTool, ProjectKey: "production", LookupKey: "search", Fingerprint: searchFingerprint},
		{ResourceKind: syncdomain.KindVariation, ProjectKey: "production", LookupKey: "support/default", Fingerprint: fingerprint(t, detached)},
	}, readLock(t, root).Resources)
	assert.Contains(t, manifestsByRoot[root].Items, syncapi.SyncManifestResource{
		ResourceKind: syncdomain.KindTool, ResourceLookupKey: "search", Fingerprint: searchFingerprint, Version: 1,
	})
}

// A detach of a variation keeps the tools that it used, with their files
// and rows. Without --yes and without a terminal, detach changes nothing.
func TestPromptDetachKeepsToolsOfTheVariation(t *testing.T) {
	root := initRepository(t)
	tool := syncdomain.Tool{Key: "search", Schema: map[string]any{"type": "object"}}
	baseline := variation("Baseline")
	baseline.Tools = []syncdomain.AttachmentRef{{Key: "search", Version: 1}}
	baseline.Attachments = []syncdomain.Attachment{toolAttachment(tool, 1)}
	writeVariation(t, root, baseline, true)
	writeManifest(t, root, baseline)
	api := &directAPI{variation: pointer(baseline), tools: map[string]versionedTool{"search": {Tool: tool, Version: 1}}}
	detach := []string{"detach", "--project", "production", "--config-key", "support", "--key", "default", "--no-input"}
	toolFile := filepath.Join(root, syncdomain.RootDir, "production", "tools", "search.json")

	_, _, err := runPrompt(t, root, api, detach...)
	require.ErrorContains(t, err, "rerun with --yes")
	assert.Len(t, readLock(t, root).Resources, 2, "nothing changes")

	stdout, _, err := runPrompt(t, root, api, append(detach, "--yes")...)
	require.NoError(t, err)
	assert.Equal(t, "Detached resources:\n- variation production/support/default\n", stdout)
	_, err = os.Stat(toolFile)
	require.NoError(t, err, "the tool file stays")
	assert.Equal(t, []syncdomain.ResourceID{{Kind: syncdomain.KindTool, ProjectKey: "production", LookupKey: "search"}},
		lockResourceIDs(readLock(t, root)))
	assert.Len(t, manifestsByRoot[root].Items, 1)
}

func lockResourceIDs(lock syncmanifest.Manifest) []syncdomain.ResourceID {
	ids := make([]syncdomain.ResourceID, 0, len(lock.Resources))
	for _, resource := range lock.Resources {
		ids = append(ids, resource.ID())
	}
	return ids
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

			stdout, err := runSyncAction(t, root, api, scope, syncprompt.SyncAction{DryRun: true}, false)

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

	stdout, err := runSyncAction(t, root, api, syncdomain.ScopeTools, syncprompt.SyncAction{DryRun: true, Filter: filter}, false)

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
		_, err := runSyncAction(t, root, api, syncdomain.ScopeTools, syncprompt.SyncAction{DryRun: true, Filter: test.filter}, false)
		require.EqualError(t, err, test.want)
		assert.Empty(t, api.requests)
	}
}

func TestPromptSyncsStandaloneToolsAndSkills(t *testing.T) {
	root, api := writeStandaloneAttachmentWorkspace(t)
	writeLocalFile(t, root, "tools/search.json", `{"formatVersion": 1, "key": "search", "description": "Search", "schema": {"type": "object"}}`)
	all := syncprompt.SyncAction{}

	// One plan pushes the tool once, creates the new tool, records the rest,
	// and then pins the new tool version in the variation.
	stdout, err := runSyncAction(t, root, api, syncdomain.ScopeAll, all, true)
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
	stdout, err = runSyncAction(t, root, api, syncdomain.ScopeAll, syncprompt.SyncAction{DryRun: true}, false)
	require.NoError(t, err)
	for _, row := range planRows(t, stdout) {
		assert.True(t, strings.HasSuffix(row, " in_sync"), row)
	}

	// A change in LaunchDarkly pulls into the standalone file.
	lookup := api.tools["lookup"]
	lookup.Description = pointer("Look up an order")
	lookup.Version++
	api.tools["lookup"] = lookup
	_, err = runSyncAction(t, root, api, syncdomain.ScopeTools, syncprompt.SyncAction{}, true)
	require.NoError(t, err)
	content, err := os.ReadFile(filepath.Join(root, syncdomain.RootDir, "production", "tools", "lookup.json"))
	require.NoError(t, err)
	assert.Contains(t, string(content), "Look up an order")

	// A hand-written skill file with upsert creates the skill.
	writeLocalFile(t, root, "skills/handoff.md", "---\nkey: handoff\ndescription: Hand off\nname: Human handoff\nupsert: true\n---\n\n# Handoff\n")
	_, err = runSyncAction(t, root, api, syncdomain.ScopeSkills, syncprompt.SyncAction{}, true)
	require.NoError(t, err)
	assert.Equal(t, "Human handoff", api.skills["handoff"].Name)

	// LaunchDarkly owns the skill name after the create. A rename there is not
	// a change, and a push of a local edit does not send the local name.
	handoff := api.skills["handoff"]
	handoff.Name = "Renamed in LaunchDarkly"
	api.skills["handoff"] = handoff
	stdout, err = runSyncAction(t, root, api, syncdomain.ScopeSkills, syncprompt.SyncAction{DryRun: true}, false)
	require.NoError(t, err)
	assert.Contains(t, planRows(t, stdout), "skill handoff in_sync", "a rename in LaunchDarkly is not a change")
	writeLocalFile(t, root, "skills/handoff.md",
		"---\nkey: handoff\ndescription: Hand off to a person\nname: Human handoff\nupsert: true\n---\n\n# Handoff\n")
	api.requests = nil
	_, err = runSyncAction(t, root, api, syncdomain.ScopeSkills, syncprompt.SyncAction{}, true)
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
	_, err = runSyncAction(t, root, api, syncdomain.ScopeTools, syncprompt.SyncAction{}, true)
	require.NoError(t, err)
	_, err = os.Stat(filepath.Join(root, syncdomain.RootDir, "production", "tools", "lookup.json"))
	assert.ErrorIs(t, err, os.ErrNotExist)
	assert.NotContains(t, string(must(os.ReadFile(filepath.Join(root, syncdomain.RootDir, "sync.lock")))), "key: lookup")
}

// A workspace with only a tool file, or only a skill file, syncs with its own
// scope and with the scope of every kind. A sync of prompts leaves it out and
// says so.
func TestScopesSyncAWorkspaceWithOnlyOneTool(t *testing.T) {
	for _, test := range []struct {
		scope syncdomain.Scope
		file  string
		row   string
		note  string
		api   *directAPI
	}{
		{
			scope: syncdomain.ScopeTools, file: "tools/lookup.json", row: "tool lookup",
			note: "Note: 1 standalone tool is not in this plan. Run ldcli sync all to include them.\n",
			api: &directAPI{tools: map[string]versionedTool{
				"lookup": {Tool: syncdomain.Tool{Key: "lookup", Schema: map[string]any{"type": "object"}}, Version: 2},
			}},
		},
		{
			scope: syncdomain.ScopeSkills, file: "skills/triage.md", row: "skill triage",
			note: "Note: 1 standalone skill is not in this plan. Run ldcli sync all to include them.\n",
			api: &directAPI{skills: map[string]versionedSkill{
				"triage": {Skill: syncdomain.Skill{Key: "triage", Description: "Triage", Markdown: "Pick a queue.\n"}, Version: 3},
			}},
		},
	} {
		t.Run(string(test.scope), func(t *testing.T) {
			root := initRepository(t)
			writeLocalFile(t, root, "tools/lookup.json", `{"formatVersion": 1, "key": "lookup", "schema": {"type": "object"}}`)
			writeLocalFile(t, root, "skills/triage.md", "---\nkey: triage\ndescription: Triage\n---\n\nPick a queue.\n")
			require.NoError(t, os.Remove(filepath.Join(root, syncdomain.RootDir, "production", otherFile(test.file))))

			stdout, err := runSyncAction(t, root, test.api, test.scope, syncprompt.SyncAction{}, true)
			require.NoError(t, err)
			assert.Contains(t, stdout, `"action": "update_manifest",
      "status": "succeeded"`)

			for _, scope := range []syncdomain.Scope{test.scope, syncdomain.ScopeAll} {
				stdout, err = runSyncAction(t, root, test.api, scope, syncprompt.SyncAction{DryRun: true}, false)
				require.NoError(t, err)
				assert.Equal(t, []string{test.row + " in_sync"}, planRows(t, stdout), "scope %s", scope)
			}

			stdout, notes, err := runScoped(t, root, test.api, syncprompt.Options{Action: syncprompt.SyncAction{DryRun: true}})
			require.NoError(t, err)
			assert.Empty(t, planRows(t, stdout))
			assert.Equal(t, test.note, notes)
		})
	}
}

// otherFile returns the standalone file of the other kind in
// TestScopesSyncAWorkspaceWithOnlyOneTool.
func otherFile(file string) string {
	if file == "tools/lookup.json" {
		return "skills/triage.md"
	}
	return "tools/lookup.json"
}

// The count of standalone files reads only their paths, so an invalid file
// does not break a sync of prompts.
func TestSyncPromptsCountsStandaloneFilesWithoutReadingThem(t *testing.T) {
	root, api := writeStandaloneAttachmentWorkspace(t)
	writeLocalFile(t, root, "tools/broken.json", "not JSON")

	_, notes, err := runScoped(t, root, api, syncprompt.Options{Action: syncprompt.SyncAction{DryRun: true}})

	require.NoError(t, err)
	assert.Equal(t, "Note: 1 standalone skill and 3 standalone tools are not in this plan. Run ldcli sync all to include them.\n", notes)
}

// A linked tool or skill has a descriptor instead of a managed file, and the
// count of standalone files includes it.
func TestSyncPromptsCountsLinkedStandaloneDescriptors(t *testing.T) {
	root, api := writeStandaloneAttachmentWorkspace(t)
	writeLocalFile(t, root, "../../tools/audit.json", `{"description": "Audit an order", "inputSchema": {"type": "object"}}`)
	_, err := synclocal.NewStore(root).WriteDescriptor(
		syncdomain.ResourceID{Kind: syncdomain.KindTool, ProjectKey: "production", LookupKey: "audit"},
		syncdomain.Reference{
			File: "tools/audit.json", Format: "json", Select: "$",
			Map: map[string]string{"description": "description", "schema": "inputSchema"},
		},
	)
	require.NoError(t, err)

	_, notes, err := runScoped(t, root, api, syncprompt.Options{Action: syncprompt.SyncAction{DryRun: true}})

	require.NoError(t, err)
	assert.Equal(t, "Note: 1 standalone skill and 3 standalone tools are not in this plan. Run ldcli sync all to include them.\n", notes)
}

// --config-key and --key select variations, so a filtered sync of prompts
// counts the standalone files of its project.
func TestSyncPromptsWithAConfigFilterCountsStandaloneFiles(t *testing.T) {
	for _, filter := range []syncdomain.Filter{
		{Project: "production"},
		{Project: "production", ConfigKey: "support"},
		{Project: "production", ConfigKey: "support", Keys: []string{"default"}},
	} {
		root, api := writeStandaloneAttachmentWorkspace(t)

		stdout, notes, err := runScoped(t, root, api, syncprompt.Options{Action: syncprompt.SyncAction{Filter: filter, DryRun: true}})

		require.NoError(t, err)
		assert.Equal(t, []string{"variation support/default in_sync"}, planRows(t, stdout), "filter %+v", filter)
		assert.Equal(t, "Note: 1 standalone skill and 2 standalone tools are not in this plan. Run ldcli sync all to include them.\n",
			notes, "filter %+v", filter)
	}
}

// A tool that a variation pull newly references has a row in the plan, so
// the count of standalone files leaves it out.
func TestSyncPromptsDoesNotCountAToolThatHasARow(t *testing.T) {
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

	stdout, notes, err := runScoped(t, root, api, syncprompt.Options{Action: syncprompt.SyncAction{DryRun: true}})

	require.NoError(t, err)
	assert.Contains(t, planRows(t, stdout), "tool search update_server")
	assert.Empty(t, notes)
}

func TestScopesStartFromARepositoryWithoutSyncFiles(t *testing.T) {
	root := initRepository(t)
	api := &directAPI{tools: map[string]versionedTool{
		"lookup": {Tool: syncdomain.Tool{Key: "lookup", Schema: map[string]any{"type": "object"}}, Version: 2},
	}}

	_, err := runSyncAction(t, root, api, syncdomain.ScopeAll, syncprompt.SyncAction{}, true)
	require.EqualError(t, err, "Nothing to sync in this repository. "+
		"To start, run ldcli sync prompts add, ldcli sync tools add, or ldcli sync skills add.")

	_, err = runSyncAction(t, root, api, syncdomain.ScopeTools, syncprompt.SyncAction{}, true)
	require.EqualError(t, err, "--project and --key are required with --no-input", "a sync of tools starts the add of tools")

	filter := syncdomain.Filter{Project: "production", Keys: []string{"lookup"}}
	stdout, err := runSyncAction(t, root, api, syncdomain.ScopeTools, syncprompt.SyncAction{Filter: filter}, true)
	require.NoError(t, err)
	assert.Equal(t, "Bootstrapped 1 tool file in .launchdarkly.\n", stdout)
	assert.Equal(t, []syncdomain.ResourceID{{Kind: syncdomain.KindTool, ProjectKey: "production", LookupKey: "lookup"}},
		lockResourceIDs(readLock(t, root)))
}

func TestScopesAddAndDetachTools(t *testing.T) {
	root, api := writeStandaloneAttachmentWorkspace(t)
	require.NoError(t, os.Remove(filepath.Join(root, syncdomain.RootDir, "production", "tools", "lookup.json")))
	lookup := syncdomain.Filter{Project: "production", Keys: []string{"lookup"}}
	lookupFile := filepath.Join(root, syncdomain.RootDir, "production", "tools", "lookup.json")

	stdout, _, err := runScoped(t, root, api, syncprompt.Options{
		Scope: syncdomain.ScopeTools, Action: syncprompt.AddAction{Filter: lookup, DryRun: true},
	})
	require.NoError(t, err)
	assert.Equal(t, "1 file to create:\n  .launchdarkly/production/tools/lookup.json   +7\n", stdout)
	_, err = os.Stat(lookupFile)
	require.ErrorIs(t, err, os.ErrNotExist)

	_, _, err = runScoped(t, root, api, syncprompt.Options{Scope: syncdomain.ScopeTools, Action: syncprompt.AddAction{Filter: lookup}})
	require.NoError(t, err)
	require.FileExists(t, lookupFile)
	lookupID := syncdomain.ResourceID{Kind: syncdomain.KindTool, ProjectKey: "production", LookupKey: "lookup"}
	assert.Contains(t, lockResourceIDs(readLock(t, root)), lookupID)

	search := syncdomain.Filter{Project: "production", Keys: []string{"search"}}
	_, _, err = runScoped(t, root, api, syncprompt.Options{
		Scope: syncdomain.ScopeTools, Action: syncprompt.DetachAction{Filter: search}, Yes: true,
	})
	require.EqualError(t, err,
		"tool production/search is used by variation production/support/default. Remove it from that variation first.")

	stdout, _, err = runScoped(t, root, api, syncprompt.Options{
		Scope: syncdomain.ScopeTools, Action: syncprompt.DetachAction{Filter: lookup}, Yes: true,
	})
	require.NoError(t, err)
	assert.Equal(t, "Detached resources:\n- tool production/lookup\n", stdout)
	_, err = os.Stat(lookupFile)
	require.ErrorIs(t, err, os.ErrNotExist)
	assert.NotContains(t, lockResourceIDs(readLock(t, root)), lookupID)
	assert.Contains(t, api.tools, "lookup", "LaunchDarkly keeps the tool")
}

// Only variations have positional selectors, so a tools or skills scope
// rejects them before any request, and the error names the kind of the scope.
func TestScopesOfToolsAndSkillsRejectPositionalSelectors(t *testing.T) {
	selector := []syncdomain.ResourceID{syncdomain.VariationID("production", "support", "default")}
	for _, scope := range []syncdomain.Scope{syncdomain.ScopeTools, syncdomain.ScopeSkills} {
		for _, filter := range []syncdomain.Filter{{}, {Project: "production"}} {
			for _, action := range []syncprompt.CommandAction{
				syncprompt.AddAction{Resources: selector, Filter: filter},
				syncprompt.DetachAction{Resources: selector, Filter: filter},
			} {
				root, api := writeStandaloneAttachmentWorkspace(t)
				api.requests = nil

				_, _, err := runScoped(t, root, api, syncprompt.Options{Scope: scope, Action: action, Yes: true})

				require.EqualError(t, err, "only variations have positional selectors. Pass "+string(scope)+" as --project and --key.",
					"%s %T %+v", scope, action, filter)
				assert.Empty(t, api.requests)
			}
		}
	}
}

// Only variations have config keys, so every tools or skills action rejects
// --config-key before any request.
func TestScopesOfToolsAndSkillsRejectConfigKeys(t *testing.T) {
	filter := syncdomain.Filter{Project: "production", ConfigKey: "support"}
	for _, scope := range []syncdomain.Scope{syncdomain.ScopeTools, syncdomain.ScopeSkills} {
		for _, action := range []syncprompt.CommandAction{
			syncprompt.SyncAction{Filter: filter},
			syncprompt.AddAction{Filter: filter},
			syncprompt.DetachAction{Filter: filter},
		} {
			root, api := writeStandaloneAttachmentWorkspace(t)
			api.requests = nil

			_, _, err := runScoped(t, root, api, syncprompt.Options{Scope: scope, Action: action, Yes: true})

			require.EqualError(t, err, "--config-key is only valid for variations", "%s %T", scope, action)
			assert.Empty(t, api.requests)
		}
	}
}

// A tool and the variation that uses it form one conflict in a sync of every
// kind. A sync of only that tool resolves it, and the next sync of every kind
// only repins the variation.
func TestScopesResolveASharedToolConflict(t *testing.T) {
	root, api := writeStandaloneAttachmentWorkspace(t)
	_, err := runSyncAction(t, root, api, syncdomain.ScopeAll, syncprompt.SyncAction{}, true)
	require.NoError(t, err)
	writeLocalFile(t, root, "tools/search.json", `{"formatVersion": 1, "key": "search", "description": "Local", "schema": {"type": "object"}}`)
	search := api.tools["search"]
	search.Description, search.Version = pointer("LaunchDarkly"), search.Version+1
	api.tools["search"] = search

	_, notes, err := runScoped(t, root, api, syncprompt.Options{Scope: syncdomain.ScopeAll, Action: syncprompt.SyncAction{}, Yes: true})
	require.EqualError(t, err, "conflict resolution requires --conflict without interactive input")
	assert.Contains(t, notes, "Conflict: tool production/search changed on both sides. "+
		"It shares a changed tool or skill with variation production/support/default, so one choice covers them all.")

	api.requests = nil
	_, notes, err = runScoped(t, root, api, syncprompt.Options{
		Scope:          syncdomain.ScopeTools,
		Action:         syncprompt.SyncAction{Filter: syncdomain.Filter{Project: "production", Keys: []string{"search"}}},
		ConflictPolicy: syncprompt.ConflictPolicy{Default: syncprompt.ConflictUseLocal},
		Yes:            true,
	})
	require.NoError(t, err)
	assert.Equal(t, 1, countRequests(api.requests, "PATCH", "/ai-tools/search"))
	assert.Zero(t, countRequests(api.requests, "PATCH", "/variations/default"), "a sync of tools never writes a variation")
	assert.Contains(t, notes, "Note: Prompt production/support/default uses tool search. "+
		"It repins on the next ldcli sync prompts or ldcli sync all.\n")

	stdout, err := runSyncAction(t, root, api, syncdomain.ScopeAll, syncprompt.SyncAction{DryRun: true}, false)
	require.NoError(t, err)
	assert.Contains(t, planRows(t, stdout), "variation support/default update_server")
	assert.Contains(t, planRows(t, stdout), "tool search in_sync")
	assert.Contains(t, stdout, `"attachment versions"`, "the variation only repins the tool")
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
func runSyncAction(
	t *testing.T, root string, api *directAPI, scope syncdomain.Scope, action syncprompt.CommandAction, yes bool,
) (string, error) {
	t.Helper()
	stdout, _, err := runScoped(t, root, api, syncprompt.Options{Scope: scope, Action: action, Yes: yes})
	return stdout, err
}

func TestSourceLinkUsesDirectionAndKeepsSourceInPlace(t *testing.T) {
	root := initRepository(t)
	source := []byte(`from agents import Agent
support = Agent(instructions="Local instructions.")
`)
	require.NoError(t, os.WriteFile(filepath.Join(root, "agents.py"), source, 0o644))
	server := variation("Server instructions.")
	api := &directAPI{
		variation: &server,
		directions: map[string]syncdomain.SyncDirection{
			"variation:support/default": syncdomain.SyncDirectionCodeCanonical,
		},
	}
	id := syncdomain.VariationID("production", "support", "default")

	output, _, err := runScoped(t, root, api, syncprompt.Options{
		Scope: syncdomain.ScopePrompts,
		Action: syncprompt.SourceLinkAction{
			ID: id, File: "agents.py", Format: "python", Select: `agents[id="support"]`,
			Map: map[string]string{"prompt": "instructions"},
		},
		Yes: true,
	})

	require.NoError(t, err)
	assert.Contains(t, output, `"action": "update_server"`)
	require.NotNil(t, api.variation)
	assert.Equal(t, "Local instructions.", api.variation.Instructions)
	descriptor := filepath.Join(root, ".launchdarkly", "production", "configs", "support", "default.ref.yaml")
	require.FileExists(t, descriptor)
	actual, err := os.ReadFile(filepath.Join(root, "agents.py"))
	require.NoError(t, err)
	assert.Equal(t, source, actual)
	lock, err := os.ReadFile(filepath.Join(root, ".launchdarkly", "sync.lock"))
	require.NoError(t, err)
	assert.Contains(t, string(lock), "formatVersion: 1")
	assert.NotContains(t, string(lock), "file: agents.py")

	api.directions["variation:support/default"] = syncdomain.SyncDirectionServerCanonical
	api.variation.Instructions = "Server instructions after link."
	output, _, err = runScoped(t, root, api, syncprompt.Options{
		Scope: syncdomain.ScopePrompts,
		Action: syncprompt.SyncAction{Filter: syncdomain.Filter{
			Project: "production", ConfigKey: "support", Keys: []string{"default"},
		}},
		Yes: true,
	})
	require.NoError(t, err)
	assert.Contains(t, output, `"action": "update_local"`)
	actual, err = os.ReadFile(filepath.Join(root, "agents.py"))
	require.NoError(t, err)
	assert.Equal(t, `from agents import Agent
support = Agent(instructions="Server instructions after link.")
`, string(actual))
}

func TestPromptDescriptorTopLevelToolSyncsEndToEnd(t *testing.T) {
	root := initRepository(t)
	require.NoError(t, os.WriteFile(filepath.Join(root, "prompt.md"), []byte("Local instructions.\n"), 0o644))
	writeLocalFile(t, root, "configs/support/default.ref.yaml",
		"# keep\nformatVersion: 1\nfile: prompt.md\nformat: markdown\n"+
			"tools:\n  - key: search\n")
	writeLocalFile(t, root, "tools/search.json",
		`{"formatVersion":1,"key":"search","schema":{"type":"object"},`+
			`"customParameters":{"timeout":5}}`+"\n")
	server := variation("Server name")
	server.Instructions = "Local instructions."
	api := &directAPI{
		variation: &server,
		tools: map[string]versionedTool{
			"search": {
				Tool: syncdomain.Tool{
					Key: "search", Schema: map[string]any{"type": "object"},
					CustomParameters: map[string]any{"timeout": float64(5)},
				},
				Version: 3,
			},
		},
	}

	_, _, err := runPrompt(t, root, api, "--yes", "--conflict", "local")

	require.NoError(t, err)
	require.NotNil(t, api.variation)
	assert.Equal(t, []syncdomain.AttachmentRef{{Key: "search", Version: 3}}, api.variation.Tools)
	descriptor, err := os.ReadFile(filepath.Join(
		root, ".launchdarkly", "production", "configs", "support", "default.ref.yaml",
	))
	require.NoError(t, err)
	assert.Contains(t, string(descriptor), "# keep\n")
	assert.NotContains(t, string(descriptor), "version:")
}

// A variation that was a managed file has a lock entry with the fingerprint
// of that file. A link of the same variation to a source plans a first sync,
// because that fingerprint says nothing about the source projection.
func TestLinkOverAManagedBaselinePlansAFirstSync(t *testing.T) {
	root := initRepository(t)
	baseline := variation("Baseline")
	writeManifest(t, root, baseline)
	require.NoError(t, os.WriteFile(filepath.Join(root, "prompt.md"), []byte("Help"), 0o644))
	server := baseline
	api := &directAPI{variation: &server}
	id := syncdomain.VariationID("production", "support", "default")

	stdout, _, err := runScoped(t, root, api, syncprompt.Options{
		Scope:  syncdomain.ScopePrompts,
		Action: syncprompt.SourceLinkAction{ID: id, File: "prompt.md", Format: "markdown"},
		Yes:    true,
	})

	require.NoError(t, err, "equal sides are adopted instead of reported as a conflict")
	for _, row := range planRows(t, stdout) {
		assert.NotContains(t, row, "conflict", "the managed-file fingerprint is not a baseline for the source")
	}
	assert.Equal(t, "Help", api.variation.Instructions)
}

// The link note goes to the error output, so the JSON result stays one
// document.
func TestSourceLinkKeepsJSONOutputValid(t *testing.T) {
	root := initRepository(t)
	require.NoError(t, os.WriteFile(filepath.Join(root, "prompt.md"), []byte("Local prompt.\n"), 0o644))
	server := variation("Server")
	server.Instructions = "Local prompt."
	api := &directAPI{variation: &server}
	id := syncdomain.VariationID("production", "support", "default")

	stdout, notes, err := runScoped(t, root, api, syncprompt.Options{
		Scope:  syncdomain.ScopePrompts,
		Action: syncprompt.SourceLinkAction{ID: id, File: "prompt.md", Format: "markdown"},
		Yes:    true,
	})

	require.NoError(t, err)
	assert.True(t, json.Valid([]byte(stdout)), "the standard output is one JSON document: %q", stdout)
	assert.Contains(t, notes, "Linked ")
}

func TestCompletionSourcePullsOneSystemMessage(t *testing.T) {
	root := initRepository(t)
	require.NoError(t, os.WriteFile(filepath.Join(root, "prompt.md"), []byte("Local prompt.\n"), 0o644))
	server := syncdomain.Variation{
		Mode: syncdomain.VariationModeCompletion, Key: "default", Name: "Default",
		Messages: []syncdomain.Message{{Role: syncdomain.RoleSystem, Content: "Server prompt."}},
	}
	api := &directAPI{
		variation: pointer(server), configMode: syncdomain.VariationModeCompletion,
		directions: map[string]syncdomain.SyncDirection{
			"variation:support/default": syncdomain.SyncDirectionServerCanonical,
		},
	}
	id := syncdomain.VariationID("production", "support", "default")

	_, _, err := runScoped(t, root, api, syncprompt.Options{
		Scope: syncdomain.ScopePrompts,
		Action: syncprompt.SourceLinkAction{
			ID: id, File: "prompt.md", Format: "markdown",
		},
		Yes: true,
	})

	require.NoError(t, err)
	content, err := os.ReadFile(filepath.Join(root, "prompt.md"))
	require.NoError(t, err)
	assert.Equal(t, "Server prompt.\n", string(content))
}

func TestCompletionMessageListPullsAndPushesLiteralContent(t *testing.T) {
	root := initRepository(t)
	source := `from agents import Agent
completion = Agent(
    name="Completion",
    messages=[
        {"role": "system", "content": "Local system."},
        {"role": "user", "content": "Local user."},
    ],
    model="gpt-5",
)
`
	require.NoError(t, os.WriteFile(filepath.Join(root, "messages.py"), []byte(source), 0o644))
	server := syncdomain.Variation{
		Mode: syncdomain.VariationModeCompletion, Key: "default", Name: "Default",
		Messages: []syncdomain.Message{
			{Role: syncdomain.RoleSystem, Content: "Server system."},
			{Role: syncdomain.RoleUser, Content: "Server user."},
		},
	}
	api := &directAPI{
		variation: pointer(server), configMode: syncdomain.VariationModeCompletion,
		directions: map[string]syncdomain.SyncDirection{
			"variation:support/default": syncdomain.SyncDirectionServerCanonical,
		},
	}
	id := syncdomain.VariationID("production", "support", "default")
	action := syncprompt.SourceLinkAction{
		ID: id, File: "messages.py", Format: "python", Select: `agents[id="completion"]`,
		Map: map[string]string{"messages": "messages"},
	}

	_, _, err := runScoped(t, root, api, syncprompt.Options{
		Scope: syncdomain.ScopePrompts, Action: action, Yes: true,
	})
	require.NoError(t, err)
	content, err := os.ReadFile(filepath.Join(root, "messages.py"))
	require.NoError(t, err)
	assert.Contains(t, string(content), `"content": "Server system."`)
	assert.Contains(t, string(content), `"content": "Server user."`)

	updated := strings.Replace(string(content), "Server user.", "Repository user.", 1)
	require.NoError(t, os.WriteFile(filepath.Join(root, "messages.py"), []byte(updated), 0o644))
	api.directions["variation:support/default"] = syncdomain.SyncDirectionCodeCanonical
	_, _, err = runScoped(t, root, api, syncprompt.Options{
		Scope: syncdomain.ScopePrompts,
		Action: syncprompt.SyncAction{
			Filter: syncdomain.Filter{Project: "production", ConfigKey: "support", Keys: []string{"default"}},
		},
		Yes: true,
	})
	require.NoError(t, err)
	assert.Equal(t, "Repository user.", api.variation.Messages[1].Content)
}

func TestStandaloneSkillSourceLinkPullsLaunchDarklyContent(t *testing.T) {
	root := initRepository(t)
	sourcePath := filepath.Join(root, ".agents", "skills", "refunds", "SKILL.md")
	require.NoError(t, os.MkdirAll(filepath.Dir(sourcePath), 0o755))
	require.NoError(t, os.WriteFile(sourcePath, []byte(`---
name: refunds
description: Local refund guidance.
license: Apache-2.0
---

# Refunds

Use the local policy.
`), 0o644))
	api := &directAPI{
		skills: map[string]versionedSkill{"refunds": {
			Skill: syncdomain.Skill{
				Key: "refunds", Name: "Refunds", Description: "Server refund guidance.",
				Markdown: "# Refunds\n\nUse the server policy.\n",
			},
			Version: 2,
		}},
		directions: map[string]syncdomain.SyncDirection{
			"skill:refunds": syncdomain.SyncDirectionServerCanonical,
		},
	}
	id := syncdomain.ResourceID{Kind: syncdomain.KindSkill, ProjectKey: "production", LookupKey: "refunds"}

	output, _, err := runScoped(t, root, api, syncprompt.Options{
		Scope: syncdomain.ScopeSkills,
		Action: syncprompt.SourceLinkAction{
			ID: id, File: ".agents/skills/refunds/SKILL.md", Format: "agent-skill",
		},
		Yes: true,
	})

	require.NoError(t, err)
	assert.Contains(t, output, `"action": "update_local"`)
	content, err := os.ReadFile(sourcePath)
	require.NoError(t, err)
	assert.Contains(t, string(content), "description: Server refund guidance.")
	assert.Contains(t, string(content), "Use the server policy.")
	assert.Contains(t, string(content), "license: Apache-2.0")
	require.FileExists(t, filepath.Join(root, ".launchdarkly", "production", "skills", "refunds.ref.yaml"))
}

func TestStandaloneToolSourceLinkPushesMappedFields(t *testing.T) {
	root := initRepository(t)
	source := []byte(`import { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
const server = new McpServer({ name: "orders", version: "1" });
server.registerTool("lookup", {
  description: "Look up an order.",
  inputSchema: { type: "object" },
}, handler);
`)
	require.NoError(t, os.WriteFile(filepath.Join(root, "tools.ts"), source, 0o644))
	api := &directAPI{
		tools: map[string]versionedTool{"lookup": {
			Tool:    syncdomain.Tool{Key: "lookup", Description: pointer("Old description."), Schema: map[string]any{"type": "string"}},
			Version: 1,
		}},
		directions: map[string]syncdomain.SyncDirection{
			"tool:lookup": syncdomain.SyncDirectionCodeCanonical,
		},
	}
	id := syncdomain.ResourceID{Kind: syncdomain.KindTool, ProjectKey: "production", LookupKey: "lookup"}

	output, _, err := runScoped(t, root, api, syncprompt.Options{
		Scope: syncdomain.ScopeTools,
		Action: syncprompt.SourceLinkAction{
			ID: id, File: "tools.ts", Format: "typescript", Select: `tools[id="lookup"]`,
			Map: map[string]string{"description": "description", "schema": "inputSchema"},
		},
		Yes: true,
	})

	require.NoError(t, err)
	assert.Contains(t, output, `"action": "update_server"`)
	assert.Equal(t, "Look up an order.", *api.tools["lookup"].Description)
	assert.Equal(t, "object", api.tools["lookup"].Schema["type"])
	actual, err := os.ReadFile(filepath.Join(root, "tools.ts"))
	require.NoError(t, err)
	assert.Equal(t, source, actual)
	require.FileExists(t, filepath.Join(root, ".launchdarkly", "production", "tools", "lookup.ref.yaml"))
}

func TestSourceLinkWithoutConfirmationWritesNothing(t *testing.T) {
	root := initRepository(t)
	require.NoError(t, os.WriteFile(filepath.Join(root, "prompt.md"), []byte("Server instructions.\n"), 0o644))
	server := variation("Default")
	server.Instructions = "Server instructions."
	api := &directAPI{
		variation: &server,
		directions: map[string]syncdomain.SyncDirection{
			"variation:support/default": syncdomain.SyncDirectionBoth,
		},
	}
	id := syncdomain.VariationID("production", "support", "default")

	_, notes, err := runScoped(t, root, api, syncprompt.Options{
		Scope: syncdomain.ScopePrompts,
		Action: syncprompt.SourceLinkAction{
			ID: id, File: "prompt.md", Format: "markdown",
		},
	})

	require.ErrorContains(t, err, "rerun with --yes")
	assert.Contains(t, notes, "Proposed descriptor .launchdarkly/production/configs/support/default.ref.yaml:")
	assert.Contains(t, notes, "format: markdown")
	require.NoFileExists(t, filepath.Join(
		root, ".launchdarkly", "production", "configs", "support", "default.ref.yaml",
	))
}

func TestStructuredSourceLinksPushAndPull(t *testing.T) {
	tests := []struct {
		name         string
		scope        syncdomain.Scope
		id           syncdomain.ResourceID
		file         string
		format       string
		selectPath   string
		fieldMap     map[string]string
		source       string
		api          *directAPI
		changeServer func(*directAPI)
		wantPush     func(*testing.T, *directAPI)
		wantPull     string
	}{
		{
			name: "JSON tool", scope: syncdomain.ScopeTools,
			id:   syncdomain.ResourceID{Kind: syncdomain.KindTool, ProjectKey: "production", LookupKey: "lookup"},
			file: "tools.json", format: "json", selectPath: `tools[1]`,
			fieldMap: map[string]string{"description": "description", "schema": "inputSchema"},
			source:   `{"tools":[{"name":"keep","description":"Keep","inputSchema":{"type":"null"}},{"name":"lookup","description":"Local lookup","inputSchema":{"type":"object"}}]}` + "\n",
			api: &directAPI{
				tools: map[string]versionedTool{"lookup": {
					Tool:    syncdomain.Tool{Key: "lookup", Description: pointer("Server lookup"), Schema: map[string]any{"type": "string"}},
					Version: 1,
				}},
				directions: map[string]syncdomain.SyncDirection{"tool:lookup": syncdomain.SyncDirectionCodeCanonical},
			},
			changeServer: func(api *directAPI) {
				tool := api.tools["lookup"]
				tool.Description = pointer("Pulled lookup")
				api.tools["lookup"] = tool
				api.directions["tool:lookup"] = syncdomain.SyncDirectionServerCanonical
			},
			wantPush: func(t *testing.T, api *directAPI) {
				assert.Equal(t, "Local lookup", *api.tools["lookup"].Description)
				assert.Equal(t, "object", api.tools["lookup"].Schema["type"])
			},
			wantPull: `"description":"Pulled lookup"`,
		},
		{
			name: "YAML variation", scope: syncdomain.ScopePrompts,
			id:   syncdomain.VariationID("production", "support", "default"),
			file: "agents.yaml", format: "yaml", selectPath: `agents[1]`,
			fieldMap: map[string]string{"prompt": "prompt", "model": "model"},
			source:   "agents:\n  - {name: keep, prompt: Keep, model: old}\n  - name: support\n    prompt: Local YAML prompt\n    model: gpt-5\n",
			api: &directAPI{
				variation: pointer(variation("Server")),
				directions: map[string]syncdomain.SyncDirection{
					"variation:support/default": syncdomain.SyncDirectionCodeCanonical,
				},
			},
			changeServer: func(api *directAPI) {
				api.variation.Instructions = "Pulled YAML prompt"
				api.directions["variation:support/default"] = syncdomain.SyncDirectionServerCanonical
			},
			wantPush: func(t *testing.T, api *directAPI) {
				assert.Equal(t, "Local YAML prompt", api.variation.Instructions)
				assert.Equal(t, "gpt-5", api.variation.Model["modelName"])
			},
			wantPull: "prompt: Pulled YAML prompt",
		},
		{
			name: "TOML variation", scope: syncdomain.ScopePrompts,
			id:   syncdomain.VariationID("production", "support", "default"),
			file: "agents.toml", format: "toml", selectPath: "agents[1]",
			fieldMap: map[string]string{"prompt": "prompt", "model": "model"},
			source:   "agents = [{ name = \"keep\", prompt = \"Keep\", model = \"old\" }, { name = \"support\", prompt = \"Local TOML prompt\", model = \"gpt-5\" }]\n",
			api: &directAPI{
				variation: pointer(variation("Server")),
				directions: map[string]syncdomain.SyncDirection{
					"variation:support/default": syncdomain.SyncDirectionCodeCanonical,
				},
			},
			changeServer: func(api *directAPI) {
				api.variation.Instructions = "Pulled TOML prompt"
				api.directions["variation:support/default"] = syncdomain.SyncDirectionServerCanonical
			},
			wantPush: func(t *testing.T, api *directAPI) {
				assert.Equal(t, "Local TOML prompt", api.variation.Instructions)
				assert.Equal(t, "gpt-5", api.variation.Model["modelName"])
			},
			wantPull: `prompt = "Pulled TOML prompt"`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := initRepository(t)
			require.NoError(t, os.WriteFile(filepath.Join(root, test.file), []byte(test.source), 0o644))
			_, _, err := runScoped(t, root, test.api, syncprompt.Options{
				Scope: test.scope,
				Action: syncprompt.SourceLinkAction{
					ID: test.id, File: test.file, Format: test.format,
					Select: test.selectPath, Map: test.fieldMap,
				},
				Yes: true,
			})
			require.NoError(t, err)
			test.wantPush(t, test.api)
			lock, err := os.ReadFile(filepath.Join(root, ".launchdarkly", "sync.lock"))
			require.NoError(t, err)
			assert.Contains(t, string(lock), "formatVersion: 1")
			assert.Contains(t, string(lock), "indexedSelections:")

			test.changeServer(test.api)
			filter := syncdomain.Filter{Project: "production", Keys: []string{test.id.LookupKey}}
			if test.id.Kind == syncdomain.KindVariation {
				filter.ConfigKey, filter.Keys[0], _ = test.id.VariationKeys()
			}
			_, _, err = runScoped(t, root, test.api, syncprompt.Options{
				Scope: test.scope, Action: syncprompt.SyncAction{Filter: filter}, Yes: true,
			})
			require.NoError(t, err)
			content, err := os.ReadFile(filepath.Join(root, test.file))
			require.NoError(t, err)
			assert.Contains(t, string(content), test.wantPull)
			assert.Contains(t, string(content), "Keep")

			_, _, err = runScoped(t, root, test.api, syncprompt.Options{
				Scope: test.scope, Action: syncprompt.SyncAction{Filter: filter}, Yes: true,
			})
			require.NoError(t, err)
		})
	}
}

func TestIndexedSourceReorderBlocksSyncWithoutWrites(t *testing.T) {
	root := initRepository(t)
	sourcePath := filepath.Join(root, "tools.json")
	original := `{"tools":[{"name":"keep","description":"Keep","inputSchema":{"type":"null"}},{"name":"lookup","description":"Local lookup","inputSchema":{"type":"object"}}]}` + "\n"
	require.NoError(t, os.WriteFile(sourcePath, []byte(original), 0o644))
	api := &directAPI{
		tools: map[string]versionedTool{"lookup": {
			Tool: syncdomain.Tool{
				Key: "lookup", Description: pointer("Server lookup"), Schema: map[string]any{"type": "string"},
			},
			Version: 1,
		}},
		directions: map[string]syncdomain.SyncDirection{
			"tool:lookup": syncdomain.SyncDirectionCodeCanonical,
		},
	}
	id := syncdomain.ResourceID{Kind: syncdomain.KindTool, ProjectKey: "production", LookupKey: "lookup"}
	_, _, err := runScoped(t, root, api, syncprompt.Options{
		Scope: syncdomain.ScopeTools,
		Action: syncprompt.SourceLinkAction{
			ID: id, File: "tools.json", Format: "json", Select: "tools[1]",
			Map: map[string]string{"description": "description", "schema": "inputSchema"},
		},
		Yes: true,
	})
	require.NoError(t, err)
	assert.Equal(t, "Local lookup", *api.tools["lookup"].Description)

	reordered := `{"tools":[{"name":"lookup","description":"Different item","inputSchema":{"type":"boolean"}},{"name":"keep","description":"Keep","inputSchema":{"type":"null"}}]}` + "\n"
	require.NoError(t, os.WriteFile(sourcePath, []byte(reordered), 0o644))
	_, _, err = runScoped(t, root, api, syncprompt.Options{
		Scope: syncdomain.ScopeTools,
		Action: syncprompt.SyncAction{Filter: syncdomain.Filter{
			Project: "production", Keys: []string{"lookup"},
		}},
		Yes: true,
	})

	problem, ok := syncdomain.ProblemFrom(err)
	require.True(t, ok)
	assert.Equal(t, syncdomain.ErrorIndexedSelectionChanged, problem.Code)
	assert.Equal(t, "Local lookup", *api.tools["lookup"].Description)
	content, readErr := os.ReadFile(sourcePath)
	require.NoError(t, readErr)
	assert.Equal(t, reordered, string(content))
}

func TestReadOnlyMappedFieldBlocksCompleteLink(t *testing.T) {
	root := initRepository(t)
	source := `import { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
const server = new McpServer({ name: "orders", version: "1" });
server.registerTool("lookup", {
  description: "Local description",
  inputSchema: makeSchema(),
}, handler);
`
	require.NoError(t, os.WriteFile(filepath.Join(root, "tools.ts"), []byte(source), 0o644))
	api := &directAPI{
		tools: map[string]versionedTool{"lookup": {
			Tool: syncdomain.Tool{
				Key: "lookup", Description: pointer("Server description"),
				Schema: map[string]any{"type": "object"},
			},
			Version: 1,
		}},
		directions: map[string]syncdomain.SyncDirection{
			"tool:lookup": syncdomain.SyncDirectionCodeCanonical,
		},
	}
	id := syncdomain.ResourceID{Kind: syncdomain.KindTool, ProjectKey: "production", LookupKey: "lookup"}
	action := syncprompt.SourceLinkAction{
		ID: id, File: "tools.ts", Format: "typescript", Select: `tools[id="lookup"]`,
		Map: map[string]string{"description": "description", "schema": "inputSchema"},
	}

	_, _, err := runScoped(t, root, api, syncprompt.Options{
		Scope: syncdomain.ScopeTools, Action: action, Yes: true,
	})
	problem, ok := syncdomain.ProblemFrom(err)
	require.True(t, ok)
	assert.Equal(t, syncdomain.ErrorFieldReadOnly, problem.Code)
	assert.Contains(t, problem.Message, `field "schema"`)
	assert.Equal(t, "Server description", *api.tools["lookup"].Description)
	assert.Equal(t, "object", api.tools["lookup"].Schema["type"])
	content, err := os.ReadFile(filepath.Join(root, "tools.ts"))
	require.NoError(t, err)
	assert.Contains(t, string(content), `description: "Local description"`)
	assert.Contains(t, string(content), "inputSchema: makeSchema()")
	require.NoFileExists(t, filepath.Join(root, ".launchdarkly", "production", "tools", "lookup.ref.yaml"))
}

func TestMarkdownProviderProfilesPullAndPreserveMetadata(t *testing.T) {
	tests := []struct {
		name, format, source, metadata string
	}{
		{name: "Cursor", format: "markdown", source: "---\nglobs: \"src/**/*.ts\"\nalwaysApply: false\n---\n\nLocal\n", metadata: "alwaysApply: false"},
		{name: "Copilot", format: "markdown", source: "---\napplyTo: \"src/**/*.ts\"\n---\n\nLocal\n", metadata: "applyTo: \"src/**/*.ts\""},
		{name: "Claude", format: "markdown", source: "---\nname: support\nmodel: sonnet\n---\n\nLocal\n", metadata: "model: sonnet"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := initRepository(t)
			require.NoError(t, os.WriteFile(filepath.Join(root, "instructions.md"), []byte(test.source), 0o644))
			server := variation("Server")
			server.Instructions = "Pulled profile instructions."
			api := &directAPI{
				variation: &server,
				directions: map[string]syncdomain.SyncDirection{
					"variation:support/default": syncdomain.SyncDirectionServerCanonical,
				},
			}
			id := syncdomain.VariationID("production", "support", "default")

			_, _, err := runScoped(t, root, api, syncprompt.Options{
				Scope: syncdomain.ScopePrompts,
				Action: syncprompt.SourceLinkAction{
					ID: id, File: "instructions.md", Format: test.format,
				},
				Yes: true,
			})

			require.NoError(t, err)
			content, err := os.ReadFile(filepath.Join(root, "instructions.md"))
			require.NoError(t, err)
			assert.Contains(t, string(content), test.metadata)
			assert.Contains(t, string(content), "Pulled profile instructions.")
		})
	}
}

// runScoped runs options through Runner.Run without a terminal, and returns
// the JSON output and the notes. A question fails the run.
func runScoped(t *testing.T, root string, api *directAPI, options syncprompt.Options) (string, string, error) {
	t.Helper()
	options.NoInput, options.Input = true, failingReader{}
	return runWith(t, root, api, syncprompt.NewRunner(api), options)
}

// runWith runs options through runner with JSON output, and returns the
// output and the notes.
func runWith(t *testing.T, root string, api *directAPI, runner syncprompt.Runner, options syncprompt.Options) (string, string, error) {
	t.Helper()
	if manifestsByRoot[root] == nil {
		manifestsByRoot[root] = &syncapi.SyncManifest{FormatVersion: syncapi.SyncManifestFormatVersion, Source: "git:example/repo", Items: []syncapi.SyncManifestResource{}}
	}
	api.manifest = manifestsByRoot[root]
	var output, notes bytes.Buffer
	options.WorkingDirectory, options.AccessToken, options.BaseURI = root, "token", "https://example.test"
	options.OutputKind, options.Output, options.ErrorOutput = "json", &output, &notes
	err := runner.Run(options)
	return output.String(), notes.String(), err
}

// failingReader fails every read, so a test fails if the runner asks a
// question.
type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, errors.New("unexpected read of input") }

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
		manifest = &syncapi.SyncManifest{FormatVersion: syncapi.SyncManifestFormatVersion, Source: "git:example/repo", Items: []syncapi.SyncManifestResource{}}
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
	remote := &syncapi.SyncManifest{FormatVersion: syncapi.SyncManifestFormatVersion, Source: "git:example/repo"}
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

// A server-canonical linked variation whose only difference is an older tool
// pin must repin LaunchDarkly once. Without the repin, every sync plans the
// same pull again.
func TestServerCanonicalSourcePullRepinsStaleToolVersions(t *testing.T) {
	root := initRepository(t)
	require.NoError(t, os.WriteFile(filepath.Join(root, "prompt.md"), []byte("Help\n"), 0o644))
	writeLocalFile(t, root, "configs/support/default.ref.yaml",
		"formatVersion: 1\nfile: prompt.md\nformat: markdown\ntools:\n  - key: search\n")
	writeLocalFile(t, root, "tools/search.json",
		`{"formatVersion":1,"key":"search","schema":{"type":"object"}}`+"\n")
	server := variation("Server")
	server.Tools = []syncdomain.AttachmentRef{{Key: "search", Version: 1}}
	api := &directAPI{
		variation: &server,
		tools: map[string]versionedTool{"search": {
			Tool: syncdomain.Tool{Key: "search", Schema: map[string]any{"type": "object"}}, Version: 2,
		}},
		directions: map[string]syncdomain.SyncDirection{
			"variation:support/default": syncdomain.SyncDirectionServerCanonical,
		},
	}

	stdout, _, err := runPrompt(t, root, api, "--dry-run", "--output", "json")
	require.NoError(t, err)
	assert.Equal(t, []string{"variation support/default update_local"}, planRows(t, stdout))

	_, _, err = runPrompt(t, root, api, "--yes")

	require.NoError(t, err)
	assert.Equal(t, []syncdomain.AttachmentRef{{Key: "search", Version: 2}}, api.variation.Tools)
	stdout, _, err = runPrompt(t, root, api, "--dry-run", "--output", "json")
	require.NoError(t, err)
	assert.Equal(t, []string{"variation support/default in_sync"}, planRows(t, stdout), "the pins are current")
}

// A link that only rebinds a resource that is in sync must still write the
// descriptor and record the new mapping in sync.lock. Otherwise the next sync
// raises the same stale-binding error that the link was meant to fix.
func TestRelinkOfAnInSyncResourceRecordsTheNewBinding(t *testing.T) {
	root := initRepository(t)
	require.NoError(t, os.WriteFile(filepath.Join(root, "agents.py"), []byte(`from agents import Agent
support = Agent(name="support", instructions="Help")
`), 0o644))
	server := variation("Server")
	api := &directAPI{
		variation: &server,
		directions: map[string]syncdomain.SyncDirection{
			"variation:support/default": syncdomain.SyncDirectionBoth,
		},
	}
	id := syncdomain.VariationID("production", "support", "default")
	link := func(selectPath string) (string, error) {
		output, _, err := runScoped(t, root, api, syncprompt.Options{
			Scope: syncdomain.ScopePrompts,
			Action: syncprompt.SourceLinkAction{
				ID: id, File: "agents.py", Format: "python", Select: selectPath,
				Map: map[string]string{"prompt": "instructions"},
			},
			Yes: true,
		})
		return output, err
	}
	_, err := link(`agents[id="support"]`)
	require.NoError(t, err)

	// The second link changes only the selector, and the content stays in sync.
	_, err = link(`agents[0]`)
	require.NoError(t, err)

	descriptor, err := os.ReadFile(filepath.Join(root, ".launchdarkly", "production", "configs", "support", "default.ref.yaml"))
	require.NoError(t, err)
	assert.Contains(t, string(descriptor), "select: agents[0]")
	stdout, _, err := runPrompt(t, root, api, "--dry-run", "--output", "json")
	require.NoError(t, err)
	assert.Equal(t, []string{"variation support/default in_sync"}, planRows(t, stdout), "the lock has the new binding")
}

// A variation push must not publish a linked tool against its LaunchDarkly
// sync setting. The tool gets a row of its own, so the setting decides.
func TestVariationPushRespectsTheSettingOfALinkedTool(t *testing.T) {
	root := initRepository(t)
	require.NoError(t, os.WriteFile(filepath.Join(root, "tool.json"), []byte(`{"description":"Local","schema":{"type":"object"}}`), 0o644))
	writeLocalFile(t, root, "tools/search.ref.yaml",
		"formatVersion: 1\nfile: tool.json\nformat: json\nselect: $\nmap:\n  description: description\n  schema: schema\n")
	local := variation("Local name")
	local.Tools = []syncdomain.AttachmentRef{{Key: "search"}}
	local.Attachments = []syncdomain.Attachment{{Kind: syncdomain.AttachmentTool, Tool: &syncdomain.Tool{
		Key: "search", Description: pointer("Local"), Schema: map[string]any{"type": "object"},
	}}}
	writeVariation(t, root, local, true)
	serverTool := syncdomain.Tool{Key: "search", Description: pointer("Server"), Schema: map[string]any{"type": "object"}}
	server := variation("Server name")
	server.Tools = []syncdomain.AttachmentRef{{Key: "search", Version: 1}}
	server.Attachments = []syncdomain.Attachment{toolAttachment(serverTool, 1)}
	// The last sync left LaunchDarkly as it is now, so only the local side changed.
	writeManifest(t, root, server)
	linked, err := synclocal.CompileAttachments(root, func(syncdomain.ResourceID) bool { return true })
	require.NoError(t, err)
	require.Len(t, linked, 1)
	recorded, err := syncdomain.SyncedAttachment{
		ProjectKey: "production", Attachment: toolAttachment(serverTool, 1), Source: linked[0].Source,
	}.Fingerprint()
	require.NoError(t, err)
	lock := readLock(t, root)
	lock.SetFingerprint(linked[0].ID(), recorded)
	require.NoError(t, lock.SetSource(linked[0].ID(), linked[0].Source, "json/1", linked[0].IndexedSelections))
	require.NoError(t, syncmanifest.WriteLock(synclocal.NewStore(root), lock))
	api := &directAPI{
		variation:  &server,
		tools:      map[string]versionedTool{"search": {Tool: serverTool, Version: 1}},
		directions: map[string]syncdomain.SyncDirection{"tool:search": syncdomain.SyncDirectionServerCanonical},
	}

	stdout, _, err := runPrompt(t, root, api, "--dry-run", "--output", "json")
	require.NoError(t, err)
	assert.Equal(t, []string{"tool search update_local", "variation support/default update_server"}, planRows(t, stdout))

	_, _, err = runPrompt(t, root, api, "--yes")

	require.NoError(t, err)
	assert.Equal(t, "Server", *api.tools["search"].Description, "the server-canonical tool is not overwritten")
	assert.Equal(t, 1, api.tools["search"].Version)
	assert.Equal(t, "Local name", api.variation.Name, "the variation is pushed")
	data, err := os.ReadFile(filepath.Join(root, "tool.json"))
	require.NoError(t, err)
	assert.Contains(t, string(data), `"Server"`, "the linked source follows LaunchDarkly")
}

// A variation push publishes the tools that it uses. A linked tool that
// LaunchDarkly deleted gets its row with the detach problem, so the push does
// not recreate the tool from the repository copy.
func TestVariationPushDoesNotRecreateADeletedLinkedTool(t *testing.T) {
	root := initRepository(t)
	require.NoError(t, os.WriteFile(filepath.Join(root, "tool.json"), []byte(`{"description":"Local","schema":{"type":"object"}}`), 0o644))
	writeLocalFile(t, root, "tools/search.ref.yaml",
		"formatVersion: 1\nfile: tool.json\nformat: json\nselect: $\nmap:\n  description: description\n  schema: schema\n")
	local := variation("Local name")
	local.Tools = []syncdomain.AttachmentRef{{Key: "search"}}
	local.Attachments = []syncdomain.Attachment{{Kind: syncdomain.AttachmentTool, Tool: &syncdomain.Tool{
		Key: "search", Description: pointer("Local"), Schema: map[string]any{"type": "object"},
	}}}
	writeVariation(t, root, local, true)
	// The last sync tracked the tool on its own, and the variation did not
	// use it yet. Since then, the repository added the tool to the variation
	// and LaunchDarkly deleted the tool.
	syncedTool := syncdomain.Tool{Key: "search", Description: pointer("Local"), Schema: map[string]any{"type": "object"}}
	server := variation("Server name")
	writeManifest(t, root, server)
	linked, err := synclocal.CompileAttachments(root, func(syncdomain.ResourceID) bool { return true })
	require.NoError(t, err)
	require.Len(t, linked, 1)
	recorded, err := syncdomain.SyncedAttachment{
		ProjectKey: "production", Attachment: toolAttachment(syncedTool, 1), Source: linked[0].Source,
	}.Fingerprint()
	require.NoError(t, err)
	lock := readLock(t, root)
	lock.SetFingerprint(linked[0].ID(), recorded)
	require.NoError(t, lock.SetSource(linked[0].ID(), linked[0].Source, "json/1", linked[0].IndexedSelections))
	require.NoError(t, syncmanifest.WriteLock(synclocal.NewStore(root), lock))
	api := &directAPI{
		variation:  &server,
		tools:      map[string]versionedTool{},
		directions: map[string]syncdomain.SyncDirection{"tool:search": syncdomain.SyncDirectionBoth},
	}

	stdout, _, err := runPrompt(t, root, api, "--dry-run", "--output", "json")

	require.Error(t, err, "the deleted linked tool blocks the plan")
	assert.Equal(t, []string{"tool search error", "variation support/default update_server"}, planRows(t, stdout))
	assert.Contains(t, stdout, "linked resource no longer exists in LaunchDarkly")
	_, _, err = runPrompt(t, root, api, "--yes")
	require.Error(t, err)
	assert.Empty(t, api.tools, "the push does not recreate the tool")
	assert.Equal(t, "Server name", api.variation.Name, "the variation is not pushed")
}

// A variation push publishes the tools that it uses. A linked tool that
// LaunchDarkly does not have yet keeps its error row, which a descriptor
// cannot clear with upsert, so the push does not create the tool against its
// server-canonical setting.
func TestVariationPushDoesNotCreateAServerCanonicalLinkedTool(t *testing.T) {
	root := initRepository(t)
	require.NoError(t, os.WriteFile(filepath.Join(root, "tool.json"), []byte(`{"description":"Local","schema":{"type":"object"}}`), 0o644))
	writeLocalFile(t, root, "tools/search.ref.yaml",
		"formatVersion: 1\nfile: tool.json\nformat: json\nselect: $\nmap:\n  description: description\n  schema: schema\n")
	local := variation("Local name")
	local.Tools = []syncdomain.AttachmentRef{{Key: "search"}}
	local.Attachments = []syncdomain.Attachment{{Kind: syncdomain.AttachmentTool, Tool: &syncdomain.Tool{
		Key: "search", Description: pointer("Local"), Schema: map[string]any{"type": "object"},
	}}}
	writeVariation(t, root, local, true)
	server := variation("Server name")
	writeManifest(t, root, server)
	api := &directAPI{
		variation:  &server,
		tools:      map[string]versionedTool{},
		directions: map[string]syncdomain.SyncDirection{"tool:search": syncdomain.SyncDirectionServerCanonical},
	}

	stdout, _, err := runPrompt(t, root, api, "--dry-run", "--output", "json")

	require.Error(t, err, "the missing linked tool blocks the plan")
	assert.Equal(t, []string{"tool search error", "variation support/default update_server"}, planRows(t, stdout))
	assert.Contains(t, stdout, `tool \"search\" does not exist in LaunchDarkly. Create it in LaunchDarkly and run sync again.`)
	assert.NotContains(t, stdout, "upsert", "a descriptor has no upsert, so the message does not ask for one")
	_, _, err = runPrompt(t, root, api, "--yes")
	require.Error(t, err)
	assert.Empty(t, api.tools, "the push does not create the tool")
	assert.Equal(t, "Server name", api.variation.Name, "the variation is not pushed")
}

// A link whose apply synced the resource keeps its descriptor, even when a
// later step fails, because LaunchDarkly has the new content.
func TestLinkKeepsTheDescriptorWhenAStepAfterTheSyncFails(t *testing.T) {
	root := initRepository(t)
	require.NoError(t, os.WriteFile(filepath.Join(root, "agents.py"), []byte("from agents import Agent\nsupport = Agent(instructions=\"Local text\")\n"), 0o644))
	server := variation("Server")
	api := &directAPI{
		variation:  &server,
		directions: map[string]syncdomain.SyncDirection{"variation:support/default": syncdomain.SyncDirectionBoth},
	}
	// The push of the variation succeeds, and the manifest write after it fails.
	api.onRequest = func(method, path string) {
		if method == http.MethodPatch && strings.Contains(path, "/variations/") {
			api.manifestWriteErr = errors.New("manifest write failed")
		}
	}
	id := syncdomain.VariationID("production", "support", "default")
	// Both sides have text, so the first sync is a conflict that the local
	// side wins, which pushes the linked text.
	resolution, err := syncprompt.ParseConflictResolution("local")
	require.NoError(t, err)

	_, _, err = runScoped(t, root, api, syncprompt.Options{
		Scope: syncdomain.ScopePrompts,
		Action: syncprompt.SourceLinkAction{
			ID: id, File: "agents.py", Format: "python", Select: `agents[id="support"]`,
			Map: map[string]string{"prompt": "instructions"},
		},
		Yes: true, NoInput: true, ConflictPolicy: syncprompt.ConflictPolicy{Default: resolution},
	})

	require.Error(t, err, "the manifest write failed")
	assert.Equal(t, "Local text", api.variation.Instructions, "the push happened")
	descriptorPath := filepath.Join(root, ".launchdarkly", "production", "configs", "support", "default.ref.yaml")
	require.FileExists(t, descriptorPath, "the descriptor stays, because LaunchDarkly has the linked content")
}

// A link that writes its descriptor and then cannot finish puts the previous
// descriptor back, so the next sync does not see a binding that the lock
// never recorded.
func TestFailedLinkRestoresThePreviousDescriptor(t *testing.T) {
	root := initRepository(t)
	source := []byte("from agents import Agent\nsupport = Agent(instructions=\"Help\")\n")
	require.NoError(t, os.WriteFile(filepath.Join(root, "agents.py"), source, 0o644))
	server := variation("Server")
	api := &directAPI{
		variation:  &server,
		directions: map[string]syncdomain.SyncDirection{"variation:support/default": syncdomain.SyncDirectionBoth},
	}
	id := syncdomain.VariationID("production", "support", "default")
	link := func() error {
		_, _, err := runScoped(t, root, api, syncprompt.Options{
			Scope: syncdomain.ScopePrompts,
			Action: syncprompt.SourceLinkAction{
				ID: id, File: "agents.py", Format: "python", Select: `agents[id="support"]`,
				Map: map[string]string{"prompt": "instructions"},
			},
			Yes: true,
		})
		return err
	}
	require.NoError(t, link())
	descriptorPath := filepath.Join(root, ".launchdarkly", "production", "configs", "support", "default.ref.yaml")
	previous, err := os.ReadFile(descriptorPath)
	require.NoError(t, err)

	// LaunchDarkly changes between the review and the apply, so the second
	// link cannot finish.
	api.onRequest = func(method, path string) {
		if method == http.MethodGet && strings.HasSuffix(path, "/ai-configs/support") && len(api.requests) > 8 {
			api.variation.Instructions = "Changed after review"
		}
	}
	require.NoError(t, os.WriteFile(filepath.Join(root, "agents.py"), []byte("from agents import Agent\nsupport = Agent(instructions=\"Help again\")\n"), 0o644))
	_, _, err = runScoped(t, root, api, syncprompt.Options{
		Scope: syncdomain.ScopePrompts,
		Action: syncprompt.SourceLinkAction{
			ID: id, File: "agents.py", Format: "python", Select: `agents[0]`,
			Map: map[string]string{"prompt": "instructions"},
		},
		Yes: true,
	})
	require.Error(t, err)
	current, err := os.ReadFile(descriptorPath)
	require.NoError(t, err)
	assert.Equal(t, string(previous), string(current), "the previous descriptor is back")
}

// linkedVariationForPush links the variation to agents.py with a prompt map,
// records a baseline that matches LaunchDarkly, and changes the local text,
// so that the plan is a push of the mapped field.
func linkedVariationForPush(t *testing.T) (string, *directAPI) {
	t.Helper()
	root := initRepository(t)
	require.NoError(t, os.WriteFile(filepath.Join(root, "agents.py"), []byte("from agents import Agent\nsupport = Agent(instructions=\"Local text\")\n"), 0o644))
	writeLocalFile(t, root, "configs/support/default.ref.yaml",
		"formatVersion: 1\nfile: agents.py\nformat: python\nselect: 'agents[id=\"support\"]'\nmap:\n  prompt: instructions\n")
	compiled, err := synclocal.CompileWorkspace(root)
	require.NoError(t, err)
	require.Len(t, compiled, 1)
	server := variation("Server name")
	server.Instructions = "Old text"
	writeManifestResources(t, root)
	lock := readLock(t, root)
	projection, err := syncdomain.ProjectVariation(server, *compiled[0].Source)
	require.NoError(t, err)
	recorded, err := syncdomain.FingerprintProjection(compiled[0].ID(), projection)
	require.NoError(t, err)
	lock.SetFingerprint(compiled[0].ID(), recorded)
	require.NoError(t, lock.SetSource(compiled[0].ID(), compiled[0].Source, "python/1", compiled[0].IndexedSelections))
	require.NoError(t, syncmanifest.WriteLock(synclocal.NewStore(root), lock))
	api := &directAPI{
		variation:  &server,
		directions: map[string]syncdomain.SyncDirection{"variation:support/default": syncdomain.SyncDirectionBoth},
	}
	return root, api
}

// onNthRead runs change before the nth read of the path with the suffix. A
// push reads a resource for the plan, for the reload after the review, and
// for the merge in the apply, so the third read is the one after the review.
func onNthRead(api *directAPI, suffix string, n int, change func()) {
	reads := 0
	api.onRequest = func(method, path string) {
		if method == http.MethodGet && strings.HasSuffix(path, suffix) {
			reads++
			if reads == n {
				change()
			}
		}
	}
}

// A linked variation push merges its mapped fields onto the latest
// LaunchDarkly copy, so an edit to an unowned field after the review survives.
func TestLinkedVariationPushKeepsUnownedFieldsChangedAfterReview(t *testing.T) {
	root, api := linkedVariationForPush(t)
	onNthRead(api, "/ai-configs/support", 3, func() { api.variation.Name = "Renamed after review" })

	_, _, err := runPrompt(t, root, api, "--yes")

	require.NoError(t, err)
	assert.Equal(t, "Local text", api.variation.Instructions, "the mapped field is pushed")
	assert.Equal(t, "Renamed after review", api.variation.Name, "the unowned field keeps the LaunchDarkly edit")
}

// A change to a mapped field after the review is not pushed over, because the
// plan never compared it.
func TestLinkedVariationPushStopsWhenMappedFieldsChangeAfterReview(t *testing.T) {
	root, api := linkedVariationForPush(t)
	onNthRead(api, "/ai-configs/support", 3, func() { api.variation.Instructions = "Changed after review" })

	_, _, err := runPrompt(t, root, api, "--yes")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "changed in LaunchDarkly after review")
	assert.Equal(t, "Changed after review", api.variation.Instructions, "the push does not write over the change")
}

// A linked tool push merges its mapped fields onto the latest LaunchDarkly
// copy. A change to a mapped field after the review is not pushed over,
// because the plan never compared it.
func TestLinkedToolPushStopsWhenMappedFieldsChangeAfterReview(t *testing.T) {
	root := initRepository(t)
	require.NoError(t, os.WriteFile(filepath.Join(root, "tool.json"), []byte(`{"description":"Local text","schema":{"type":"object"}}`), 0o644))
	writeLocalFile(t, root, "tools/search.ref.yaml",
		"formatVersion: 1\nfile: tool.json\nformat: json\nselect: $\nmap:\n  description: description\n  schema: schema\n")
	baseline := toolAttachment(syncdomain.Tool{Key: "search", Description: pointer("Old text"), Schema: map[string]any{"type": "object"}}, 1)
	writeManifestResources(t, root)
	trackAttachment(t, root, baseline)
	linked, err := synclocal.CompileAttachments(root, func(syncdomain.ResourceID) bool { return true })
	require.NoError(t, err)
	recorded, err := syncdomain.SyncedAttachment{ProjectKey: "production", Attachment: baseline, Source: linked[0].Source}.Fingerprint()
	require.NoError(t, err)
	lock := readLock(t, root)
	lock.SetFingerprint(linked[0].ID(), recorded)
	require.NoError(t, lock.SetSource(linked[0].ID(), linked[0].Source, "json/1", linked[0].IndexedSelections))
	require.NoError(t, syncmanifest.WriteLock(synclocal.NewStore(root), lock))
	serverTool := syncdomain.Tool{Key: "search", Description: pointer("Old text"), Schema: map[string]any{"type": "object"}}
	api := &directAPI{
		tools:      map[string]versionedTool{"search": {Tool: serverTool, Version: 1}},
		directions: map[string]syncdomain.SyncDirection{"tool:search": syncdomain.SyncDirectionBoth},
	}
	onNthRead(api, "/ai-tools/search", 3, func() {
		tool := api.tools["search"]
		tool.Description = pointer("Changed after review")
		api.tools["search"] = tool
	})

	_, _, err = runScoped(t, root, api, syncprompt.Options{
		Scope: syncdomain.ScopeTools, Action: syncprompt.SyncAction{Filter: syncdomain.Filter{Project: "production"}},
		Yes: true, NoInput: true,
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "changed in LaunchDarkly after review")
	assert.Equal(t, "Changed after review", *api.tools["search"].Description, "the push does not write over the change")
}

// A prompts run with an in-sync variation does not rewrite the baseline of a
// linked tool that it did not sync, so a local edit to the tool stays a local
// change for the next tools run.
func TestInSyncPromptRunKeepsTheBaselineOfAnUnsyncedLinkedTool(t *testing.T) {
	root := initRepository(t)
	require.NoError(t, os.WriteFile(filepath.Join(root, "agents.py"), []byte("from agents import Agent\nsupport = Agent(instructions=\"Same text\")\n"), 0o644))
	writeLocalFile(t, root, "configs/support/default.ref.yaml",
		"formatVersion: 1\nfile: agents.py\nformat: python\nselect: 'agents[id=\"support\"]'\nmap:\n  prompt: instructions\ntools:\n  - key: search\n")
	require.NoError(t, os.WriteFile(filepath.Join(root, "tool.json"), []byte(`{"description":"Local edit","schema":{"type":"object"}}`), 0o644))
	writeLocalFile(t, root, "tools/search.ref.yaml",
		"formatVersion: 1\nfile: tool.json\nformat: json\nselect: $\nmap:\n  description: description\n  schema: schema\n")
	serverTool := syncdomain.Tool{Key: "search", Description: pointer("Server"), Schema: map[string]any{"type": "object"}}
	server := variation("Server")
	server.Instructions = "Same text"
	server.Tools = []syncdomain.AttachmentRef{{Key: "search", Version: 1}}
	server.Attachments = []syncdomain.Attachment{toolAttachment(serverTool, 1)}
	compiled, err := synclocal.CompileWorkspace(root)
	require.NoError(t, err)
	require.Len(t, compiled, 1)
	linked, err := synclocal.CompileAttachments(root, func(syncdomain.ResourceID) bool { return true })
	require.NoError(t, err)
	require.Len(t, linked, 1)
	// The last sync left the variation and the tool at the LaunchDarkly content.
	writeManifestResources(t, root)
	lock := readLock(t, root)
	projection, err := syncdomain.ProjectVariation(server, *compiled[0].Source)
	require.NoError(t, err)
	variationFingerprint, err := syncdomain.FingerprintProjection(compiled[0].ID(), projection)
	require.NoError(t, err)
	lock.SetFingerprint(compiled[0].ID(), variationFingerprint)
	require.NoError(t, lock.SetSource(compiled[0].ID(), compiled[0].Source, "python/1", compiled[0].IndexedSelections))
	recorded, err := syncdomain.SyncedAttachment{
		ProjectKey: "production", Attachment: toolAttachment(serverTool, 1), Source: linked[0].Source,
	}.Fingerprint()
	require.NoError(t, err)
	lock.SetFingerprint(linked[0].ID(), recorded)
	require.NoError(t, lock.SetSource(linked[0].ID(), linked[0].Source, "json/1", linked[0].IndexedSelections))
	require.NoError(t, syncmanifest.WriteLock(synclocal.NewStore(root), lock))
	// The push of the edited tool fails, so the tool stays unsynced while
	// the in-sync variation row completes.
	api := &directAPI{
		variation:    &server,
		tools:        map[string]versionedTool{"search": {Tool: serverTool, Version: 1}},
		toolWriteErr: errors.New("tool write failed"),
		directions: map[string]syncdomain.SyncDirection{
			"variation:support/default": syncdomain.SyncDirectionBoth, "tool:search": syncdomain.SyncDirectionBoth,
		},
	}

	stdout, _, err := runPrompt(t, root, api, "--yes", "--output", "json")

	require.Error(t, err, "the tool push failed")
	assert.Contains(t, stdout, `"action": "in_sync"`, "the linked variation is in sync on its mapped fields")
	assert.Equal(t, "Server", *api.tools["search"].Description, "LaunchDarkly keeps its copy")
	after := readLock(t, root)
	index := slices.IndexFunc(after.Resources, func(resource syncmanifest.Resource) bool { return resource.ID() == linked[0].ID() })
	require.NotEqual(t, -1, index)
	assert.Equal(t, recorded, after.Resources[index].Fingerprint, "the baseline of the unsynced tool stays the LaunchDarkly copy, so the local edit stays a local change")
}

// A variation push must not write a linked tool whose owned fields match,
// even when LaunchDarkly holds extra fields that the link does not own.
func TestVariationPushKeepsUnownedFieldsOfALinkedTool(t *testing.T) {
	root := initRepository(t)
	require.NoError(t, os.WriteFile(filepath.Join(root, "tool.json"), []byte(`{"description":"Shared","schema":{"type":"object"}}`), 0o644))
	writeLocalFile(t, root, "tools/search.ref.yaml",
		"formatVersion: 1\nfile: tool.json\nformat: json\nselect: $\nmap:\n  description: description\n  schema: schema\n")
	local := variation("Local name")
	local.Tools = []syncdomain.AttachmentRef{{Key: "search"}}
	local.Attachments = []syncdomain.Attachment{{Kind: syncdomain.AttachmentTool, Tool: &syncdomain.Tool{
		Key: "search", Description: pointer("Shared"), Schema: map[string]any{"type": "object"},
	}}}
	writeVariation(t, root, local, true)
	serverTool := syncdomain.Tool{
		Key: "search", Description: pointer("Shared"), Schema: map[string]any{"type": "object"},
		CustomParameters: map[string]any{"timeout": float64(5)},
	}
	server := variation("Server name")
	server.Tools = []syncdomain.AttachmentRef{{Key: "search", Version: 1}}
	server.Attachments = []syncdomain.Attachment{toolAttachment(serverTool, 1)}
	writeManifest(t, root, server)
	api := &directAPI{
		variation:  &server,
		tools:      map[string]versionedTool{"search": {Tool: serverTool, Version: 1}},
		directions: map[string]syncdomain.SyncDirection{"tool:search": syncdomain.SyncDirectionServerCanonical},
	}

	stdout, _, err := runPrompt(t, root, api, "--dry-run", "--output", "json")
	require.NoError(t, err)
	assert.Equal(t, []string{"variation support/default update_server"}, planRows(t, stdout), "the tool has no owned change")

	_, _, err = runPrompt(t, root, api, "--yes")

	require.NoError(t, err)
	assert.Zero(t, countRequests(api.requests, "PATCH", "/ai-tools/search"), "the linked tool is not written")
	assert.Equal(t, map[string]any{"timeout": float64(5)}, api.tools["search"].CustomParameters, "LaunchDarkly keeps its own fields")
	assert.Equal(t, "Local name", api.variation.Name)
}

// A variation push publishes the linked tools that it uses through the latest
// LaunchDarkly copy. A linked tool that was in sync at the review has no row,
// so a change to its mapped fields after the review stops the push instead of
// being written over.
func TestVariationPushStopsWhenAnInSyncLinkedToolChangesAfterReview(t *testing.T) {
	root := initRepository(t)
	require.NoError(t, os.WriteFile(filepath.Join(root, "tool.json"), []byte(`{"description":"Shared","schema":{"type":"object"}}`), 0o644))
	writeLocalFile(t, root, "tools/search.ref.yaml",
		"formatVersion: 1\nfile: tool.json\nformat: json\nselect: $\nmap:\n  description: description\n  schema: schema\n")
	local := variation("Local name")
	local.Tools = []syncdomain.AttachmentRef{{Key: "search"}}
	local.Attachments = []syncdomain.Attachment{{Kind: syncdomain.AttachmentTool, Tool: &syncdomain.Tool{
		Key: "search", Description: pointer("Shared"), Schema: map[string]any{"type": "object"},
	}}}
	writeVariation(t, root, local, true)
	serverTool := syncdomain.Tool{Key: "search", Description: pointer("Shared"), Schema: map[string]any{"type": "object"}}
	server := variation("Server name")
	server.Tools = []syncdomain.AttachmentRef{{Key: "search", Version: 1}}
	server.Attachments = []syncdomain.Attachment{toolAttachment(serverTool, 1)}
	writeManifest(t, root, server)
	api := &directAPI{
		variation:  &server,
		tools:      map[string]versionedTool{"search": {Tool: serverTool, Version: 1}},
		directions: map[string]syncdomain.SyncDirection{"tool:search": syncdomain.SyncDirectionServerCanonical},
	}
	onNthRead(api, "/ai-tools/search", 3, func() {
		tool := api.tools["search"]
		tool.Description = pointer("Changed after review")
		api.tools["search"] = tool
	})

	_, _, err := runPrompt(t, root, api, "--yes")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "changed in LaunchDarkly after review")
	assert.Zero(t, countRequests(api.requests, "PATCH", "/ai-tools/search"), "the linked tool is not written")
	assert.Equal(t, "Changed after review", *api.tools["search"].Description)
	assert.Equal(t, "Server name", api.variation.Name, "the variation is not pushed")
}

// A conflict that LaunchDarkly wins becomes a pull. The pull checks run on it
// before the review, so a pull that cannot clear a mapped field is a problem
// in the plan, not a failure after the confirmation.
func TestResolvedConflictPullIsCheckedBeforeTheReview(t *testing.T) {
	root := initRepository(t)
	require.NoError(t, os.WriteFile(filepath.Join(root, "tool.json"), []byte(`{"description":"Local text","schema":{"type":"object"}}`), 0o644))
	writeLocalFile(t, root, "tools/search.ref.yaml",
		"formatVersion: 1\nfile: tool.json\nformat: json\nselect: $\nmap:\n  description: description\n  schema: schema\n")
	baseline := toolAttachment(syncdomain.Tool{Key: "search", Description: pointer("Old text"), Schema: map[string]any{"type": "object"}}, 1)
	writeManifestResources(t, root)
	trackAttachment(t, root, baseline)
	linked, err := synclocal.CompileAttachments(root, func(syncdomain.ResourceID) bool { return true })
	require.NoError(t, err)
	recorded, err := syncdomain.SyncedAttachment{ProjectKey: "production", Attachment: baseline, Source: linked[0].Source}.Fingerprint()
	require.NoError(t, err)
	lock := readLock(t, root)
	lock.SetFingerprint(linked[0].ID(), recorded)
	require.NoError(t, lock.SetSource(linked[0].ID(), linked[0].Source, "json/1", linked[0].IndexedSelections))
	require.NoError(t, syncmanifest.WriteLock(synclocal.NewStore(root), lock))
	// LaunchDarkly dropped the description, so both sides changed.
	api := &directAPI{tools: map[string]versionedTool{"search": {
		Tool: syncdomain.Tool{Key: "search", Schema: map[string]any{"type": "object"}}, Version: 2,
	}}}

	resolution, err := syncprompt.ParseConflictResolution("launchdarkly")
	require.NoError(t, err)
	stdout, _, err := runScoped(t, root, api, syncprompt.Options{
		Scope: syncdomain.ScopeTools, Action: syncprompt.SyncAction{Filter: syncdomain.Filter{Project: "production"}},
		Yes: true, NoInput: true, ConflictPolicy: syncprompt.ConflictPolicy{Default: resolution}, OutputKind: "json",
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), `mapped field "description"`, "the review reports the pull that cannot clear the field")
	assert.NotContains(t, stdout, `"status": "failed"`, "nothing was applied")
	data, readErr := os.ReadFile(filepath.Join(root, "tool.json"))
	require.NoError(t, readErr)
	assert.Contains(t, string(data), "Local text", "the source is unchanged")
}

// A variation conflict that the user resolves to the local side becomes a
// push. A linked tool that the push would publish still follows its own sync
// setting, so it gets a row of its own in the conflicted plan.
func TestConflictResolvedToLocalRespectsTheSettingOfALinkedTool(t *testing.T) {
	root := initRepository(t)
	require.NoError(t, os.WriteFile(filepath.Join(root, "tool.json"), []byte(`{"description":"Local","schema":{"type":"object"}}`), 0o644))
	writeLocalFile(t, root, "tools/search.ref.yaml",
		"formatVersion: 1\nfile: tool.json\nformat: json\nselect: $\nmap:\n  description: description\n  schema: schema\n")
	local := variation("Local name")
	local.Tools = []syncdomain.AttachmentRef{{Key: "search"}}
	local.Attachments = []syncdomain.Attachment{{Kind: syncdomain.AttachmentTool, Tool: &syncdomain.Tool{
		Key: "search", Description: pointer("Local"), Schema: map[string]any{"type": "object"},
	}}}
	writeVariation(t, root, local, true)
	baselineTool := syncdomain.Tool{Key: "search", Description: pointer("Baseline"), Schema: map[string]any{"type": "object"}}
	baseline := variation("Baseline name")
	baseline.Tools = []syncdomain.AttachmentRef{{Key: "search", Version: 1}}
	baseline.Attachments = []syncdomain.Attachment{toolAttachment(baselineTool, 1)}
	writeManifest(t, root, baseline)
	linked, err := synclocal.CompileAttachments(root, func(syncdomain.ResourceID) bool { return true })
	require.NoError(t, err)
	recorded, err := syncdomain.SyncedAttachment{ProjectKey: "production", Attachment: toolAttachment(baselineTool, 1), Source: linked[0].Source}.Fingerprint()
	require.NoError(t, err)
	lock := readLock(t, root)
	lock.SetFingerprint(linked[0].ID(), recorded)
	require.NoError(t, lock.SetSource(linked[0].ID(), linked[0].Source, "json/1", linked[0].IndexedSelections))
	require.NoError(t, syncmanifest.WriteLock(synclocal.NewStore(root), lock))
	// Both sides changed the variation name, and the tool changed only locally.
	serverTool := baselineTool
	server := variation("Server name")
	server.Tools = []syncdomain.AttachmentRef{{Key: "search", Version: 1}}
	server.Attachments = []syncdomain.Attachment{toolAttachment(serverTool, 1)}
	api := &directAPI{
		variation:  &server,
		tools:      map[string]versionedTool{"search": {Tool: serverTool, Version: 1}},
		directions: map[string]syncdomain.SyncDirection{"tool:search": syncdomain.SyncDirectionServerCanonical},
	}

	stdout, _, err := runPrompt(t, root, api, "--dry-run", "--output", "json")
	require.Error(t, err, "a dry run reports the conflict as blocking")
	assert.Equal(t, []string{"tool search update_local", "variation support/default conflict"}, planRows(t, stdout), "the linked tool has a row in the conflicted plan")

	_, _, err = runPrompt(t, root, api, "--conflict", "local", "--yes", "--no-input")

	require.NoError(t, err)
	assert.Equal(t, "Baseline", *api.tools["search"].Description, "the server-canonical tool is not published by the push")
	assert.Equal(t, "Local name", api.variation.Name, "the variation push wins the conflict")
}

// A pull over a linked tool that changed locally becomes a conflict in the
// baseline checks. The linked tool still gets its row, so that a choice of
// the local side cannot publish a server-canonical tool.
func TestConflictFromABaselineCheckStillPlansTheLinkedToolRow(t *testing.T) {
	root := initRepository(t)
	require.NoError(t, os.WriteFile(filepath.Join(root, "tool.json"), []byte(`{"description":"Local","schema":{"type":"object"}}`), 0o644))
	writeLocalFile(t, root, "tools/search.ref.yaml",
		"formatVersion: 1\nfile: tool.json\nformat: json\nselect: $\nmap:\n  description: description\n  schema: schema\n")
	local := variation("Baseline name")
	local.Tools = []syncdomain.AttachmentRef{{Key: "search"}}
	local.Attachments = []syncdomain.Attachment{{Kind: syncdomain.AttachmentTool, Tool: &syncdomain.Tool{
		Key: "search", Description: pointer("Local"), Schema: map[string]any{"type": "object"},
	}}}
	writeVariation(t, root, local, true)
	baselineTool := syncdomain.Tool{Key: "search", Description: pointer("Baseline"), Schema: map[string]any{"type": "object"}}
	baseline := variation("Baseline name")
	baseline.Tools = []syncdomain.AttachmentRef{{Key: "search", Version: 1}}
	baseline.Attachments = []syncdomain.Attachment{toolAttachment(baselineTool, 1)}
	writeManifest(t, root, baseline)
	linked, err := synclocal.CompileAttachments(root, func(syncdomain.ResourceID) bool { return true })
	require.NoError(t, err)
	recorded, err := syncdomain.SyncedAttachment{ProjectKey: "production", Attachment: toolAttachment(baselineTool, 1), Source: linked[0].Source}.Fingerprint()
	require.NoError(t, err)
	lock := readLock(t, root)
	lock.SetFingerprint(linked[0].ID(), recorded)
	require.NoError(t, lock.SetSource(linked[0].ID(), linked[0].Source, "json/1", linked[0].IndexedSelections))
	require.NoError(t, syncmanifest.WriteLock(synclocal.NewStore(root), lock))
	// Only LaunchDarkly changed the variation, so the row is a pull until
	// the baseline check sees the local change to the tool.
	server := variation("Server name")
	server.Tools = []syncdomain.AttachmentRef{{Key: "search", Version: 1}}
	server.Attachments = []syncdomain.Attachment{toolAttachment(baselineTool, 1)}
	api := &directAPI{
		variation:  &server,
		tools:      map[string]versionedTool{"search": {Tool: baselineTool, Version: 1}},
		directions: map[string]syncdomain.SyncDirection{"tool:search": syncdomain.SyncDirectionServerCanonical},
	}

	stdout, _, err := runPrompt(t, root, api, "--dry-run", "--output", "json")
	require.Error(t, err, "a dry run reports the conflict as blocking")
	assert.Equal(t, []string{"tool search update_local", "variation support/default conflict"}, planRows(t, stdout), "the linked tool has a row in the conflicted plan")

	_, _, err = runPrompt(t, root, api, "--conflict", "local", "--yes", "--no-input")

	require.NoError(t, err)
	assert.Equal(t, "Baseline", *api.tools["search"].Description, "the server-canonical tool is not published by the push")
	assert.Equal(t, "Baseline name", api.variation.Name, "the variation push wins the conflict")
}

// A code-canonical setting turns a planned pull of a linked variation into a
// push. The linked tool that the push would publish still gets its row, so
// its own setting decides.
func TestSettingFlipToPushStillPlansTheLinkedToolRow(t *testing.T) {
	root := initRepository(t)
	require.NoError(t, os.WriteFile(filepath.Join(root, "agents.py"), []byte("from agents import Agent\nsupport = Agent(instructions=\"Local text\")\n"), 0o644))
	writeLocalFile(t, root, "configs/support/default.ref.yaml",
		"formatVersion: 1\nfile: agents.py\nformat: python\nselect: 'agents[id=\"support\"]'\nmap:\n  prompt: instructions\ntools:\n  - key: search\n")
	require.NoError(t, os.WriteFile(filepath.Join(root, "tool.json"), []byte(`{"description":"Local tool","schema":{"type":"object"}}`), 0o644))
	writeLocalFile(t, root, "tools/search.ref.yaml",
		"formatVersion: 1\nfile: tool.json\nformat: json\nselect: $\nmap:\n  description: description\n  schema: schema\n")
	baselineTool := syncdomain.Tool{Key: "search", Description: pointer("Baseline tool"), Schema: map[string]any{"type": "object"}}
	compiled, err := synclocal.CompileWorkspace(root)
	require.NoError(t, err)
	require.Len(t, compiled, 1)
	linked, err := synclocal.CompileAttachments(root, func(syncdomain.ResourceID) bool { return true })
	require.NoError(t, err)
	// The last sync left the variation at "Local text" and the tool at its baseline.
	server := variation("Server")
	server.Instructions = "Local text"
	server.Tools = []syncdomain.AttachmentRef{{Key: "search", Version: 1}}
	server.Attachments = []syncdomain.Attachment{toolAttachment(baselineTool, 1)}
	writeManifestResources(t, root)
	lock := readLock(t, root)
	projection, err := syncdomain.ProjectVariation(server, *compiled[0].Source)
	require.NoError(t, err)
	variationFingerprint, err := syncdomain.FingerprintProjection(compiled[0].ID(), projection)
	require.NoError(t, err)
	lock.SetFingerprint(compiled[0].ID(), variationFingerprint)
	require.NoError(t, lock.SetSource(compiled[0].ID(), compiled[0].Source, "python/1", compiled[0].IndexedSelections))
	toolFingerprint, err := syncdomain.SyncedAttachment{ProjectKey: "production", Attachment: toolAttachment(baselineTool, 1), Source: linked[0].Source}.Fingerprint()
	require.NoError(t, err)
	lock.SetFingerprint(linked[0].ID(), toolFingerprint)
	require.NoError(t, lock.SetSource(linked[0].ID(), linked[0].Source, "json/1", linked[0].IndexedSelections))
	require.NoError(t, syncmanifest.WriteLock(synclocal.NewStore(root), lock))
	// LaunchDarkly changed the instructions, so the plan is a pull until the
	// code-canonical setting turns it into a push.
	server.Instructions = "Server text"
	api := &directAPI{
		variation: &server,
		tools:     map[string]versionedTool{"search": {Tool: baselineTool, Version: 1}},
		directions: map[string]syncdomain.SyncDirection{
			"variation:support/default": syncdomain.SyncDirectionCodeCanonical,
			"tool:search":               syncdomain.SyncDirectionServerCanonical,
		},
	}

	stdout, _, err := runPrompt(t, root, api, "--dry-run", "--output", "json")
	require.NoError(t, err)
	assert.Equal(t, []string{"tool search update_local", "variation support/default update_server"}, planRows(t, stdout),
		"the flipped push plans a row for its linked tool")

	_, _, err = runPrompt(t, root, api, "--yes")

	require.NoError(t, err)
	assert.Equal(t, "Baseline tool", *api.tools["search"].Description, "the server-canonical tool is not published")
	assert.Equal(t, "Local text", api.variation.Instructions, "the code-canonical variation is pushed")
}
