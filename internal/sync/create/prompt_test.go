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
