package prompt

import (
	"encoding/json"
	"errors"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
	syncapi "github.com/launchdarkly/ldcli/internal/sync/api"
	synclocal "github.com/launchdarkly/ldcli/internal/sync/local"
	syncmanifest "github.com/launchdarkly/ldcli/internal/sync/manifest"
)

func TestExecutePlanDoesNotMutateReviewedManifest(t *testing.T) {
	id := ResourceID{Kind: syncdomain.KindVariation, ProjectKey: "project", LookupKey: "config/variation"}
	reviewedManifest := syncmanifest.Manifest{
		FormatVersion: syncmanifest.FormatVersion,
		Resources: []syncmanifest.Resource{{
			ResourceKind: id.Kind,
			ProjectKey:   id.ProjectKey,
			LookupKey:    id.LookupKey,
			Fingerprint:  "reviewed",
		}},
	}
	plan := Plan{Resources: []PlannedResource{{
		ID:               id,
		Action:           ActionUpdateManifest,
		LocalFingerprint: "updated",
	}}}

	_, updatedManifest, err := executePlan("", synclocal.Store{}, syncapi.Client{}, reviewedManifest, plan, nil)

	require.NoError(t, err)
	assert.Equal(t, "reviewed", reviewedManifest.Resources[0].Fingerprint)
	assert.Equal(t, "updated", updatedManifest.Resources[0].Fingerprint)
}

func TestApplyServerChangeDoesNotRereadAfterDefinitiveAPIError(t *testing.T) {
	transport := &definitiveMutationAPI{}
	variation := testVariation("local")
	resource := PlannedResource{
		ID:     testResourceID(),
		Action: ActionUpdateServer,
		Local:  &variation,
	}

	client := syncapi.NewClient(transport, "token", "https://example.com")
	err := applyServerChange(client, newAttachmentResolver(client), resource)

	require.ErrorContains(t, err, `"statusCode":400`)
	assert.Zero(t, transport.reads)
}

func TestExecutePlanDoesNotMutateWhileConflictIsUnresolved(t *testing.T) {
	transport := &attachmentMutationAPI{
		current: syncdomain.Tool{Key: "search", Schema: map[string]any{"type": "object"}},
		version: 2,
	}
	client := syncapi.NewClient(transport, "token", "https://example.com")
	variation := testVariation("local")
	variation.Tools = []syncdomain.AttachmentRef{{Key: "search"}}
	variation.Attachments = []syncdomain.Attachment{{
		Kind: syncdomain.AttachmentTool,
		Tool: &syncdomain.Tool{Key: "search", Schema: map[string]any{"type": "string"}},
	}}
	plan := Plan{Resources: []PlannedResource{
		{
			ID:     ResourceID{Kind: syncdomain.KindVariation, ProjectKey: "project", LookupKey: "config/first"},
			Action: ActionUpdateServer, Local: &variation,
		},
		{
			ID:     ResourceID{Kind: syncdomain.KindVariation, ProjectKey: "project", LookupKey: "config/second"},
			Action: ActionConflict, Local: &variation,
		},
	}}

	outcomes, _, err := executePlan("", synclocal.Store{}, client, syncmanifest.New(), plan, nil)

	require.ErrorContains(t, err, "cannot sync conflicted resource")
	assert.Empty(t, outcomes)
	assert.Zero(t, transport.reads)
	assert.Zero(t, transport.updates)
}

func TestExecutePlanSkipsEveryConsumerWhenSharedAttachmentFails(t *testing.T) {
	transport := &attachmentMutationAPI{
		current:   syncdomain.Tool{Key: "search", Schema: map[string]any{"type": "object"}},
		version:   2,
		updateErr: errors.New(`{"code":"invalid_request","statusCode":400}`),
	}
	client := syncapi.NewClient(transport, "token", "https://example.com")
	description := "Search all documentation"
	variation := syncdomain.Variation{
		Mode: syncdomain.VariationModeAgent, Key: "default", Name: "Support",
		Tools: []syncdomain.AttachmentRef{{Key: "search"}},
		Attachments: []syncdomain.Attachment{{
			Kind: syncdomain.AttachmentTool,
			Tool: &syncdomain.Tool{
				Key: "search", Description: &description, Schema: map[string]any{"type": "object"},
			},
		}},
	}
	plan := Plan{Resources: []PlannedResource{
		{
			ID:     ResourceID{Kind: syncdomain.KindVariation, ProjectKey: "project", LookupKey: "config/first"},
			Action: ActionUpdateServer, Local: &variation,
		},
		{
			ID:     ResourceID{Kind: syncdomain.KindVariation, ProjectKey: "project", LookupKey: "config/second"},
			Action: ActionUpdateServer, Local: &variation,
		},
	}}

	originalManifest := syncmanifest.New()
	originalManifest.SetFingerprint(
		syncdomain.ResourceID{Kind: syncdomain.KindTool, ProjectKey: "project", LookupKey: "search"},
		"sha256:reviewed",
	)
	outcomes, manifest, err := executePlan("", synclocal.Store{}, client, originalManifest, plan, nil)

	require.Error(t, err)
	require.Len(t, outcomes, 2)
	assert.Equal(t, OutcomeFailed, outcomes[0].Status)
	assert.Equal(t, OutcomeFailed, outcomes[1].Status)
	require.Len(t, manifest.Resources, 1)
	assert.Equal(t, "sha256:reviewed", manifest.Resources[0].Fingerprint)
	assert.Equal(t, 1, transport.updates)
	assert.Equal(t, 1, transport.reads)
}

func TestAttachmentVersioningDoesNotMutateReviewedVariation(t *testing.T) {
	transport := &attachmentMutationAPI{
		current: syncdomain.Tool{Key: "search", Schema: map[string]any{"type": "object"}},
		version: 2,
	}
	client := syncapi.NewClient(transport, "token", "https://example.com")
	variation := testVariation("local")
	variation.Tools = []syncdomain.AttachmentRef{{Key: "search", Version: 1}}
	variation.Attachments = []syncdomain.Attachment{{
		Kind:    syncdomain.AttachmentTool,
		Version: 2,
		Tool:    &syncdomain.Tool{Key: "search", Schema: map[string]any{"type": "object"}},
	}}

	resolved, err := newAttachmentResolver(client).resolveVariation("project", variation)
	require.NoError(t, err)
	pinned := variationPinnedToLatest(variation)

	assert.Equal(t, 1, variation.Tools[0].Version)
	assert.Equal(t, 2, resolved.Tools[0].Version)
	assert.Equal(t, 2, pinned.Tools[0].Version)
}

type attachmentMutationAPI struct {
	current   syncdomain.Tool
	version   int
	reads     int
	updates   int
	updateErr error
}

func (api *attachmentMutationAPI) MakeRequest(
	_ string,
	method string,
	_ string,
	_ string,
	_ url.Values,
	body []byte,
	_ bool,
) ([]byte, error) {
	switch method {
	case "GET":
		api.reads++
	case "PATCH":
		api.updates++
		if api.updateErr != nil {
			return nil, api.updateErr
		}
		if err := json.Unmarshal(body, &api.current); err != nil {
			return nil, err
		}
		api.current.Key = "search"
		api.version++
	}
	return json.Marshal(struct {
		syncdomain.Tool
		Version int `json:"version"`
	}{Tool: api.current, Version: api.version})
}

func (*attachmentMutationAPI) MakeUnauthenticatedRequest(string, string, []byte) ([]byte, error) {
	return nil, nil
}

type definitiveMutationAPI struct {
	reads int
}

func (api *definitiveMutationAPI) MakeRequest(
	_ string,
	method string,
	_ string,
	_ string,
	_ url.Values,
	_ []byte,
	_ bool,
) ([]byte, error) {
	if method == "GET" {
		api.reads++
		return []byte(`{"key":"support","mode":"agent","variations":[]}`), nil
	}
	return nil, errors.New(`{"code":"invalid_request","statusCode":400}`)
}

func (*definitiveMutationAPI) MakeUnauthenticatedRequest(string, string, []byte) ([]byte, error) {
	return nil, nil
}
