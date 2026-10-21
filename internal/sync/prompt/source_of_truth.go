package prompt

import (
	"fmt"
	"slices"
	"strings"

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
	for index, projectKey := range projectKeys {
		rules := syncapi.ExactVariationRules(byProject[projectKey], action.SourceOfTruth)
		if err := api.PatchSyncSettings(projectKey, rules, nil); err != nil {
			if index > 0 {
				_ = syncconsole.New(options.ErrorOutput).Printf(
					"The source of truth changed for project %s only.\n", strings.Join(projectKeys[:index], ", "),
				)
			}
			return err
		}
	}
	return syncconsole.New(options.Output).Line(action.SourceOfTruth.Summary())
}
