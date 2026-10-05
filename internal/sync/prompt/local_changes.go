package prompt

import (
	"fmt"

	synclocal "github.com/launchdarkly/ldcli/internal/sync/local"
)

// applyLocalChange writes the server state represented by one reviewed action.
func applyLocalChange(store synclocal.Store, resource PlannedResource) error {
	configKey, variationKey, err := splitVariationLookupKey(resource.ID.LookupKey)
	if err != nil {
		return err
	}

	switch resource.Action {
	case ActionUpdateLocal:
		if resource.Local == nil {
			_, err := store.Add([]synclocal.VariationFile{{
				ProjectKey: resource.ID.ProjectKey,
				ConfigKey:  configKey,
				Upsert:     true,
				Variation:  *resource.Server,
			}})
			return err
		}
		_, err := store.ReplaceVariations([]synclocal.VariationReplacement{{
			ProjectKey: resource.ID.ProjectKey,
			ConfigKey:  configKey,
			Variation:  *resource.Server,
		}})
		return err
	case ActionDeleteLocal:
		_, err := store.DeleteVariations([]synclocal.VariationDeletion{{
			ProjectKey:   resource.ID.ProjectKey,
			ConfigKey:    configKey,
			VariationKey: variationKey,
		}})
		return err
	default:
		return fmt.Errorf("action %q does not change a local file", resource.Action)
	}
}
