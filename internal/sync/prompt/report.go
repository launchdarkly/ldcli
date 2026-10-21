package prompt

import (
	"maps"
	"slices"

	syncapi "github.com/launchdarkly/ldcli/internal/sync/api"
	syncconsole "github.com/launchdarkly/ldcli/internal/sync/console"
)

// maxReportErrorLength is the largest number of characters that the report
// of one failed resource can send.
const maxReportErrorLength = 1000

// sendSyncReports sends one sync report for each project in outcomes. If a
// report fails, it writes a warning to the error output. A failed report does
// not change the result of the sync, so it returns nothing.
func sendSyncReports(options Options, client syncapi.Client, outcomes []ResourceOutcome) {
	if options.syncSource == "" {
		return
	}
	byProject := make(map[string][]syncapi.SyncReportResource)
	for _, outcome := range outcomes {
		byProject[outcome.ID.ProjectKey] = append(byProject[outcome.ID.ProjectKey], reportResource(outcome))
	}

	console := syncconsole.New(options.ErrorOutput)
	for _, projectKey := range slices.Sorted(maps.Keys(byProject)) {
		err := client.PostSyncReport(projectKey, syncapi.SyncReport{
			Source:    options.syncSource,
			Resources: byProject[projectKey],
		})
		if err != nil {
			_ = console.Printf(
				"Warning: sync did not send its report to LaunchDarkly for project %s: %s\n",
				projectKey, syncapi.ErrorText(err.Error()),
			)
		}
	}
}

// reportResource returns the report of one outcome. A resource that did not
// sync, because it failed or sync skipped it, is a failure.
func reportResource(outcome ResourceOutcome) syncapi.SyncReportResource {
	resource := syncapi.SyncReportResource{
		ResourceKind:      outcome.ID.Kind,
		ResourceLookupKey: outcome.ID.LookupKey,
		Status:            syncapi.ReportStatusSucceeded,
		Version:           outcome.Version,
	}
	if outcome.Status != OutcomeSucceeded {
		resource.Status, resource.Version = syncapi.ReportStatusFailed, 0
		resource.Error = truncate(syncapi.ErrorText(outcome.Error), maxReportErrorLength)
		if resource.Error == "" {
			resource.Error = "sync did not apply the change"
		}
	}
	return resource
}

// inSyncOutcomes returns a succeeded outcome for each resource of a plan that
// has no changes.
func inSyncOutcomes(plan Plan) []ResourceOutcome {
	outcomes := make([]ResourceOutcome, 0, len(plan.Resources))
	for _, resource := range plan.Resources {
		outcomes = append(outcomes, ResourceOutcome{ID: resource.ID, Action: resource.Action, Status: OutcomeSucceeded})
	}
	return outcomes
}

// truncate returns at most limit characters of text.
func truncate(text string, limit int) string {
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return string(runes[:limit])
}
