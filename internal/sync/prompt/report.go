package prompt

import (
	"io"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
	syncapi "github.com/launchdarkly/ldcli/internal/sync/api"
	syncconsole "github.com/launchdarkly/ldcli/internal/sync/console"
)

const maxReportErrorLength = 1000

// sendSyncReports sends the report of each project. A failed report is only a warning.
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

// reportResource returns the report of one outcome. A skipped resource is a failure.
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

func inSyncOutcomes(plan Plan) []ResourceOutcome {
	outcomes := make([]ResourceOutcome, 0, len(plan.Resources))
	for _, resource := range plan.Resources {
		outcomes = append(outcomes, ResourceOutcome{ID: resource.ID, Action: resource.Action, Status: OutcomeSucceeded})
	}
	return outcomes
}

func truncate(text string, limit int) string {
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return string(runes[:limit])
}
