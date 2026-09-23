package api

import (
	"encoding/json"
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
	Key                string               `json:"key"`
	Name               string               `json:"name"`
	Instructions       string               `json:"instructions,omitempty"`
	ModelConfigKey     string               `json:"modelConfigKey,omitempty"`
	ModelConfigVersion int                  `json:"modelConfigVersion,omitempty"`
	Model              map[string]any       `json:"model,omitempty"`
	Messages           []syncdomain.Message `json:"messages,omitempty"`
}

type updateVariationRequest struct {
	Name               string                `json:"name"`
	Instructions       *string               `json:"instructions,omitempty"`
	ModelConfigKey     string                `json:"modelConfigKey"`
	ModelConfigVersion int                   `json:"modelConfigVersion,omitempty"`
	Model              map[string]any        `json:"model"`
	Messages           *[]syncdomain.Message `json:"messages,omitempty"`
}

// NewClient creates a direct config variation client.
func NewClient(transport resources.Client, accessToken, baseURI string) Client {
	return Client{
		transport:   transport,
		accessToken: accessToken,
		baseURI:     baseURI,
	}
}

// ReadVariation returns one variation and its parent config mode.
func (client Client) ReadVariation(projectKey, configKey, variationKey string) (VariationState, error) {
	config, err := NewCatalogClient(client.transport, client.accessToken, client.baseURI).Config(projectKey, configKey)
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

	body, err := json.Marshal(createVariationRequest{
		Key:                variation.Key,
		Name:               variation.Name,
		Instructions:       variation.Instructions,
		ModelConfigKey:     variation.ModelConfigKey,
		ModelConfigVersion: variation.ModelConfigVersion,
		Model:              variation.Model,
		Messages:           variation.Messages,
	})
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
		return fmt.Errorf("create config variation %q: %w", variation.Key, err)
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
		return fmt.Errorf("update config variation %q: %w", variation.Key, err)
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
		return fmt.Errorf("archive config variation %q: %w", variationKey, err)
	}

	return nil
}

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
