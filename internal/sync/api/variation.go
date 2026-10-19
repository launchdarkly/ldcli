package api

import (
	"encoding/json"
	"net/http"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
)

const variationResource = "config variation"

// VariationState is one variation as LaunchDarkly stores it. ConfigMode is set
// even when the variation does not exist, because the parent config owns it.
type VariationState struct {
	Variation  syncdomain.Variation
	Exists     bool
	ConfigMode syncdomain.VariationMode
}

type createVariationRequest struct {
	Key                string                     `json:"key"`
	Name               string                     `json:"name"`
	Instructions       string                     `json:"instructions,omitempty"`
	ModelConfigKey     string                     `json:"modelConfigKey,omitempty"`
	ModelConfigVersion int                        `json:"modelConfigVersion,omitempty"`
	Model              map[string]any             `json:"model,omitempty"`
	OutputFormat       map[string]any             `json:"outputFormat,omitempty"`
	Messages           []syncdomain.Message       `json:"messages,omitempty"`
	Tools              []syncdomain.AttachmentRef `json:"tools,omitempty"`
	Skills             []syncdomain.AttachmentRef `json:"skills,omitempty"`
}

// updateVariationRequest sends every field that sync owns, so that a field
// that is empty in the local file is also cleared in LaunchDarkly. The model
// config key is the exception. The API rejects an empty key, so a variation
// without a model config leaves out the field.
type updateVariationRequest struct {
	Name               string                      `json:"name"`
	Instructions       *string                     `json:"instructions,omitempty"`
	ModelConfigKey     string                      `json:"modelConfigKey,omitempty"`
	ModelConfigVersion int                         `json:"modelConfigVersion,omitempty"`
	Model              map[string]any              `json:"model"`
	OutputFormat       map[string]any              `json:"outputFormat"`
	Messages           *[]syncdomain.Message       `json:"messages,omitempty"`
	Tools              *[]syncdomain.AttachmentRef `json:"tools,omitempty"`
	Skills             *[]syncdomain.AttachmentRef `json:"skills,omitempty"`
}

// ReadVariation returns one variation and the mode of its parent config.
func (client Client) ReadVariation(projectKey, configKey, variationKey string) (VariationState, error) {
	config, err := client.Config(projectKey, configKey)
	if err != nil {
		return VariationState{}, err
	}
	state := VariationState{ConfigMode: config.Mode}
	for _, variation := range config.Variations {
		if variation.Key == variationKey {
			state.Variation, state.Exists = variation, true
			break
		}
	}
	return state, nil
}

// CreateVariation creates a variation in a config. It returns the version
// that LaunchDarkly gives the new variation, or 0 when the response has no
// version.
func (client Client) CreateVariation(projectKey, configKey string, variation syncdomain.Variation) (int, error) {
	request := createVariationRequest{
		Key:                variation.Key,
		Name:               variation.Name,
		ModelConfigKey:     variation.ModelConfigKey,
		ModelConfigVersion: variation.ModelConfigVersion,
		Model:              variation.Model,
		OutputFormat:       variation.OutputFormat,
		Tools:              variation.Tools,
		Skills:             variation.Skills,
	}
	// An agent config rejects messages and a completion config rejects
	// instructions, so the request includes only the field for this mode.
	if variation.Mode == syncdomain.VariationModeAgent {
		request.Instructions = variation.Instructions
	} else {
		request.Messages = variation.Messages
	}

	response, err := client.mutate(mutation{
		method: http.MethodPost, action: "create", resource: variationResource, key: variation.Key, body: request,
	}, variationPath(projectKey, configKey)...)
	return writtenVersion(response), err
}

// UpdateVariation replaces every field of the variation that sync owns. It
// returns the new version of the variation, or 0 when the response has no
// version.
func (client Client) UpdateVariation(projectKey, configKey string, variation syncdomain.Variation) (int, error) {
	// The API keeps the current value when model or outputFormat is absent or
	// null. An empty object clears the value.
	request := updateVariationRequest{
		Name:               variation.Name,
		ModelConfigKey:     variation.ModelConfigKey,
		ModelConfigVersion: variation.ModelConfigVersion,
		Model:              emptyIfNil(variation.Model),
		OutputFormat:       emptyIfNil(variation.OutputFormat),
	}
	if variation.Tools != nil {
		request.Tools = &variation.Tools
	}
	if variation.Skills != nil {
		request.Skills = &variation.Skills
	}
	if variation.Mode == syncdomain.VariationModeAgent {
		request.Instructions = &variation.Instructions
	} else {
		messages := variation.Messages
		if messages == nil {
			messages = []syncdomain.Message{}
		}
		request.Messages = &messages
	}

	response, err := client.mutate(mutation{
		method: http.MethodPatch, action: "update", resource: variationResource, key: variation.Key, body: request,
	}, variationPath(projectKey, configKey, variation.Key)...)
	return writtenVersion(response), err
}

// writtenVersion returns the version in a variation write response. The write
// succeeded, so a response without a usable version returns 0 and no error.
func writtenVersion(response []byte) int {
	var written struct {
		Version int `json:"version"`
	}
	if len(response) == 0 || json.Unmarshal(response, &written) != nil || written.Version < 0 {
		return 0
	}
	return written.Version
}

// ArchiveVariation archives one variation. LaunchDarkly keeps an archived
// variation, but sync treats it as absent.
func (client Client) ArchiveVariation(projectKey, configKey, variationKey string) error {
	_, err := client.mutate(mutation{
		method: http.MethodPatch, action: "archive", resource: variationResource, key: variationKey,
		body: map[string]string{"state": "archived"},
	}, variationPath(projectKey, configKey, variationKey)...)
	return err
}

func variationPath(projectKey, configKey string, path ...string) []string {
	return projectPath(projectKey, append([]string{"ai-configs", configKey, "variations"}, path...)...)
}

func emptyIfNil(value map[string]any) map[string]any {
	if value == nil {
		return map[string]any{}
	}
	return value
}
