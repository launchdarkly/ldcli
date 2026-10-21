package prompt

import (
	"fmt"
	"strings"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
	syncconsole "github.com/launchdarkly/ldcli/internal/sync/console"
)

func runSourceOfTruth(options Options, workspace syncWorkspace, action SourceOfTruthAction) error {
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
	}

	var changed []string
	for projectKey, ids := range syncdomain.GroupByProject(action.Variations) {
		if err := workspace.api.SetSourceOfTruth(projectKey, ids, action.SourceOfTruth); err != nil {
			if len(changed) != 0 {
				_ = syncconsole.New(options.ErrorOutput).Printf(
					"The source of truth changed for project %s only.\n", strings.Join(changed, ", "),
				)
			}
			return err
		}
		changed = append(changed, projectKey)
	}
	return syncconsole.New(options.Output).Line(action.SourceOfTruth.Summary())
}
