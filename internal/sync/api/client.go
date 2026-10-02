package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"

	"github.com/launchdarkly/ldcli/internal/resources"
	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
)

// Client reads and mutates prompt variations through the existing config endpoints.
type Client struct {
	transport   resources.Client
	accessToken string
	baseURI     string
}

// VariationState includes the parent config mode even when the requested
// variation does not exist.
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
	Messages           []syncdomain.Message       `json:"messages,omitempty"`
	Tools              []syncdomain.AttachmentRef `json:"tools,omitempty"`
	Skills             []syncdomain.AttachmentRef `json:"skills,omitempty"`
}

type updateVariationRequest struct {
	Name               string                      `json:"name"`
	Instructions       *string                     `json:"instructions,omitempty"`
	ModelConfigKey     string                      `json:"modelConfigKey"`
	ModelConfigVersion int                         `json:"modelConfigVersion,omitempty"`
	Model              map[string]any              `json:"model"`
	Messages           *[]syncdomain.Message       `json:"messages,omitempty"`
	Tools              *[]syncdomain.AttachmentRef `json:"tools,omitempty"`
	Skills             *[]syncdomain.AttachmentRef `json:"skills,omitempty"`
}

type mutationError struct {
	err       error
	message   string
	uncertain bool
}

// Error returns the contextual mutation failure shown to the user.
func (err mutationError) Error() string {
	return err.message
}

// Unwrap exposes the transport or API error for errors.Is and errors.As.
func (err mutationError) Unwrap() error {
	return err.err
}

// MutationMayHaveSucceeded reports whether a transport failure prevented the
// client from receiving a definitive API response.
func MutationMayHaveSucceeded(err error) bool {
	var mutation mutationError
	return errors.As(err, &mutation) && mutation.uncertain
}

// NewClient creates a direct config variation client.
func NewClient(transport resources.Client, accessToken, baseURI string) Client {
	return Client{
		transport:   transport,
		accessToken: accessToken,
		baseURI:     baseURI,
	}
}

// ModelConfig returns the latest version of one model config.
func (client Client) ModelConfig(projectKey, modelConfigKey string) (ModelConfig, error) {
	endpoint, err := url.JoinPath(client.baseURI, "api/v2/projects", projectKey, "ai-configs/model-configs", modelConfigKey)
	if err != nil {
		return ModelConfig{}, fmt.Errorf("build model config endpoint: %w", err)
	}

	response, err := client.transport.MakeRequest(client.accessToken, http.MethodGet, endpoint, "", nil, nil, false)
	if err != nil {
		return ModelConfig{}, fmt.Errorf("get model config %q: %w", modelConfigKey, err)
	}

	var modelConfig ModelConfig
	if err := json.Unmarshal(response, &modelConfig); err != nil {
		return ModelConfig{}, fmt.Errorf("decode model config response: %w", err)
	}
	return modelConfig, nil
}

// ReadVariation returns one variation and its parent config mode.
func (client Client) ReadVariation(projectKey, configKey, variationKey string) (VariationState, error) {
	config, err := client.Config(projectKey, configKey)
	if err != nil {
		return VariationState{}, err
	}

	for _, variation := range config.Variations {
		if variation.Key != variationKey {
			continue
		}
		if err := syncdomain.ValidateDirectAPIVariation(variation); err != nil {
			return VariationState{}, fmt.Errorf(
				"read config variation %q: %w",
				variationKey,
				err,
			)
		}
		return VariationState{Variation: variation, Exists: true, ConfigMode: config.Mode}, nil
	}

	return VariationState{ConfigMode: config.Mode}, nil
}

// CreateVariation creates a variation using only fields supported by the
// existing public endpoint.
func (client Client) CreateVariation(projectKey, configKey string, variation syncdomain.Variation) error {
	if err := syncdomain.ValidateDirectAPIVariation(variation); err != nil {
		return fmt.Errorf("create config variation %q: %w", variation.Key, err)
	}

	request := createVariationRequest{
		Key:                variation.Key,
		Name:               variation.Name,
		ModelConfigKey:     variation.ModelConfigKey,
		ModelConfigVersion: variation.ModelConfigVersion,
		Model:              variation.Model,
		Tools:              variation.Tools,
		Skills:             variation.Skills,
	}
	if variation.Mode == syncdomain.VariationModeAgent {
		request.Instructions = variation.Instructions
	} else {
		request.Messages = variation.Messages
	}

	body, err := json.Marshal(request)
	if err != nil {
		return fmt.Errorf("encode config variation %q: %w", variation.Key, err)
	}

	endpoint, err := client.variationEndpoint(projectKey, configKey)
	if err != nil {
		return err
	}

	_, err = client.transport.MakeRequest(
		client.accessToken,
		http.MethodPost,
		endpoint,
		"application/json",
		nil,
		body,
		false,
	)
	if err != nil {
		return newMutationError("create", variation.Key, err)
	}

	return nil
}

// UpdateVariation replaces all supported, locally owned variation fields.
func (client Client) UpdateVariation(projectKey, configKey string, variation syncdomain.Variation) error {
	if err := syncdomain.ValidateDirectAPIVariation(variation); err != nil {
		return fmt.Errorf("update config variation %q: %w", variation.Key, err)
	}

	model := variation.Model
	if model == nil {
		model = map[string]any{}
	}
	request := updateVariationRequest{
		Name:               variation.Name,
		ModelConfigKey:     variation.ModelConfigKey,
		ModelConfigVersion: variation.ModelConfigVersion,
		Model:              model,
	}
	if variation.Tools != nil {
		request.Tools = &variation.Tools
	}
	if variation.Skills != nil {
		request.Skills = &variation.Skills
	}

	// Agent and completion configs reject fields owned by the other mode, even
	// when those fields are empty. Include only the prompt field for this mode.
	if variation.Mode == syncdomain.VariationModeAgent {
		request.Instructions = &variation.Instructions
	} else {
		messages := variation.Messages
		if messages == nil {
			messages = []syncdomain.Message{}
		}
		request.Messages = &messages
	}

	body, err := json.Marshal(request)
	if err != nil {
		return fmt.Errorf("encode config variation %q: %w", variation.Key, err)
	}

	endpoint, err := client.variationEndpoint(projectKey, configKey, variation.Key)
	if err != nil {
		return err
	}

	_, err = client.transport.MakeRequest(
		client.accessToken,
		http.MethodPatch,
		endpoint,
		"application/json",
		nil,
		body,
		false,
	)
	if err != nil {
		return newMutationError("update", variation.Key, err)
	}

	return nil
}

// ArchiveVariation archives one variation without permanently deleting it.
func (client Client) ArchiveVariation(projectKey, configKey, variationKey string) error {
	endpoint, err := client.variationEndpoint(projectKey, configKey, variationKey)
	if err != nil {
		return err
	}

	_, err = client.transport.MakeRequest(
		client.accessToken,
		http.MethodPatch,
		endpoint,
		"application/json",
		nil,
		[]byte(`{"state":"archived"}`),
		false,
	)
	if err != nil {
		return newMutationError("archive", variationKey, err)
	}

	return nil
}

// variationEndpoint builds an escaped direct-variation API URL.
func (client Client) variationEndpoint(projectKey, configKey string, path ...string) (string, error) {
	parts := []string{
		"api/v2/projects",
		projectKey,
		"ai-configs",
		configKey,
		"variations",
	}
	parts = append(parts, path...)

	endpoint, err := url.JoinPath(client.baseURI, parts...)
	if err != nil {
		return "", fmt.Errorf("build config variation endpoint: %w", err)
	}
	return endpoint, nil
}

// newMutationError records whether a failed request received a definitive API
// response. Errors without a status code may represent a committed write whose
// response was lost, so the reconciliation layer verifies those with a read.
func newMutationError(action, variationKey string, err error) error {
	return newResourceMutationError(action, "config variation", variationKey, err)
}

func newResourceMutationError(action, resource, key string, err error) error {
	_, definitiveResponse := responseStatusCode(err)

	return mutationError{
		err:       err,
		message:   fmt.Sprintf("%s %s %q: %s", action, resource, key, err),
		uncertain: !definitiveResponse,
	}
}

func responseStatusCode(err error) (int, bool) {
	for current := err; current != nil; current = errors.Unwrap(current) {
		var response struct {
			StatusCode int `json:"statusCode"`
		}
		if json.Unmarshal([]byte(current.Error()), &response) == nil && response.StatusCode != 0 {
			return response.StatusCode, true
		}
	}
	return 0, false
}
