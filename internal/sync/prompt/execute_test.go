package prompt

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
	syncapi "github.com/launchdarkly/ldcli/internal/sync/api"
	"github.com/launchdarkly/ldcli/internal/sync/api/apitest"
	synclocal "github.com/launchdarkly/ldcli/internal/sync/local"
	syncmanifest "github.com/launchdarkly/ldcli/internal/sync/manifest"
)

func TestExecutePlanDoesNotMutateReviewedManifest(t *testing.T) {
	id := ResourceID{Kind: syncdomain.KindVariation, ProjectKey: "project", LookupKey: "config/variation"}
	reviewedManifest := syncmanifest.Manifest{
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

	client := newTestClient(t, transport.serve)
	err := applyServerChange(client, newAttachmentResolver(client), resource)

	require.ErrorContains(t, err, `"statusCode":400`)
	assert.Zero(t, transport.reads)
}

func TestExecutePlanDoesNotMutateWhileConflictIsUnresolved(t *testing.T) {
	transport := &attachmentMutationAPI{
		current: syncdomain.Tool{Key: "search", Schema: map[string]any{"type": "object"}},
		version: 2,
	}
	client := newTestClient(t, transport.serve)
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
		current:      syncdomain.Tool{Key: "search", Schema: map[string]any{"type": "object"}},
		version:      2,
		updateStatus: http.StatusBadRequest,
	}
	client := newTestClient(t, transport.serve)
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
	client := newTestClient(t, transport.serve)
	variation := testVariation("local")
	variation.Tools = []syncdomain.AttachmentRef{{Key: "search", Version: 1}}
	variation.Attachments = []syncdomain.Attachment{{
		Kind:    syncdomain.AttachmentTool,
		Version: 2,
		Tool:    &syncdomain.Tool{Key: "search", Schema: map[string]any{"type": "object"}},
	}}

	resolved, err := newAttachmentResolver(client).resolveVariation("project", variation)
	require.NoError(t, err)
	pinned, err := variation.PinnedToLatest()
	require.NoError(t, err)

	assert.Equal(t, 1, variation.Tools[0].Version)
	assert.Equal(t, 2, resolved.Tools[0].Version)
	assert.Equal(t, 2, pinned.Tools[0].Version)
}

func TestApplyResourceChangeRefreshesPinsWithCompleteServerModel(t *testing.T) {
	root := t.TempDir()
	store := synclocal.NewStore(root)
	authored := testVariation("Local")
	authored.ModelConfigKey = "claude"
	authored.Model = map[string]any{"temperature": 0.2}
	authored.Tools = []syncdomain.AttachmentRef{{Key: "search"}}
	authored.Attachments = []syncdomain.Attachment{{
		Kind:    syncdomain.AttachmentTool,
		Version: 1,
		Tool:    &syncdomain.Tool{Key: "search", Schema: map[string]any{"type": "object"}},
	}}
	_, err := store.Add([]synclocal.VariationFile{{
		ProjectKey: "production",
		ConfigKey:  "support",
		Variation:  authored,
	}})
	require.NoError(t, err)

	canonicalLocal := authored
	canonicalLocal.ModelConfigVersion = 7
	canonicalLocal.Model = map[string]any{"modelName": "claude-sonnet", "temperature": 0.2}
	server := canonicalLocal
	server.Name = "Server"
	server.Tools = []syncdomain.AttachmentRef{{Key: "search", Version: 1}}
	server.Attachments[0].Version = 2
	resource := PlannedResource{
		ID:                           testResourceID(),
		Action:                       ActionUpdateLocal,
		Local:                        &canonicalLocal,
		Server:                       &server,
		ServerHasStaleAttachmentPins: true,
	}
	transport := &variationUpdateAPI{}
	client := newTestClient(t, transport.serve)

	err = applyResourceChange(
		root,
		store,
		client,
		newAttachmentResolver(client),
		resource,
		&authored,
	)

	require.NoError(t, err)
	var request struct {
		ModelConfigVersion int                        `json:"modelConfigVersion"`
		Model              map[string]any             `json:"model"`
		Tools              []syncdomain.AttachmentRef `json:"tools"`
	}
	require.NoError(t, json.Unmarshal(transport.body, &request))
	assert.Equal(t, 7, request.ModelConfigVersion)
	assert.Equal(t, map[string]any{"modelName": "claude-sonnet", "temperature": 0.2}, request.Model)
	require.Len(t, request.Tools, 1)
	assert.Equal(t, 2, request.Tools[0].Version)
}

type variationUpdateAPI struct {
	body []byte
}

func (api *variationUpdateAPI) serve(request apitest.Request) (int, []byte) {
	method, body := request.Method, request.Body
	if method == "PATCH" {
		api.body = body
	}
	return http.StatusOK, []byte(`{}`)
}

type attachmentMutationAPI struct {
	current      syncdomain.Tool
	version      int
	reads        int
	updates      int
	updateStatus int
}

func (api *attachmentMutationAPI) serve(request apitest.Request) (int, []byte) {
	method, body := request.Method, request.Body
	switch method {
	case "GET":
		api.reads++
	case "PATCH":
		api.updates++
		if api.updateStatus != 0 {
			return api.updateStatus, []byte(`{"code":"invalid_request"}`)
		}
		if err := json.Unmarshal(body, &api.current); err != nil {
			return http.StatusBadRequest, nil
		}
		api.current.Key = "search"
		api.version++
	}
	return apitest.JSON(struct {
		syncdomain.Tool
		Version int `json:"version"`
	}{Tool: api.current, Version: api.version})
}

type definitiveMutationAPI struct {
	reads int
}

func (api *definitiveMutationAPI) serve(request apitest.Request) (int, []byte) {
	method := request.Method
	if method == "GET" {
		api.reads++
		return http.StatusOK, []byte(`{"key":"support","mode":"agent","variations":[]}`)
	}
	return 400, []byte(`{"code":"invalid_request","statusCode":400}`)
}
