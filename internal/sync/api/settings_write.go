package api

import (
	"fmt"
	"net/http"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
)

// syncSettingsWriteLimit is the most rules one sync settings write accepts.
const syncSettingsWriteLimit = 100

type syncSettingsPatch struct {
	Upserts   []syncSettingRule `json:"upserts"`
	Deletions []syncSettingRule `json:"deletions"`
}

type syncSettingRule struct {
	ResourceKind    syncdomain.Kind          `json:"resourceKind"`
	ResourcePattern string                   `json:"resourcePattern"`
	Direction       syncdomain.SyncDirection `json:"direction"`
}

// ClaimForCode makes code the owner of each resource.
func (client Client) ClaimForCode(projectKey string, ids []syncdomain.ResourceID) error {
	return client.setSyncDirection(projectKey, ids, syncdomain.SyncDirectionCodeCanonical)
}

// ReturnToLaunchDarkly makes LaunchDarkly the owner of each resource.
func (client Client) ReturnToLaunchDarkly(projectKey string, ids []syncdomain.ResourceID) error {
	return client.setSyncDirection(projectKey, ids, syncdomain.SyncDirectionServerCanonical)
}

// setSyncDirection upserts one exact rule per resource, in batches of at most
// syncSettingsWriteLimit rules. It checks every resource before it writes.
func (client Client) setSyncDirection(
	projectKey string, ids []syncdomain.ResourceID, direction syncdomain.SyncDirection,
) error {
	rules := make([]syncSettingRule, 0, len(ids))
	for _, id := range ids {
		if id.ProjectKey != projectKey {
			return fmt.Errorf("update sync settings: %s is not in project %q", id.Label(), projectKey)
		}
		rules = append(rules, syncSettingRule{ResourceKind: id.Kind, ResourcePattern: id.LookupKey, Direction: direction})
	}
	for start := 0; start < len(rules); start += syncSettingsWriteLimit {
		end := min(start+syncSettingsWriteLimit, len(rules))
		_, err := client.mutate(mutation{
			method: http.MethodPatch, action: "update", resource: "sync settings in project", key: projectKey,
			body: syncSettingsPatch{Upserts: rules[start:end], Deletions: []syncSettingRule{}},
		}, projectPath(projectKey, "configs", "sync", "settings")...)
		if IsForbidden(err) {
			return fmt.Errorf(
				"%w: your role needs the updateSyncSettings action on proj/%s:ai-code-sync/*; "+
					"ask a LaunchDarkly admin to grant it: %w",
				ErrSyncSettingsForbidden, projectKey, err,
			)
		}
		if err != nil {
			return err
		}
	}
	return nil
}
