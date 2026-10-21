package api

import (
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
	"github.com/launchdarkly/ldcli/internal/sync/api/apitest"
)

// headerServer records the headers of each request, by method and path.
type headerServer struct {
	url     string
	headers map[string]http.Header
}

func newHeaderServer(t *testing.T) *headerServer {
	t.Helper()
	recorder := &headerServer{headers: map[string]http.Header{}}
	server := apitest.NewServer(t, func(request apitest.Request) ([]byte, error) {
		recorder.headers[request.Method+" "+request.Path] = request.Header
		switch {
		case strings.HasSuffix(request.Path, "/configs/sync/manifests"):
			return []byte(`{"source":"git:example.com/acme/app","items":[]}`), nil
		case strings.HasSuffix(request.Path, "/configs/sync/settings"):
			return []byte(`{"items":[]}`), nil
		case strings.Contains(request.Path, "/ai-tools"):
			return []byte(`{"key":"search","schema":{},"version":1}`), nil
		default:
			return []byte(`{"key":"support","mode":"agent","variations":[]}`), nil
		}
	})
	recorder.url = server.URL
	return recorder
}

func (recorder *headerServer) client(syncSource string) Client {
	return NewTransport("1.2.3").Client(Options{BaseURI: recorder.url, AccessToken: "token", SyncSource: syncSource})
}

func TestClientSendsSyncHeadersOnEveryWriteAndNotOnReads(t *testing.T) {
	recorder := newHeaderServer(t)
	source := "git:example.com/acme/app"
	client := recorder.client(source)
	variation := testVariation(syncdomain.VariationModeAgent)
	tool := syncdomain.Attachment{
		Kind: syncdomain.AttachmentTool, Tool: &syncdomain.Tool{Key: "search", Schema: map[string]any{}},
	}
	ids := []syncdomain.ResourceID{syncdomain.VariationID("project", "support", "helpful")}

	require.NoError(t, discardVersion(client.CreateVariation("project", "support", variation)))
	require.NoError(t, discardVersion(client.UpdateVariation("project", "support", variation)))
	require.NoError(t, client.CreateAttachment("project", tool))
	require.NoError(t, client.UpdateAttachment("project", tool))
	_, err := client.PatchSyncManifest("project", source, nil, nil)
	require.NoError(t, err)
	require.NoError(t, client.SetSourceOfTruth("project", ids, syncdomain.SourceOfTruthCodeOnly))
	require.NoError(t, client.PostSyncReport("project", SyncReport{Source: source, Resources: []SyncReportResource{
		{ResourceKind: syncdomain.KindVariation, ResourceLookupKey: "support/helpful", Status: ReportStatusSucceeded},
	}}))
	writes := []string{
		"POST /api/v2/projects/project/ai-configs/support/variations",
		"PATCH /api/v2/projects/project/ai-configs/support/variations/helpful",
		"POST /api/v2/projects/project/ai-tools",
		"PATCH /api/v2/projects/project/ai-tools/search",
		"PATCH /api/v2/projects/project/configs/sync/manifests",
		"PATCH /api/v2/projects/project/configs/sync/settings",
		"POST /api/v2/projects/project/configs/sync/reports",
	}
	require.Len(t, recorder.headers, len(writes))
	for _, write := range writes {
		headers := recorder.headers[write]
		require.NotNil(t, headers, write)
		assert.Equal(t, []string{source}, headers.Values(SyncSourceHeader), write)
		assert.Empty(t, headers.Values("LD-Sync-Commit"), write)
		assert.Equal(t, "token", headers.Get("Authorization"), write)
		assert.Equal(t, "launchdarkly-cli/v1.2.3", headers.Get("User-Agent"), write)
		assert.Equal(t, "20240415", headers.Get("LD-API-Version"), write)
		assert.Equal(t, "application/json", headers.Get("Content-Type"), write)
	}

	recorder.headers = map[string]http.Header{}
	_, err = client.ReadVariation("project", "support", "helpful")
	require.NoError(t, err)
	_, err = client.GetSyncManifest("project", source)
	require.NoError(t, err)
	_, err = client.ResolveSourceOfTruth("project", ids)
	require.NoError(t, err)
	_, err = client.ReadAttachment("project", syncdomain.AttachmentTool, "search")
	require.NoError(t, err)
	require.Len(t, recorder.headers, 4)
	for read, headers := range recorder.headers {
		assert.Empty(t, headers.Values(SyncSourceHeader), read)
		assert.Equal(t, "token", headers.Get("Authorization"), read)
		assert.Equal(t, "launchdarkly-cli/v1.2.3", headers.Get("User-Agent"), read)
		assert.Equal(t, "20240415", headers.Get("LD-API-Version"), read)
	}
}
