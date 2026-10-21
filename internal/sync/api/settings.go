package api

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
)

const maxSettingsResources = 100

// ErrSyncSettingsUnavailable means that the sync settings API of a project returned 404.
var ErrSyncSettingsUnavailable = errors.New("sync settings are not available")

// SettingsUnavailableWarning returns the warning for a project without sync settings.
func SettingsUnavailableWarning(projectKey, effect string) string {
	return fmt.Sprintf("Warning: sync settings are not available for project %s. %s\n", projectKey, effect)
}

func settingsUnavailable(err error) bool {
	status, ok := responseStatusCode(err)
	return ok && status == http.StatusNotFound
}

type syncSetting struct {
	ResourceKind      syncdomain.Kind `json:"resourceKind"`
	ResourceLookupKey string          `json:"resourceLookupKey"`
	Direction         string          `json:"direction"`
}

// ResolveSourceOfTruth returns the source of truth of each resource in one project.
func (client Client) ResolveSourceOfTruth(
	projectKey string,
	ids []syncdomain.ResourceID,
) (map[syncdomain.ResourceID]syncdomain.SourceOfTruth, error) {
	sources := make(map[syncdomain.ResourceID]syncdomain.SourceOfTruth, len(ids))
	for start := 0; start < len(ids); start += maxSettingsResources {
		batch := ids[start:min(start+maxSettingsResources, len(ids))]
		if err := client.resolveSourceOfTruthBatch(projectKey, batch, sources); err != nil {
			return nil, err
		}
	}
	return sources, nil
}

func (client Client) resolveSourceOfTruthBatch(
	projectKey string,
	ids []syncdomain.ResourceID,
	sources map[syncdomain.ResourceID]syncdomain.SourceOfTruth,
) error {
	query := url.Values{}
	for _, id := range ids {
		query.Add("resources", string(id.Kind)+":"+id.LookupKey)
		sources[id] = syncdomain.SourceOfTruthUIAndCode
	}

	description := fmt.Sprintf("get sync settings for project %q", projectKey)
	endpoint, err := client.endpoint(settingsPath(projectKey)...)
	if err != nil {
		return err
	}
	response, err := client.do(http.MethodGet, endpoint, query, nil)
	if err != nil {
		if settingsUnavailable(err) {
			return fmt.Errorf("%s: %w", description, ErrSyncSettingsUnavailable)
		}
		return contextualAPIError(err, description, projectKey)
	}

	settings, err := decodeJSON[struct {
		Items []syncSetting `json:"items"`
	}](response, fmt.Sprintf("sync settings for project %q", projectKey))
	if err != nil {
		return err
	}
	for _, item := range settings.Items {
		id := syncdomain.ResourceID{Kind: item.ResourceKind, ProjectKey: projectKey, LookupKey: item.ResourceLookupKey}
		if _, requested := sources[id]; !requested {
			continue
		}
		switch item.Direction {
		case directionCodeCanonical:
			sources[id] = syncdomain.SourceOfTruthCodeOnly
		// The server still accepts server_canonical rules. ldcli syncs them as ui-and-code.
		case directionBoth, "server_canonical":
		default:
			return fmt.Errorf("%s: resource %s: unsupported sync direction %q", description, id, item.Direction)
		}
	}
	return nil
}

const (
	directionCodeCanonical = "code_canonical"
	directionBoth          = "both"
)

func wireDirection(source syncdomain.SourceOfTruth) string {
	if source == syncdomain.SourceOfTruthCodeOnly {
		return directionCodeCanonical
	}
	return directionBoth
}

func settingsPath(projectKey string) []string {
	return projectPath(projectKey, "configs/sync/settings")
}

// SettingsWriter writes the source of truth rules of variations.
type SettingsWriter interface {
	SetSourceOfTruth(projectKey string, ids []syncdomain.ResourceID, source syncdomain.SourceOfTruth) error
	DeleteSourceOfTruth(projectKey string, ids []syncdomain.ResourceID) error
}

type settingRule struct {
	ResourceKind    syncdomain.Kind `json:"resourceKind"`
	ResourcePattern string          `json:"resourcePattern"`
	Direction       string          `json:"direction,omitempty"`
}

// SetSourceOfTruth writes an exact rule with source for each variation of one project.
func (client Client) SetSourceOfTruth(projectKey string, ids []syncdomain.ResourceID, source syncdomain.SourceOfTruth) error {
	return client.patchSettings(projectKey, ids, wireDirection(source))
}

// DeleteSourceOfTruth deletes the exact rule of each variation of one project, if the rule exists.
func (client Client) DeleteSourceOfTruth(projectKey string, ids []syncdomain.ResourceID) error {
	return client.patchSettings(projectKey, ids, "")
}

// patchSettings upserts an exact rule for each resource, or deletes it when direction is empty.
func (client Client) patchSettings(projectKey string, ids []syncdomain.ResourceID, direction string) error {
	for start := 0; start < len(ids); start += maxSettingsResources {
		body := struct {
			Upserts   []settingRule `json:"upserts"`
			Deletions []settingRule `json:"deletions"`
		}{Upserts: []settingRule{}, Deletions: []settingRule{}}
		for _, id := range ids[start:min(start+maxSettingsResources, len(ids))] {
			rule := settingRule{ResourceKind: id.Kind, ResourcePattern: id.LookupKey, Direction: direction}
			if direction == "" {
				body.Deletions = append(body.Deletions, rule)
			} else {
				body.Upserts = append(body.Upserts, rule)
			}
		}
		_, err := client.mutate(mutation{
			method: http.MethodPatch, action: "update", resource: "sync settings for project", key: projectKey, body: body,
		}, settingsPath(projectKey)...)
		if settingsUnavailable(err) {
			return fmt.Errorf("update sync settings for project %q: %w", projectKey, ErrSyncSettingsUnavailable)
		}
		if err != nil {
			return err
		}
	}
	return nil
}
