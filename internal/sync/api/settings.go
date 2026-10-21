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

// ErrSyncSettingsUnavailable means that the sync settings API of a project
// returned 404. For example, the plan of the account does not include the feature.
var ErrSyncSettingsUnavailable = errors.New("sync settings are not available")

// SettingsUnavailableWarning returns the warning for a project without sync
// settings. The effect tells the user what the command did instead.
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

// ResolveSourceOfTruth returns the source of truth of each resource in one
// project. If the API returns 404, the error wraps ErrSyncSettingsUnavailable.
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
		if id.ProjectKey != projectKey {
			return fmt.Errorf("resource %s is not in project %q", id, projectKey)
		}
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

// The sync settings API names a source of truth with a direction.
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

// maxSettingsPatchItems is the largest number of upserts, and also of
// deletions, that one sync settings patch can send.
const maxSettingsPatchItems = 100

// SyncSettingRule is one explicit source of truth rule. ResourcePattern is a lookup
// key, for example "support-bot/default", or a pattern such as
// "support-bot/*".
type SyncSettingRule struct {
	ResourceKind    syncdomain.Kind `json:"resourceKind"`
	ResourcePattern string          `json:"resourcePattern"`
	Direction       string          `json:"direction"`
}

// SyncSettingPattern names one explicit rule to delete.
type SyncSettingPattern struct {
	ResourceKind    syncdomain.Kind `json:"resourceKind"`
	ResourcePattern string          `json:"resourcePattern"`
}

// PatchSyncSettings creates or replaces each rule in upserts, and deletes each
// rule in deletions. A deletion of a rule that does not exist does not fail.
// If the API returns 404, the error wraps ErrSyncSettingsUnavailable.
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
		if settingsUnavailable(err) {
			return fmt.Errorf("update sync settings for project %q: %w", projectKey, ErrSyncSettingsUnavailable)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// ExactVariationRules returns one rule for each variation, with source.
// The pattern of each rule is the lookup key of the variation, so that the
// rule matches only that variation.
func ExactVariationRules(ids []syncdomain.ResourceID, source syncdomain.SourceOfTruth) []SyncSettingRule {
	rules := make([]SyncSettingRule, 0, len(ids))
	for _, id := range ids {
		rules = append(rules, SyncSettingRule{ResourceKind: id.Kind, ResourcePattern: id.LookupKey, Direction: wireDirection(source)})
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
