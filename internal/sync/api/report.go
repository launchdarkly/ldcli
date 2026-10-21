package api

import (
	"net/http"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
)

const maxReportResources = 1000

// SyncReportResource is one resource of a sync report. An empty Error means that it synced.
type SyncReportResource struct {
	ID    syncdomain.ResourceID
	Error string
}

type reportResource struct {
	ResourceKind      syncdomain.Kind `json:"resourceKind"`
	ResourceLookupKey string          `json:"resourceLookupKey"`
	Status            string          `json:"status"`
	Error             string          `json:"error,omitempty"`
}

// PostSyncReport sends the sync report of one project. It sends nothing without resources.
func (client Client) PostSyncReport(projectKey, source string, resources []SyncReportResource) error {
	for start := 0; start < len(resources); start += maxReportResources {
		body := struct {
			Source    string           `json:"source"`
			Resources []reportResource `json:"resources"`
		}{Source: source}
		for _, resource := range resources[start:min(start+maxReportResources, len(resources))] {
			status := "succeeded"
			if resource.Error != "" {
				status = "failed"
			}
			body.Resources = append(body.Resources, reportResource{
				ResourceKind: resource.ID.Kind, ResourceLookupKey: resource.ID.LookupKey, Status: status, Error: resource.Error,
			})
		}
		if _, err := client.mutate(mutation{
			method: http.MethodPost, action: "send", resource: "sync report for project", key: projectKey, body: body,
		}, projectPath(projectKey, "configs/sync/reports")...); err != nil {
			return err
		}
	}
	return nil
}
