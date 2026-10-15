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
