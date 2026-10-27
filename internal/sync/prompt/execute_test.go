package prompt

import (
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strings"
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
	_, err := applyServerChange(client, newAttachmentResolver(client), resource)

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
	client := syncapi.NewClient(transport, "token", "https://example.com")

	pins, err := applyResourceChange(
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
	assert.Equal(t, []syncdomain.AttachmentRef{{Key: "search", Version: 2}}, pins)
}

type variationUpdateAPI struct {
	body []byte
}

func (api *variationUpdateAPI) MakeRequest(
	_ string,
	method string,
	_ string,
	_ string,
	_ url.Values,
	body []byte,
	_ bool,
) ([]byte, error) {
	if method == "PATCH" {
		api.body = body
	}
	return []byte(`{}`), nil
}

func (*variationUpdateAPI) MakeUnauthenticatedRequest(string, string, []byte) ([]byte, error) {
	return nil, nil
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

// Another working copy can create the same tool or skill first. A create that
// gets a 409 reads the attachment again, and succeeds when LaunchDarkly has
// the local content.
func TestPublishReadsAgainAfterACreateConflict(t *testing.T) {
	description := "Search the docs"
	local := syncdomain.Attachment{Kind: syncdomain.AttachmentTool, Upsert: true, Tool: &syncdomain.Tool{
		Key: "search", Description: &description, Schema: map[string]any{"type": "object"},
	}}

	tests := map[string]struct {
		remote    string
		wantError string
	}{
		"same content":      {remote: `{"key":"search","description":"Search the docs","schema":{"type":"object"},"version":1}`},
		"different content": {remote: `{"key":"search","schema":{"type":"string"},"version":1}`, wantError: `tool "search" changed concurrently`},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			transport := &createConflictAPI{remote: test.remote}
			client := syncapi.NewClient(transport, "token", "https://example.com")

			published, err := newAttachmentResolver(client).resolve("project", local, true)

			if test.wantError != "" {
				require.ErrorContains(t, err, test.wantError)
				require.ErrorContains(t, err, `"statusCode":409`)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, 1, published.Version)
			assert.Equal(t, []string{"GET", "POST", "GET"}, transport.methods)
		})
	}
}

func TestPublishCreatesAnAttachmentOnlyWithUpsert(t *testing.T) {
	tests := map[string]struct {
		attachment syncdomain.Attachment
		want       string
	}{
		"tool": {
			attachment: syncdomain.Attachment{Kind: syncdomain.AttachmentTool, Tool: &syncdomain.Tool{Key: "refund", Schema: map[string]any{}}},
			want: `tool "refund" does not exist in LaunchDarkly. ` +
				`To create it, add "upsert": true to its file and run ldcli sync tools --project project --key refund.`,
		},
		"skill": {
			attachment: syncdomain.Attachment{Kind: syncdomain.AttachmentSkill, Skill: &syncdomain.Skill{Key: "handoff", Markdown: "# Handoff\n"}},
			want: `skill "handoff" does not exist in LaunchDarkly. ` +
				"To create it, add upsert: true to its front matter and run ldcli sync skills --project project --key handoff.",
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			client := syncapi.NewClient(&createConflictAPI{}, "token", "https://example.com")

			_, err := newAttachmentResolver(client).resolve("project", test.attachment, false)

			require.EqualError(t, err, test.want)
		})
	}
}

func TestApplyServerChangeReadsAgainAfterAVariationCreateConflict(t *testing.T) {
	local := testVariation("New")
	resource := PlannedResource{ID: testResourceID(), Action: ActionCreateServer, Local: &local}
	var err error
	resource.LocalFingerprint, err = syncdomain.FingerprintVariation(resource.ID.ProjectKey, resource.ID.LookupKey, local)
	require.NoError(t, err)

	tests := map[string]struct {
		remote    syncdomain.Variation
		wantError string
	}{
		"same content":      {remote: local},
		"different content": {remote: testVariation("Other"), wantError: "server variation changed concurrently"},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			remote, err := json.Marshal(map[string]any{"key": "support", "mode": "agent", "variations": []syncdomain.Variation{test.remote}})
			require.NoError(t, err)
			client := syncapi.NewClient(&createConflictAPI{remote: string(remote)}, "token", "https://example.com")

			_, err = applyServerChange(client, newAttachmentResolver(client), resource)

			if test.wantError != "" {
				require.ErrorContains(t, err, test.wantError)
				return
			}
			require.NoError(t, err)
		})
	}
}

// createConflictAPI answers each create with a 409. A read returns remote
// after the create, and a 404 before it.
type createConflictAPI struct {
	remote  string
	created bool
	methods []string
}

func (api *createConflictAPI) MakeRequest(_ string, method string, _ string, _ string, _ url.Values, _ []byte, _ bool) ([]byte, error) {
	api.methods = append(api.methods, method)
	switch {
	case method == "POST":
		api.created = true
		return nil, errors.New(`{"code":"conflict","statusCode":409}`)
	case api.created:
		return []byte(api.remote), nil
	default:
		return nil, errors.New(`{"code":"not_found","statusCode":404}`)
	}
}

func (*createConflictAPI) MakeUnauthenticatedRequest(string, string, []byte) ([]byte, error) {
	return nil, nil
}

func TestExecutePlanAppliesToolRows(t *testing.T) {
	tool := func(description string, version int) *syncdomain.Attachment {
		return &syncdomain.Attachment{Kind: syncdomain.AttachmentTool, Version: version, Tool: &syncdomain.Tool{
			Key: "search", Description: &description, Schema: map[string]any{"type": "object"},
		}}
	}
	baseline := *tool("Old", 0)
	id := baseline.ID("production")

	tests := map[string]struct {
		local, server   *syncdomain.Attachment
		wantAction      Action
		wantVersion     int
		wantDescription string
	}{
		"push":   {local: tool("New", 0), server: tool("Old", 2), wantAction: ActionUpdateServer, wantVersion: 3, wantDescription: "New"},
		"pull":   {local: tool("Old", 0), server: tool("New", 5), wantAction: ActionUpdateLocal, wantVersion: 5, wantDescription: "New"},
		"delete": {local: tool("Old", 0), wantAction: ActionDeleteLocal},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			store := synclocal.NewStore(root)
			_, err := store.ReplaceAttachments([]syncdomain.SyncedAttachment{{ProjectKey: "production", Attachment: *test.local}})
			require.NoError(t, err)
			resource := planAttachment(id, test.local, test.server, attachmentFingerprint(t, baseline), true)
			require.Equal(t, test.wantAction, resource.Action)
			transport := &attachmentMutationAPI{current: *tool("Old", 0).Tool, version: 2}
			client := syncapi.NewClient(transport, "token", "https://example.com")
			manifest := syncmanifest.New()
			manifest.SetFingerprint(id, resource.BaselineFingerprint)

			outcomes, next, err := executePlan(root, store, client, manifest, Plan{Resources: []PlannedResource{resource}}, nil)

			require.NoError(t, err)
			require.Len(t, outcomes, 1)
			assert.Equal(t, OutcomeSucceeded, outcomes[0].Status)
			assert.Equal(t, test.wantVersion, outcomes[0].Version)
			local, err := synclocal.CompileAttachments(root, func(syncdomain.ResourceID) bool { return true })
			require.NoError(t, err)
			if test.wantDescription == "" {
				assert.Empty(t, local)
				assert.Empty(t, next.Resources, "a deletion removes the baseline")
				return
			}
			require.Len(t, local, 1)
			assert.Equal(t, test.wantDescription, *local[0].Attachment.Tool.Description)
			assert.Equal(t, attachmentFingerprint(t, *tool("New", 0)), next.Resources[0].Fingerprint)
		})
	}
}

// A tool deletion runs after every variation row, so that the pull of a
// variation that drops the tool runs first. Without that pull, the variation
// still uses the file, and the deletion keeps it.
func TestExecutePlanDeletesAToolOnlyAfterNoVariationUsesIt(t *testing.T) {
	tool := testAttachment(syncdomain.AttachmentTool, "local")
	withTool := testVariation("support")
	withTool.Tools = []syncdomain.AttachmentRef{{Key: "search"}}
	withTool.Attachments = []syncdomain.Attachment{tool}
	withoutTool := testVariation("support")
	deletion := planAttachment(tool.ID("production"), &tool, nil, attachmentFingerprint(t, tool), true)
	require.Equal(t, ActionDeleteLocal, deletion.Action)
	pull := PlannedResource{ID: testResourceID(), Action: ActionUpdateLocal, Local: &withTool, Server: &withoutTool}
	variationDeletion := PlannedResource{ID: testResourceID(), Action: ActionDeleteLocal, Local: &withTool}

	for name, test := range map[string]struct {
		resources []PlannedResource
		wantError string
	}{
		"the variation pull drops the tool": {resources: []PlannedResource{deletion, pull}},
		"the variation is deleted":          {resources: []PlannedResource{deletion, variationDeletion}},
		"no pull":                           {resources: []PlannedResource{deletion}, wantError: "variation production/support/default still uses it"},
	} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			store := synclocal.NewStore(root)
			_, err := store.Add([]synclocal.VariationFile{{ProjectKey: "production", ConfigKey: "support", Variation: withTool}})
			require.NoError(t, err)

			outcomes, _, err := executePlan(root, store, syncapi.Client{}, syncmanifest.New(), Plan{Resources: test.resources}, nil)

			toolFile := filepath.Join(root, syncdomain.RootDir, "production", "tools", "search.json")
			_, statErr := os.Stat(toolFile)
			if test.wantError != "" {
				require.ErrorContains(t, err, test.wantError)
				assert.Equal(t, OutcomeFailed, outcomes[0].Status)
				assert.NoError(t, statErr)
				return
			}
			require.NoError(t, err)
			assert.ErrorIs(t, statErr, os.ErrNotExist)
			assert.Equal(t, []ResourceID{deletion.ID, pull.ID}, []ResourceID{outcomes[0].ID, outcomes[1].ID}, "outcomes stay in plan order")
		})
	}
}

// A tool row runs before the variations that use the tool. A variation push
// that newly attaches the tool must pin the content that the tool row pulled,
// and must not publish its older local copy.
func TestExecutePlanPushPinsAToolThatTheSamePlanPulled(t *testing.T) {
	old, pulled := testAttachment(syncdomain.AttachmentTool, "old"), testAttachment(syncdomain.AttachmentTool, "new")
	pulled.Version = 5
	withTool := testVariation("support")
	withTool.Tools = []syncdomain.AttachmentRef{{Key: "search"}}
	withTool.Attachments = []syncdomain.Attachment{old}
	withoutTool := testVariation("support")
	root := t.TempDir()
	store := synclocal.NewStore(root)
	_, err := store.Add([]synclocal.VariationFile{{ProjectKey: "production", ConfigKey: "support", Variation: withTool}})
	require.NoError(t, err)

	toolRow := planAttachment(old.ID("production"), &old, &pulled, attachmentFingerprint(t, old), true)
	require.Equal(t, ActionUpdateLocal, toolRow.Action)
	push := PlannedResource{
		ID: testResourceID(), Action: ActionUpdateServer, Local: &withTool, Server: &withoutTool,
		LocalFingerprint: fingerprintOf(t, withTool),
	}
	current := testAttachment(syncdomain.AttachmentTool, "new")
	api := &settleAPI{tool: attachmentMutationAPI{current: *current.Tool, version: 5}}
	manifest := syncmanifest.New()
	manifest.SetFingerprint(toolRow.ID, toolRow.BaselineFingerprint)

	outcomes, next, err := executePlan(root, store, syncapi.NewClient(api, "token", "https://example.com"),
		manifest, Plan{Resources: []PlannedResource{toolRow, push}}, nil)

	require.NoError(t, err)
	assert.Equal(t, 0, api.tool.updates, "the push must not publish the old local copy")
	require.Len(t, api.variations, 1)
	assert.JSONEq(t, `[{"key":"search","version":5}]`, string(api.variations[0]["tools"]))
	assert.Equal(t, []syncdomain.AttachmentRef{{Key: "search", Version: 5}}, outcomes[1].pins)
	assert.Equal(t, attachmentFingerprint(t, pulled), fingerprintIn(next, toolRow.ID), "sync.lock records the pulled tool")
	withPulled := withTool
	withPulled.Attachments = []syncdomain.Attachment{pulled}
	assert.Equal(t, fingerprintOf(t, withPulled), fingerprintIn(next, push.ID))
	assert.Equal(t, "new", *readLocalTool(t, root).Description)
}

// A variation pull that newly references a tool must keep the edit that a
// tool row before it pushed. If that push fails, the pull fails too, so that
// it cannot write the old LaunchDarkly copy over the edit.
func TestExecutePlanPullKeepsAToolThatTheSamePlanPushed(t *testing.T) {
	for name, test := range map[string]struct {
		updateErr error
		wantError string
	}{
		"the tool push succeeds": {},
		"the tool push fails":    {updateErr: errors.New(`{"code":"forbidden","statusCode":403}`), wantError: "forbidden"},
	} {
		t.Run(name, func(t *testing.T) {
			edited, remote := testAttachment(syncdomain.AttachmentTool, "new"), testAttachment(syncdomain.AttachmentTool, "old")
			remote.Version = 2
			withTool := testVariation("support")
			withTool.Tools = []syncdomain.AttachmentRef{{Key: "search", Version: 2}}
			withTool.Attachments = []syncdomain.Attachment{remote}
			withoutTool := testVariation("support")
			root := t.TempDir()
			store := synclocal.NewStore(root)
			_, err := store.Add([]synclocal.VariationFile{{ProjectKey: "production", ConfigKey: "support", Variation: withoutTool}})
			require.NoError(t, err)
			_, err = store.ReplaceAttachments([]syncdomain.SyncedAttachment{{ProjectKey: "production", Attachment: edited}})
			require.NoError(t, err)

			toolRow := planAttachment(edited.ID("production"), &edited, &remote, attachmentFingerprint(t, remote), true)
			require.Equal(t, ActionUpdateServer, toolRow.Action)
			pull := PlannedResource{
				ID: testResourceID(), Action: ActionUpdateLocal, Local: &withoutTool, Server: &withTool,
				ServerFingerprint: fingerprintOf(t, withTool),
			}
			current := testAttachment(syncdomain.AttachmentTool, "old")
			api := &settleAPI{tool: attachmentMutationAPI{current: *current.Tool, version: 2, updateErr: test.updateErr}}

			outcomes, next, err := executePlan(root, store, syncapi.NewClient(api, "token", "https://example.com"),
				syncmanifest.New(), Plan{Resources: []PlannedResource{toolRow, pull}}, nil)

			assert.Equal(t, "new", *readLocalTool(t, root).Description, "the local edit stays")
			if test.wantError != "" {
				require.ErrorContains(t, err, test.wantError)
				assert.Equal(t, OutcomeFailed, outcomes[1].Status, "the pull must not run")
				return
			}
			require.NoError(t, err)
			assert.Equal(t, "new", *api.tool.current.Description)
			assert.Equal(t, attachmentFingerprint(t, edited), fingerprintIn(next, toolRow.ID))
			withEdit := withTool
			withEdit.Attachments = []syncdomain.Attachment{edited}
			assert.Equal(t, fingerprintOf(t, withEdit), fingerprintIn(next, pull.ID))
		})
	}
}

// settleAPI serves the tool "search" like attachmentMutationAPI, and records
// the body of each variation update.
type settleAPI struct {
	tool       attachmentMutationAPI
	variations []map[string]json.RawMessage
}

func (api *settleAPI) MakeRequest(
	accessToken, method, path, contentType string, query url.Values, body []byte, isBeta bool,
) ([]byte, error) {
	if strings.Contains(path, "/ai-tools/") {
		return api.tool.MakeRequest(accessToken, method, path, contentType, query, body, isBeta)
	}
	var request map[string]json.RawMessage
	if err := json.Unmarshal(body, &request); err != nil {
		return nil, err
	}
	api.variations = append(api.variations, request)
	return []byte(`{}`), nil
}

func (*settleAPI) MakeUnauthenticatedRequest(string, string, []byte) ([]byte, error) {
	return nil, nil
}

func fingerprintOf(t *testing.T, variation syncdomain.Variation) string {
	t.Helper()
	value, err := syncdomain.FingerprintVariation("production", "support/default", variation)
	require.NoError(t, err)
	return value
}

func fingerprintIn(manifest syncmanifest.Manifest, id ResourceID) string {
	for _, resource := range manifest.Resources {
		if resource.ID() == id {
			return resource.Fingerprint
		}
	}
	return ""
}

func readLocalTool(t *testing.T, root string) syncdomain.Tool {
	t.Helper()
	local, err := synclocal.CompileAttachments(root, func(syncdomain.ResourceID) bool { return true })
	require.NoError(t, err)
	require.Len(t, local, 1)
	return *local[0].Attachment.Tool
}
