package prompt

import (
	"fmt"
	"strings"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
	syncapi "github.com/launchdarkly/ldcli/internal/sync/api"
	synclocal "github.com/launchdarkly/ldcli/internal/sync/local"
)

type serverPull struct {
	ProjectKey   string
	LookupKey    string
	ConfigKey    string
	VariationKey string
	Path         string
	Action       serverPullAction
}

type serverPullAction int

const (
	replaceLocalFile serverPullAction = iota
	restoreLocalFile
	deleteLocalFile
)

func isServerPull(resource syncapi.PlannedResource) bool {
	if resource.Error != nil ||
		resource.ResourceKind != syncdomain.KindVariation ||
		resource.Status == syncapi.ResourceStatusInSync {
		return false
	}

	serverChanged := resource.Status == syncapi.ResourceStatusServerChanged
	serverCanUpdateLocal := resource.SyncDirection == syncapi.SyncDirectionBoth ||
		resource.SyncDirection == syncapi.SyncDirectionServerCanonical
	restoreServerCanonicalFile := resource.LocalDeleted &&
		resource.SyncDirection == syncapi.SyncDirectionServerCanonical

	return (serverChanged && serverCanUpdateLocal) || restoreServerCanonicalFile
}

func pullServerVariations(
	catalog syncapi.CatalogClient,
	store synclocal.Store,
	plans []syncapi.ProjectPlan,
) ([]serverPull, error) {
	pulls, err := collectServerPulls(plans)
	if err != nil || len(pulls) == 0 {
		return pulls, err
	}

	replacements, additions, deletions, err := prepareServerPulls(catalog, pulls)
	if err != nil {
		return nil, err
	}

	replacedPaths, err := store.ReplaceVariations(replacementInputs(replacements))
	if err != nil {
		return nil, err
	}
	addedPaths, err := store.Add(additionInputs(additions))
	if err != nil {
		return nil, err
	}
	deletedPaths, err := store.DeleteVariations(deletionInputs(deletions))
	if err != nil {
		return nil, err
	}

	for index, path := range replacedPaths {
		pulls[replacements[index].pullIndex].Path = path
	}
	for index, path := range addedPaths {
		pulls[additions[index].pullIndex].Path = path
	}
	for index, path := range deletedPaths {
		pulls[deletions[index].pullIndex].Path = path
	}

	return pulls, nil
}

func collectServerPulls(plans []syncapi.ProjectPlan) ([]serverPull, error) {
	var pulls []serverPull
	for _, plan := range plans {
		for _, resource := range plan.Resources {
			if !isServerPull(resource) {
				continue
			}
			configKey, variationKey, ok := strings.Cut(resource.LookupKey, "/")
			if !ok ||
				configKey == "" ||
				variationKey == "" ||
				strings.Contains(variationKey, "/") {
				return nil, fmt.Errorf(
					"invalid variation lookup key %q",
					resource.LookupKey,
				)
			}
			pulls = append(pulls, serverPull{
				ProjectKey:   plan.ProjectKey,
				LookupKey:    resource.LookupKey,
				ConfigKey:    configKey,
				VariationKey: variationKey,
				Action:       pullAction(resource),
			})
		}
	}
	return pulls, nil
}

func pullAction(resource syncapi.PlannedResource) serverPullAction {
	switch {
	case resource.ServerDeleted:
		return deleteLocalFile
	case resource.LocalDeleted:
		return restoreLocalFile
	default:
		return replaceLocalFile
	}
}

type configRef struct {
	projectKey string
	configKey  string
}

type preparedReplacement struct {
	pullIndex int
	input     synclocal.VariationReplacement
}

type preparedAddition struct {
	pullIndex int
	input     synclocal.VariationFile
}

type preparedDeletion struct {
	pullIndex int
	input     synclocal.VariationDeletion
}

func prepareServerPulls(
	catalog syncapi.CatalogClient,
	pulls []serverPull,
) ([]preparedReplacement, []preparedAddition, []preparedDeletion, error) {
	configs := make(map[configRef]syncapi.Config)
	var replacements []preparedReplacement
	var additions []preparedAddition
	var deletions []preparedDeletion

	for index, pull := range pulls {
		if pull.Action == deleteLocalFile {
			deletions = append(deletions, preparedDeletion{
				pullIndex: index,
				input: synclocal.VariationDeletion{
					ProjectKey:   pull.ProjectKey,
					ConfigKey:    pull.ConfigKey,
					VariationKey: pull.VariationKey,
				},
			})
			continue
		}

		variation, err := serverVariation(catalog, configs, pull)
		if err != nil {
			return nil, nil, nil, err
		}

		if pull.Action == restoreLocalFile {
			additions = append(additions, preparedAddition{
				pullIndex: index,
				input: synclocal.VariationFile{
					ProjectKey: pull.ProjectKey,
					ConfigKey:  pull.ConfigKey,
					Upsert:     false,
					Variation:  variation,
				},
			})
		} else {
			replacements = append(replacements, preparedReplacement{
				pullIndex: index,
				input: synclocal.VariationReplacement{
					ProjectKey: pull.ProjectKey,
					ConfigKey:  pull.ConfigKey,
					Variation:  variation,
				},
			})
		}
	}

	return replacements, additions, deletions, nil
}

func serverVariation(
	catalog syncapi.CatalogClient,
	configs map[configRef]syncapi.Config,
	pull serverPull,
) (syncdomain.Variation, error) {
	ref := configRef{
		projectKey: pull.ProjectKey,
		configKey:  pull.ConfigKey,
	}

	config, exists := configs[ref]
	if !exists {
		var err error
		config, err = catalog.Config(ref.projectKey, ref.configKey)
		if err != nil {
			return syncdomain.Variation{}, err
		}
		configs[ref] = config
	}

	for _, variation := range config.Variations {
		if variation.Key == pull.VariationKey {
			return variation, nil
		}
	}

	return syncdomain.Variation{}, fmt.Errorf(
		"variation %q was not found in AI Config %q",
		pull.VariationKey,
		pull.ConfigKey,
	)
}

func replacementInputs(prepared []preparedReplacement) []synclocal.VariationReplacement {
	inputs := make([]synclocal.VariationReplacement, len(prepared))
	for index, replacement := range prepared {
		inputs[index] = replacement.input
	}
	return inputs
}

func additionInputs(prepared []preparedAddition) []synclocal.VariationFile {
	inputs := make([]synclocal.VariationFile, len(prepared))
	for index, addition := range prepared {
		inputs[index] = addition.input
	}
	return inputs
}

func deletionInputs(prepared []preparedDeletion) []synclocal.VariationDeletion {
	inputs := make([]synclocal.VariationDeletion, len(prepared))
	for index, deletion := range prepared {
		inputs[index] = deletion.input
	}
	return inputs
}
