package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	lderrors "github.com/launchdarkly/ldcli/internal/errors"
	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
)

func TestClientPostSyncReport(t *testing.T) {
	transport := &recordingServer{}
	client := transport.client(t)

	err := client.PostSyncReport("project", SyncReport{
		Source: "git:github.com/acme/support-app",
		Resources: []SyncReportResource{
			{ResourceKind: syncdomain.KindVariation, ResourceLookupKey: "support-bot/default", Status: ReportStatusSucceeded, Version: 7},
			{ResourceKind: syncdomain.KindVariation, ResourceLookupKey: "support-bot/short", Status: ReportStatusSucceeded},
			{ResourceKind: syncdomain.KindVariation, ResourceLookupKey: "support-bot/long", Status: ReportStatusFailed, Error: "Invalid key ''"},
		},
	})

	require.NoError(t, err)
	require.Len(t, transport.Requests, 1)
	request := transport.Requests[0]
	assert.Equal(t, http.MethodPost, request.Method)
	assert.Equal(t, "/api/v2/projects/project/configs/sync/reports", request.Path)
	assert.Equal(t, "application/json", request.Header.Get("Content-Type"))
	assert.JSONEq(t, `{
		"source": "git:github.com/acme/support-app",
		"resources": [
			{"resourceKind":"variation","resourceLookupKey":"support-bot/default","status":"succeeded","version":7},
			{"resourceKind":"variation","resourceLookupKey":"support-bot/short","status":"succeeded"},
			{"resourceKind":"variation","resourceLookupKey":"support-bot/long","status":"failed","error":"Invalid key ''"}
		]
	}`, string(request.Body))
}

func TestClientPostSyncReportSendsAtMostOneThousandResources(t *testing.T) {
	resources := make([]SyncReportResource, 0, 2500)
	for index := range 2500 {
		resources = append(resources, SyncReportResource{
			ResourceKind: syncdomain.KindVariation, ResourceLookupKey: fmt.Sprintf("support/v%04d", index),
			Status: ReportStatusSucceeded,
		})
	}
	transport := &recordingServer{}
	client := transport.client(t)

	require.NoError(t, client.PostSyncReport("project", SyncReport{Source: "git:example.com/acme/app", Resources: resources}))

	require.Len(t, transport.Requests, 3)
	for index, expected := range []int{1000, 1000, 500} {
		var report SyncReport
		require.NoError(t, json.Unmarshal(transport.Requests[index].Body, &report))
		assert.Len(t, report.Resources, expected)
		assert.Equal(t, "git:example.com/acme/app", report.Source)
		assert.Equal(t, fmt.Sprintf("support/v%04d", index*1000), report.Resources[0].ResourceLookupKey)
	}
}

func TestClientPostSyncReportWithoutResourcesSendsNoRequest(t *testing.T) {
	transport := &recordingServer{}

	require.NoError(t, transport.client(t).PostSyncReport("project", SyncReport{Source: "git:x/y"}))

	assert.Empty(t, transport.Requests)
}

func TestClientPostSyncReportReturnsError(t *testing.T) {
	transport := &recordingServer{Err: lderrors.NewError(`{"code":"not_found","message":"Not found","statusCode":404}`)}
	client := transport.client(t)

	err := client.PostSyncReport("project", SyncReport{Source: "git:x/y", Resources: []SyncReportResource{
		{ResourceKind: syncdomain.KindVariation, ResourceLookupKey: "support/default", Status: ReportStatusSucceeded},
	}})

	require.Error(t, err)
	assert.True(t, IsNotFound(err))
	assert.Equal(t, `send sync report for project "project": Not found`, ErrorText(err.Error()))
}
