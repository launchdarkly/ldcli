package api

import (
	"net/http"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
)

// MaxReportResources is the largest number of resources that one sync report
// request can send.
const MaxReportResources = 1000

// The status values of a reported resource.
const (
	ReportStatusSucceeded = "succeeded"
	ReportStatusFailed    = "failed"
)

// SyncReport is the result of one sync for one project and one repository
// source.
type SyncReport struct {
	Source    string               `json:"source"`
	Resources []SyncReportResource `json:"resources"`
}

// SyncReportResource is the result of one resource in a sync report.
type SyncReportResource struct {
	ResourceKind      syncdomain.Kind `json:"resourceKind"`
	ResourceLookupKey string          `json:"resourceLookupKey"`
	Status            string          `json:"status"`
	// Error is the reason of a failure. It is empty when the status is
	// ReportStatusSucceeded.
	Error string `json:"error,omitempty"`
	// Version is the LaunchDarkly version that the sync wrote, or 0.
	Version int `json:"version,omitempty"`
}

// PostSyncReport sends the report for one project. If the report has more
// than MaxReportResources resources, it sends one request for each group of
// MaxReportResources resources. It stops at the first request that fails. A
// report without resources sends no request.
func (client Client) PostSyncReport(projectKey string, report SyncReport) error {
	for start := 0; start < len(report.Resources); start += MaxReportResources {
		part := report
		part.Resources = report.Resources[start:min(start+MaxReportResources, len(report.Resources))]
		if _, err := client.mutate(mutation{
			method: http.MethodPost, action: "send", resource: "sync report for project", key: projectKey, body: part,
		}, projectPath(projectKey, "configs/sync/reports")...); err != nil {
			return err
		}
	}
	return nil
}
