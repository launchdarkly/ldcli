package prompt

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
	syncapi "github.com/launchdarkly/ldcli/internal/sync/api"
)

func validatePlansForApply(plans []syncapi.ProjectPlan) error {
	for _, plan := range plans {
		for _, resource := range plan.Resources {
			switch {
			case resource.Error != nil:
				return fmt.Errorf(
					"cannot apply %s/%s: %s",
					plan.ProjectKey,
					resource.LookupKey,
					resource.Error.Message,
				)
			case resource.Status == syncapi.ResourceStatusConflict:
				return fmt.Errorf(
					"cannot apply conflicted resource %s/%s",
					plan.ProjectKey,
					resource.LookupKey,
				)
			case resource.SyncDirection == syncapi.SyncDirectionServerCanonical &&
				resource.Status != syncapi.ResourceStatusInSync:
				return fmt.Errorf(
					"cannot apply server-canonical resource %s/%s",
					plan.ProjectKey,
					resource.LookupKey,
				)
			}
		}
	}
	return nil
}

func planResourceErrors(plans []syncapi.ProjectPlan) error {
	var resourceErrors []error
	for _, plan := range plans {
		for _, resource := range plan.Resources {
			if resource.Error == nil {
				continue
			}
			resourceErrors = append(resourceErrors, fmt.Errorf(
				"- %s/%s: %s: %s",
				plan.ProjectKey,
				resource.LookupKey,
				resource.Error.Code,
				resource.Error.Message,
			))
		}
	}
	if len(resourceErrors) == 0 {
		return nil
	}
	return fmt.Errorf("cannot sync:\n%w", errors.Join(resourceErrors...))
}

func validatePlansForSync(plans []syncapi.ProjectPlan) error {
	for _, plan := range plans {
		for _, resource := range plan.Resources {
			switch {
			case resource.Error != nil:
				return fmt.Errorf(
					"cannot sync %s/%s: %s",
					plan.ProjectKey,
					resource.LookupKey,
					resource.Error.Message,
				)
			case isServerPull(resource):
				continue
			case resource.Status == syncapi.ResourceStatusConflict:
				return fmt.Errorf(
					"cannot sync conflicted resource %s/%s",
					plan.ProjectKey,
					resource.LookupKey,
				)
			case resource.SyncDirection == syncapi.SyncDirectionServerCanonical &&
				resource.Status != syncapi.ResourceStatusInSync:
				return fmt.Errorf(
					"cannot apply server-canonical resource %s/%s",
					plan.ProjectKey,
					resource.LookupKey,
				)
			}
		}
	}
	return nil
}

func plansRequireApply(plans []syncapi.ProjectPlan) bool {
	for _, plan := range plans {
		for _, resource := range plan.Resources {
			if resource.Status != syncapi.ResourceStatusInSync ||
				resource.ManifestUpdateRequired {
				return true
			}
		}
	}
	return false
}

func plansNeedConfirmation(plans []syncapi.ProjectPlan) bool {
	for _, plan := range plans {
		for _, resource := range plan.Resources {
			if resource.Status != syncapi.ResourceStatusInSync {
				return true
			}
		}
	}
	return false
}

func validateReplannedPlans(
	reviewed []syncapi.ProjectPlan,
	durable []syncapi.ProjectPlan,
	pulled []serverPull,
) error {
	if len(reviewed) != len(durable) {
		return syncStateChangedError()
	}

	pulledResources := make(map[projectResourceKey]struct{}, len(pulled))
	for _, resource := range pulled {
		pulledResources[projectResourceKey{
			projectKey: resource.ProjectKey,
			lookupKey:  resource.LookupKey,
		}] = struct{}{}
	}

	durableByProject := make(map[string]syncapi.ProjectPlan, len(durable))
	for _, plan := range durable {
		durableByProject[plan.ProjectKey] = plan
	}

	for _, reviewedPlan := range reviewed {
		durablePlan, exists := durableByProject[reviewedPlan.ProjectKey]
		if !exists || len(reviewedPlan.Resources) != len(durablePlan.Resources) {
			return syncStateChangedError()
		}

		durableByResource := make(
			map[plannedResourceKey]syncapi.PlannedResource,
			len(durablePlan.Resources),
		)
		for _, resource := range durablePlan.Resources {
			durableByResource[plannedResourceKey{
				kind:      resource.ResourceKind,
				lookupKey: resource.LookupKey,
			}] = resource
		}

		for _, reviewedResource := range reviewedPlan.Resources {
			key := plannedResourceKey{
				kind:      reviewedResource.ResourceKind,
				lookupKey: reviewedResource.LookupKey,
			}
			durableResource, exists := durableByResource[key]
			if !exists {
				return syncStateChangedError()
			}

			_, wasPulled := pulledResources[projectResourceKey{
				projectKey: reviewedPlan.ProjectKey,
				lookupKey:  reviewedResource.LookupKey,
			}]
			if err := validateReplannedResource(
				reviewedPlan.ProjectKey,
				reviewedResource,
				durableResource,
				wasPulled,
			); err != nil {
				return err
			}
		}
	}

	return validatePlansForApply(durable)
}

type projectResourceKey struct {
	projectKey string
	lookupKey  string
}

type plannedResourceKey struct {
	kind      syncdomain.Kind
	lookupKey string
}

func validateReplannedResource(
	projectKey string,
	reviewed syncapi.PlannedResource,
	durable syncapi.PlannedResource,
	wasPulled bool,
) error {
	if reviewed.SyncDirection != durable.SyncDirection {
		return syncStateChangedError()
	}
	if wasPulled {
		return validatePulledResource(projectKey, reviewed, durable)
	}

	unchanged := reviewed.Status == durable.Status &&
		reviewed.ManifestUpdateRequired == durable.ManifestUpdateRequired &&
		reviewed.LocalDeleted == durable.LocalDeleted &&
		reviewed.ServerDeleted == durable.ServerDeleted &&
		reflect.DeepEqual(reviewed.Error, durable.Error) &&
		equalJSON(reviewed.Diff, durable.Diff)
	if unchanged {
		return nil
	}

	return fmt.Errorf(
		"sync state changed for %s/%s after review; run sync again",
		projectKey,
		reviewed.LookupKey,
	)
}

func validatePulledResource(
	projectKey string,
	reviewed syncapi.PlannedResource,
	durable syncapi.PlannedResource,
) error {
	if durable.Error != nil || durable.Status != syncapi.ResourceStatusInSync {
		return fmt.Errorf(
			"server state changed while pulling %s/%s; run sync again",
			projectKey,
			reviewed.LookupKey,
		)
	}
	if reviewed.ServerDeleted &&
		(!durable.LocalDeleted || !durable.ServerDeleted) {
		return fmt.Errorf(
			"server state changed while deleting %s/%s; run sync again",
			projectKey,
			reviewed.LookupKey,
		)
	}
	return nil
}

func syncStateChangedError() error {
	return fmt.Errorf("sync state changed after review; run sync again")
}

func equalJSON(left, right json.RawMessage) bool {
	if len(left) == 0 || len(right) == 0 {
		return len(left) == len(right)
	}

	var leftValue any
	var rightValue any
	return json.Unmarshal(left, &leftValue) == nil &&
		json.Unmarshal(right, &rightValue) == nil &&
		reflect.DeepEqual(leftValue, rightValue)
}
