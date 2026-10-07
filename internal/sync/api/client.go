package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	lderrors "github.com/launchdarkly/ldcli/internal/errors"
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

// SyncManifest is the complete synchronization baseline for one project and source.
type SyncManifest struct {
	Source string                 `json:"source"`
	Items  []SyncManifestResource `json:"items"`
}

// SyncManifestResource is one versioned baseline entry returned by LaunchDarkly.
type SyncManifestResource struct {
	ResourceKind      syncdomain.Kind `json:"resourceKind"`
	ResourceLookupKey string          `json:"resourceLookupKey"`
	Fingerprint       string          `json:"fingerprint"`
	Version           int             `json:"version"`
}

// SyncManifestUpsert creates or updates one baseline entry.
type SyncManifestUpsert struct {
	ResourceKind      syncdomain.Kind `json:"resourceKind"`
	ResourceLookupKey string          `json:"resourceLookupKey"`
	Fingerprint       string          `json:"fingerprint"`
	Version           int             `json:"version"`
}

// SyncManifestDeletion removes one baseline entry at its expected version.
type SyncManifestDeletion struct {
	ResourceKind      syncdomain.Kind `json:"resourceKind"`
	ResourceLookupKey string          `json:"resourceLookupKey"`
	Version           int             `json:"version"`
}

type patchSyncManifestRequest struct {
	Source    string                 `json:"source"`
	Upserts   []SyncManifestUpsert   `json:"upserts"`
	Deletions []SyncManifestDeletion `json:"deletions"`
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

type updateVariationRequest struct {
	Name               string                      `json:"name"`
	Instructions       *string                     `json:"instructions,omitempty"`
	ModelConfigKey     string                      `json:"modelConfigKey"`
	ModelConfigVersion int                         `json:"modelConfigVersion,omitempty"`
	Model              map[string]any              `json:"model"`
	OutputFormat       map[string]any              `json:"outputFormat"`
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

// GetSyncManifest returns the synchronization baseline for one project and source.
func (client Client) GetSyncManifest(projectKey, source string) (SyncManifest, error) {
	endpoint, err := client.syncManifestEndpoint(projectKey)
	if err != nil {
		return SyncManifest{}, err
	}

	response, err := client.transport.MakeRequest(
		client.accessToken,
		http.MethodGet,
		endpoint,
		"",
		url.Values{"source": []string{source}},
		nil,
		false,
	)
	if err != nil {
		return SyncManifest{}, contextualAPIError(
			err,
			fmt.Sprintf("get sync manifest for project %q", projectKey),
			projectKey,
		)
	}

	var manifest SyncManifest
	if err := json.Unmarshal(response, &manifest); err != nil {
		return SyncManifest{}, fmt.Errorf("decode sync manifest for project %q: %w", projectKey, err)
	}
	return manifest, nil
}

// PatchSyncManifest applies versioned baseline changes and returns the refreshed manifest.
func (client Client) PatchSyncManifest(
	projectKey string,
	source string,
	upserts []SyncManifestUpsert,
	deletions []SyncManifestDeletion,
) (SyncManifest, error) {
	endpoint, err := client.syncManifestEndpoint(projectKey)
	if err != nil {
		return SyncManifest{}, err
	}
	if upserts == nil {
		upserts = []SyncManifestUpsert{}
	}
	if deletions == nil {
		deletions = []SyncManifestDeletion{}
	}
	body, err := json.Marshal(patchSyncManifestRequest{
		Source: source, Upserts: upserts, Deletions: deletions,
	})
	if err != nil {
		return SyncManifest{}, fmt.Errorf("encode sync manifest changes for project %q: %w", projectKey, err)
	}

	response, err := client.transport.MakeRequest(
		client.accessToken,
		http.MethodPatch,
		endpoint,
		"application/json",
		nil,
		body,
		false,
	)
	if err != nil {
		return SyncManifest{}, newResourceMutationError("update", "sync manifest for project", projectKey, err)
	}

	var manifest SyncManifest
	if err := json.Unmarshal(response, &manifest); err != nil {
		return SyncManifest{}, newResourceMutationError(
			"decode updated",
			"sync manifest for project",
			projectKey,
			err,
		)
	}
	return manifest, nil
}

// ModelConfig returns the latest version of one model config.
func (client Client) ModelConfig(projectKey, modelConfigKey string) (ModelConfig, error) {
	endpoint, err := url.JoinPath(client.baseURI, "api/v2/projects", projectKey, "ai-configs/model-configs", modelConfigKey)
	if err != nil {
		return ModelConfig{}, fmt.Errorf("build model config endpoint: %w", err)
	}

	response, err := client.transport.MakeRequest(client.accessToken, http.MethodGet, endpoint, "", nil, nil, false)
	if err != nil {
		return ModelConfig{}, contextualAPIError(
			err,
			fmt.Sprintf("get model config %q in project %q", modelConfigKey, projectKey),
			projectKey,
		)
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
		OutputFormat:       variation.OutputFormat,
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
	outputFormat := variation.OutputFormat
	if outputFormat == nil {
		// The API treats an omitted or null outputFormat as unchanged. Send an
		// empty object when the local field is absent so removing it also syncs.
		outputFormat = map[string]any{}
	}
	request := updateVariationRequest{
		Name:               variation.Name,
		ModelConfigKey:     variation.ModelConfigKey,
		ModelConfigVersion: variation.ModelConfigVersion,
		Model:              model,
		OutputFormat:       outputFormat,
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

func (client Client) syncManifestEndpoint(projectKey string) (string, error) {
	endpoint, err := url.JoinPath(
		client.baseURI,
		"api/v2/projects",
		projectKey,
		"configs/sync/manifests",
	)
	if err != nil {
		return "", fmt.Errorf("build sync manifest endpoint: %w", err)
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
	contextual := contextualAPIError(
		err,
		fmt.Sprintf("%s %s %q", action, resource, key),
		"",
	)

	return mutationError{
		err:       contextual,
		message:   contextual.Error(),
		uncertain: !definitiveResponse,
	}
}

func responseStatusCode(err error) (int, bool) {
	response, ok := responseError(err)
	if !ok {
		return 0, false
	}
	status, ok := response["statusCode"].(float64)
	return int(status), ok && status != 0
}

// contextualAPIError keeps the API's structured status fields while adding
// the resource identity needed to act on the failure.
func contextualAPIError(err error, context, projectKey string) error {
	response, ok := responseError(err)
	if !ok {
		return fmt.Errorf("%s: %w", context, err)
	}
	status, _ := response["statusCode"].(float64)
	if int(status) == http.StatusNotFound && projectKey != "" {
		response["message"] = context
		response["suggestion"] = fmt.Sprintf(
			"Verify the resource key and that it belongs to project %q.",
			projectKey,
		)
	} else if message, _ := response["message"].(string); message != "" {
		response["message"] = context + ": " + strings.ReplaceAll(message, "AI config", "config")
	} else {
		response["message"] = context
	}
	body, _ := json.Marshal(response)
	return lderrors.NewErrorWrapped(string(body), err)
}

func responseError(err error) (map[string]any, bool) {
	for current := err; current != nil; current = errors.Unwrap(current) {
		var response map[string]any
		if json.Unmarshal([]byte(current.Error()), &response) == nil && len(response) != 0 {
			return response, true
		}
	}
	return nil, false
}

// IsConflict reports whether a wrapped LaunchDarkly API error has a 409 status.
func IsConflict(err error) bool {
	status, ok := responseStatusCode(err)
	return ok && status == http.StatusConflict
}
