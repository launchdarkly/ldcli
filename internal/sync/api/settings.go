package api

import (
	"fmt"
	"net/url"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
)

type settingsResponse struct {
	Items []resolvedSetting `json:"items"`
}

type resolvedSetting struct {
	ResourceKind      syncdomain.Kind          `json:"resourceKind"`
	ResourceLookupKey string                   `json:"resourceLookupKey"`
	Direction         syncdomain.SyncDirection `json:"direction"`
}

// SyncDirections returns the effective LaunchDarkly setting for each resource.
func (client Client) SyncDirections(
	projectKey string,
	ids []syncdomain.ResourceID,
) (map[syncdomain.ResourceID]syncdomain.SyncDirection, error) {
	if len(ids) == 0 {
		return map[syncdomain.ResourceID]syncdomain.SyncDirection{}, nil
	}
	if len(ids) > 100 {
		return nil, fmt.Errorf("read sync settings: at most 100 resources are allowed")
	}
	query := url.Values{}
	for _, id := range ids {
		if id.ProjectKey != projectKey {
			return nil, fmt.Errorf("read sync settings: %s is not in project %q", id.Label(), projectKey)
		}
		query.Add("resources", string(id.Kind)+":"+id.LookupKey)
	}
	response, err := client.read(
		fmt.Sprintf("get sync settings in project %q", projectKey),
		projectKey,
		query,
		projectPath(projectKey, "configs", "sync", "settings")...,
	)
	if err != nil {
		return nil, err
	}
	if len(response) == 0 {
		return map[syncdomain.ResourceID]syncdomain.SyncDirection{}, nil
	}
	settings, err := decodeJSON[settingsResponse](response, "sync settings response")
	if err != nil {
		return nil, err
	}
	result := make(map[syncdomain.ResourceID]syncdomain.SyncDirection, len(settings.Items))
	for _, item := range settings.Items {
		id := syncdomain.ResourceID{
			Kind: item.ResourceKind, ProjectKey: projectKey, LookupKey: item.ResourceLookupKey,
		}
		if !item.Direction.Valid() {
			return nil, syncdomain.NewProblem(
				syncdomain.ErrorSyncSettingUnavailable,
				fmt.Sprintf("LaunchDarkly returned unsupported direction %q", item.Direction),
				"Configure a supported sync setting in LaunchDarkly.",
			).WithResource(id)
		}
		result[id] = item.Direction
	}
	return result, nil
}
