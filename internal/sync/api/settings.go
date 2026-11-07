package api

import (
	"fmt"
	"net/url"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
)

// syncSettingsPageLimit is the page size of the sync settings endpoint. It
// matches the most resources that one call accepts.
const syncSettingsPageLimit = 100

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
	result := make(map[syncdomain.ResourceID]syncdomain.SyncDirection, len(ids))
	// The endpoint pages its items. Read every page, so that a batch of more
	// than one page of resources does not look like missing settings.
	for offset := 0; ; {
		page, err := client.readSyncSettingsPage(projectKey, query, offset)
		if err != nil {
			return nil, err
		}
		for _, item := range page.Items {
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
		offset += len(page.Items)
		// The total count ends the paging when the response has one. Without
		// it, a short page is the last one.
		switch {
		case len(page.Items) == 0:
			return result, nil
		case page.TotalCount > 0 && offset >= page.TotalCount:
			return result, nil
		case page.TotalCount == 0 && len(page.Items) < syncSettingsPageLimit:
			return result, nil
		}
	}
}

// readSyncSettingsPage reads one page of resolved settings. A response
// without a body is an empty page.
func (client Client) readSyncSettingsPage(projectKey string, query url.Values, offset int) (Page[resolvedSetting], error) {
	page := Page[resolvedSetting]{}
	values := pageQuery(syncSettingsPageLimit, offset)
	for key, value := range query {
		values[key] = value
	}
	response, err := client.read(
		fmt.Sprintf("get sync settings in project %q", projectKey),
		projectKey,
		values,
		projectPath(projectKey, "configs", "sync", "settings")...,
	)
	if err != nil || len(response) == 0 {
		return page, err
	}
	return decodeJSON[Page[resolvedSetting]](response, "sync settings response")
}
