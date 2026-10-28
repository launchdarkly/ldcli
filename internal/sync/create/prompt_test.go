package create

import (
	"bytes"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
	syncapi "github.com/launchdarkly/ldcli/internal/sync/api"
)

func TestFindModelConfigReturnsTheModelConfigWithTheKey(t *testing.T) {
	catalog := &fakeCatalog{modelConfigs: []syncapi.ModelConfig{{Key: "gpt", Version: 3}, {Key: "claude", Version: 2}}}

	modelConfig, err := FindModelConfig(catalog, "demo", "claude")
	require.NoError(t, err)
	assert.Equal(t, syncapi.ModelConfig{Key: "claude", Version: 2}, modelConfig)

	_, err = FindModelConfig(catalog, "demo", "llama")
	require.EqualError(t, err, `model config "llama" was not found`)
}

func TestSelectConfigSkipsTheProjectQuestionAndNamesAMissingProject(t *testing.T) {
	catalog := &fakeCatalog{}
	questions := Questions{Allowed: true, Input: &bytes.Buffer{}, Output: &bytes.Buffer{}}

	_, _, _, err := SelectConfig(questions, catalog, "demo", nil)

	require.EqualError(t, err, `project "demo" was not found in LaunchDarkly, or the access token cannot read it`)
	assert.Equal(t, []string{"demo"}, catalog.configSearches)
}

func TestVariationTargetWithoutQuestionsNamesEveryMissingFlag(t *testing.T) {
	_, _, err := Input{Content: pointer("Escalate.")}.VariationTarget(&fakeCatalog{}, syncdomain.Filter{}, Questions{})
	require.EqualError(t, err,
		"variation creation requires --project, --config-key, --key, and --model-config-key without interactive input")

	input := Input{Content: pointer("Escalate."), ModelConfigKey: pointer("claude")}
	filter := syncdomain.Filter{Project: "demo", ConfigKey: "support", Keys: []string{"escalation"}}
	id, canceled, err := input.VariationTarget(&fakeCatalog{}, filter, Questions{})
	require.NoError(t, err)
	assert.False(t, canceled)
	assert.Equal(t, syncdomain.VariationID("demo", "support", "escalation"), id)
}

func TestVariationReadsTheContentWithTheRulesOfAVariationFile(t *testing.T) {
	catalog := &fakeCatalog{modelConfigs: []syncapi.ModelConfig{{Key: "claude", ID: "claude-sonnet", Version: 3}}}
	id := syncdomain.VariationID("demo", "support", "incident-escalation")
	tests := map[string]struct {
		mode    syncdomain.VariationMode
		content string
		want    syncdomain.Variation
	}{
		"agent instructions": {
			mode: syncdomain.VariationModeAgent, content: "<user>Escalate.</user>\n",
			want: syncdomain.Variation{Instructions: "<user>Escalate.</user>"},
		},
		"completion role blocks": {
			mode: syncdomain.VariationModeCompletion, content: "<system>Be brief.</system><user>Escalate.</user>",
			want: syncdomain.Variation{Messages: []syncdomain.Message{
				{Role: syncdomain.RoleSystem, Content: "Be brief."}, {Role: "user", Content: "Escalate."},
			}},
		},
		"completion plain text": {
			mode: syncdomain.VariationModeCompletion, content: "Escalate.",
			want: syncdomain.Variation{Messages: []syncdomain.Message{{Role: syncdomain.RoleSystem, Content: "Escalate."}}},
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			input := Input{Content: &test.content, ModelConfigKey: pointer("claude")}

			variation, canceled, err := input.Variation(catalog, id, syncapi.Config{Key: "support", Mode: test.mode}, Questions{})

			require.NoError(t, err)
			assert.False(t, canceled)
			want := test.want
			want.Mode, want.Key, want.Name = test.mode, "incident-escalation", "Incident escalation"
			want.ModelConfigKey, want.ModelConfigVersion = "claude", 3
			want.Model = catalog.modelConfigs[0].VariationModel()
			assert.Equal(t, want, variation)
		})
	}
}

func TestMatchesVariationComparesOnlyThePassedFields(t *testing.T) {
	id := syncdomain.VariationID("demo", "support", "escalation")
	existing := syncdomain.Variation{
		Mode: syncdomain.VariationModeAgent, Key: "escalation", Name: "Escalation", Instructions: "Escalate.",
		ModelConfigKey: "claude", ModelConfigVersion: 2, Model: map[string]any{"modelName": "claude-sonnet"},
		Tools: []syncdomain.AttachmentRef{{Key: "search", Version: 4}},
	}
	tests := map[string]struct {
		input Input
		want  bool
	}{
		"prompt only":            {input: Input{Content: pointer("Escalate.\n")}, want: true},
		"same name and model":    {input: Input{Content: pointer("Escalate."), Name: pointer("Escalation"), ModelConfigKey: pointer("claude")}, want: true},
		"other prompt":           {input: Input{Content: pointer("Escalate now.")}},
		"other name":             {input: Input{Content: pointer("Escalate."), Name: pointer("Escalate")}},
		"other model config key": {input: Input{Content: pointer("Escalate."), ModelConfigKey: pointer("gpt")}},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			matches, err := test.input.MatchesVariation(id, existing)

			require.NoError(t, err)
			assert.Equal(t, test.want, matches)
		})
	}
}

// fakeCatalog has model configs. It fails a project search, and answers a
// config search with a 404, as LaunchDarkly does for a missing project.
type fakeCatalog struct {
	modelConfigs   []syncapi.ModelConfig
	configSearches []string
}

func (*fakeCatalog) SearchProjects(string, int, int) (syncapi.Page[syncapi.Project], error) {
	return syncapi.Page[syncapi.Project]{}, errors.New("asked for the project")
}

func (catalog *fakeCatalog) SearchConfigs(
	projectKey, _ string, _ []syncdomain.VariationMode, _, _ int,
) (syncapi.Page[syncapi.Config], error) {
	catalog.configSearches = append(catalog.configSearches, projectKey)
	return syncapi.Page[syncapi.Config]{}, errors.New(`{"code":"not_found","statusCode":404}`)
}

func (*fakeCatalog) Config(string, string) (syncapi.Config, error) {
	return syncapi.Config{}, errors.New("read the config")
}

func (catalog *fakeCatalog) ModelConfigs(string) ([]syncapi.ModelConfig, error) {
	return catalog.modelConfigs, nil
}
