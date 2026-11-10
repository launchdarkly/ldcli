package api

import (
	"fmt"
	"maps"
	"net/http"
	"net/url"
	"slices"
	"strings"

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

// SyncDirections returns the effective LaunchDarkly setting for each
// resource. A resource of a kind that LaunchDarkly has no setting for is left
// out, and the caller decides what a missing setting means.
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
	byKind := map[syncdomain.Kind][]syncdomain.ResourceID{}
	for _, id := range ids {
		if id.ProjectKey != projectKey {
			return nil, fmt.Errorf("read sync settings: %s is not in project %q", id.Label(), projectKey)
		}
		byKind[id.Kind] = append(byKind[id.Kind], id)
	}
	result := make(map[syncdomain.ResourceID]syncdomain.SyncDirection, len(ids))
	// The endpoint rejects a request that names a kind it has no setting for,
	// and the rejection fails every resource of the request. Each kind travels
	// alone, so a tool or a skill cannot take the variations down with it.
	for _, kind := range slices.Sorted(maps.Keys(byKind)) {
		directions, err := client.syncDirectionsOfKind(projectKey, byKind[kind])
		if err != nil {
			return nil, err
		}
		maps.Copy(result, directions)
	}
	return result, nil
}

// syncDirectionsOfKind reads the settings of resources of one kind. A kind
// that LaunchDarkly rejects as unsupported has no settings.
func (client Client) syncDirectionsOfKind(
	projectKey string,
	ids []syncdomain.ResourceID,
) (map[syncdomain.ResourceID]syncdomain.SyncDirection, error) {
	query := url.Values{}
	for _, id := range ids {
		query.Add("resources", string(id.Kind)+":"+id.LookupKey)
	}
	result := make(map[syncdomain.ResourceID]syncdomain.SyncDirection, len(ids))
	// The endpoint pages its items. Read every page, so that a batch of more
	// than one page of resources does not look like missing settings.
	for offset := 0; ; {
		page, err := client.readSyncSettingsPage(projectKey, query, offset)
		if unsupportedResourceKind(err) {
			return result, nil
		}
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

// unsupportedResourceKind reports the 400 response of the sync settings
// endpoint for a resource kind that LaunchDarkly has no setting for.
func unsupportedResourceKind(err error) bool {
	response, ok := responseError(err)
	if !ok {
		return false
	}
	status, _ := response["statusCode"].(float64)
	message, _ := response["message"].(string)
	return int(status) == http.StatusBadRequest && strings.Contains(strings.ToLower(message), "unsupported resource kind")
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
