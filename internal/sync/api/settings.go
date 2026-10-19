package api

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
)

// maxSettingsResources is the largest number of resources that one sync
// settings request can ask for.
const maxSettingsResources = 100

// ErrSyncSettingsUnavailable means that the project has no sync settings API.
// For example, the plan of the account does not include the feature.
var ErrSyncSettingsUnavailable = errors.New("sync settings are not available")

// syncSetting is the direction that LaunchDarkly resolves for one resource.
type syncSetting struct {
	ResourceKind      syncdomain.Kind `json:"resourceKind"`
	ResourceLookupKey string          `json:"resourceLookupKey"`
	Direction         string          `json:"direction"`
}

// ResolveSyncSettings returns the sync direction of each resource in one
// project. A resource without a rule has DirectionBoth. If the API returns
// 403 or 404, the error wraps ErrSyncSettingsUnavailable.
func (client Client) ResolveSyncSettings(
	projectKey string,
	ids []syncdomain.ResourceID,
) (map[syncdomain.ResourceID]syncdomain.Direction, error) {
	directions := make(map[syncdomain.ResourceID]syncdomain.Direction, len(ids))
	for start := 0; start < len(ids); start += maxSettingsResources {
		batch := ids[start:min(start+maxSettingsResources, len(ids))]
		if err := client.resolveSyncSettingsBatch(projectKey, batch, directions); err != nil {
			return nil, err
		}
	}
	return directions, nil
}

// resolveSyncSettingsBatch sends one request and adds its directions to
// directions. A resource that the response does not include has
// DirectionBoth.
func (client Client) resolveSyncSettingsBatch(
	projectKey string,
	ids []syncdomain.ResourceID,
	directions map[syncdomain.ResourceID]syncdomain.Direction,
) error {
	query := url.Values{}
	for _, id := range ids {
		if id.ProjectKey != projectKey {
			return fmt.Errorf("resource %s is not in project %q", id, projectKey)
		}
		query.Add("resources", string(id.Kind)+":"+id.LookupKey)
		directions[id] = syncdomain.DirectionBoth
	}

	description := fmt.Sprintf("get sync settings for project %q", projectKey)
	endpoint, err := client.endpoint(settingsPath(projectKey)...)
	if err != nil {
		return err
	}
	response, err := client.transport.MakeRequest(client.accessToken, http.MethodGet, endpoint, "", query, nil, false)
	if err != nil {
		if status, ok := responseStatusCode(err); ok && (status == http.StatusForbidden || status == http.StatusNotFound) {
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
		if _, requested := directions[id]; !requested {
			continue
		}
		direction, err := syncdomain.ParseDirection(item.Direction)
		if err != nil {
			return fmt.Errorf("%s: resource %s: %w", description, id, err)
		}
		directions[id] = direction
	}
	return nil
}

func settingsPath(projectKey string) []string {
	return projectPath(projectKey, "configs/sync/settings")
}

// maxSettingsPatchItems is the largest number of upserts, and also of
// deletions, that one sync settings patch can send.
const maxSettingsPatchItems = 100

// SyncSettingRule is one explicit direction rule. ResourcePattern is a lookup
// key, for example "support-bot/default", or a pattern such as
// "support-bot/*".
type SyncSettingRule struct {
	ResourceKind    syncdomain.Kind      `json:"resourceKind"`
	ResourcePattern string               `json:"resourcePattern"`
	Direction       syncdomain.Direction `json:"direction"`
}

// SyncSettingPattern names one explicit direction rule to delete.
type SyncSettingPattern struct {
	ResourceKind    syncdomain.Kind `json:"resourceKind"`
	ResourcePattern string          `json:"resourcePattern"`
}

// PatchSyncSettings creates or replaces each rule in upserts, and deletes each
// rule in deletions. A deletion of a rule that does not exist does not fail.
// If the API returns 403 or 404, the error wraps ErrSyncSettingsUnavailable.
// Each request sends at most 100 upserts and 100 deletions.
func (client Client) PatchSyncSettings(
	projectKey string,
	upserts []SyncSettingRule,
	deletions []SyncSettingPattern,
) error {
	for start := 0; start < max(len(upserts), len(deletions)); start += maxSettingsPatchItems {
		batchUpserts := batch(upserts, start, maxSettingsPatchItems)
		batchDeletions := batch(deletions, start, maxSettingsPatchItems)
		_, err := client.mutate(mutation{
			method: http.MethodPatch, action: "update", resource: "sync settings for project", key: projectKey,
			body: struct {
				Upserts   []SyncSettingRule    `json:"upserts"`
				Deletions []SyncSettingPattern `json:"deletions"`
			}{Upserts: batchUpserts, Deletions: batchDeletions},
		}, settingsPath(projectKey)...)
		if status, ok := responseStatusCode(err); ok && (status == http.StatusForbidden || status == http.StatusNotFound) {
			return fmt.Errorf("update sync settings for project %q: %w", projectKey, ErrSyncSettingsUnavailable)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// ExactVariationRules returns one rule for each variation, with direction.
// The pattern of each rule is the lookup key of the variation, so that the
// rule matches only that variation.
func ExactVariationRules(ids []syncdomain.ResourceID, direction syncdomain.Direction) []SyncSettingRule {
	rules := make([]SyncSettingRule, 0, len(ids))
	for _, id := range ids {
		rules = append(rules, SyncSettingRule{ResourceKind: id.Kind, ResourcePattern: id.LookupKey, Direction: direction})
	}
	return rules
}

// ExactVariationPatterns returns the pattern of the exact rule of each
// variation.
func ExactVariationPatterns(ids []syncdomain.ResourceID) []SyncSettingPattern {
	patterns := make([]SyncSettingPattern, 0, len(ids))
	for _, id := range ids {
		patterns = append(patterns, SyncSettingPattern{ResourceKind: id.Kind, ResourcePattern: id.LookupKey})
	}
	return patterns
}

// batch returns the items from start, at most size items. It returns an
// empty list, not nil, so that the JSON body has a list.
func batch[T any](items []T, start, size int) []T {
	if start >= len(items) {
		return []T{}
	}
	return items[start:min(start+size, len(items))]
}
