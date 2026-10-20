package prompt

import (
	"errors"
	"fmt"
	"slices"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
	syncapi "github.com/launchdarkly/ldcli/internal/sync/api"
	syncconsole "github.com/launchdarkly/ldcli/internal/sync/console"
)

// runSourceOfTruth writes an exact sync settings rule for each selected
// variation. Each variation must already have a local file, because the
// source of truth applies only to a variation that the workspace syncs.
func (runner Runner) runSourceOfTruth(options Options, workspace syncWorkspace, action SourceOfTruthAction) error {
	byProject := make(map[string][]syncdomain.ResourceID)
	var projectKeys []string
	for _, variation := range action.Variations {
		configKey, variationKey, err := variation.VariationKeys()
		if err != nil {
			return err
		}
		exists, err := workspace.local.VariationExists(variation.ProjectKey, configKey, variationKey)
		if err != nil {
			return err
		}
		if !exists {
			return fmt.Errorf(
				"variation %s does not sync in this workspace. To add it, run ldcli sync prompts add %s",
				variation, variation,
			)
		}
		if slices.Contains(byProject[variation.ProjectKey], variation) {
			return fmt.Errorf("variation %s was selected more than once", variation)
		}
		if _, ok := byProject[variation.ProjectKey]; !ok {
			projectKeys = append(projectKeys, variation.ProjectKey)
		}
		byProject[variation.ProjectKey] = append(byProject[variation.ProjectKey], variation)
	}
	slices.Sort(projectKeys)

	api := runner.api(options)
	for _, projectKey := range projectKeys {
		rules := syncapi.ExactVariationRules(byProject[projectKey], action.Direction)
		err := api.PatchSyncSettings(projectKey, rules, nil)
		switch {
		case errors.Is(err, syncapi.ErrSyncSettingsUnavailable):
			return fmt.Errorf("sync settings are not available for project %s, so the source of truth did not change", projectKey)
		case err != nil:
			return err
		}
	}
	return syncconsole.New(options.Output).Line(sourceOfTruthSummary(action.Direction))
}

// sourceOfTruthSummary tells the user what the source of truth of the
// variations now is.
func sourceOfTruthSummary(direction syncdomain.Direction) string {
	if direction == syncdomain.DirectionCodeCanonical {
		return "The local files are now the source of truth for the prompts of these variations. " +
			"LaunchDarkly rejects a prompt change that does not come from sync. The other settings sync both ways."
	}
	return "The LaunchDarkly UI and the local files can now both change these variations. " +
		"If both sides change a variation, sync asks which side to keep."
}
