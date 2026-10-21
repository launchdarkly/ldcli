package prompt

import (
	"io"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
	syncapi "github.com/launchdarkly/ldcli/internal/sync/api"
	syncconsole "github.com/launchdarkly/ldcli/internal/sync/console"
)

// maxReportErrorLength is the largest number of characters that the report
// of one failed resource can send.
const maxReportErrorLength = 1000

// sendSyncReports sends one sync report for each project in outcomes. If a
// report fails, it writes a warning to the error output. A failed report does
// not change the result of the sync, so it returns nothing.
func (workspace syncWorkspace) sendSyncReports(errorOutput io.Writer, outcomes []ResourceOutcome) {
	ids := make([]ResourceID, 0, len(outcomes))
	resources := make(map[ResourceID]syncapi.SyncReportResource, len(outcomes))
	for _, outcome := range outcomes {
		ids = append(ids, outcome.ID)
		resources[outcome.ID] = reportResource(outcome)
	}

	console := syncconsole.New(errorOutput)
	for projectKey, projectIDs := range syncdomain.GroupByProject(ids) {
		report := make([]syncapi.SyncReportResource, 0, len(projectIDs))
		for _, id := range projectIDs {
			report = append(report, resources[id])
		}
		if err := workspace.api.PostSyncReport(projectKey, workspace.source, report); err != nil {
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
	resource := syncapi.SyncReportResource{ID: outcome.ID}
	if outcome.Status != OutcomeSucceeded {
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
