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

type headerServer struct {
	url     string
	headers map[string]http.Header
}

func newHeaderServer(t *testing.T) *headerServer {
	t.Helper()
	recorder := &headerServer{headers: map[string]http.Header{}}
	server := apitest.NewServer(t, func(request apitest.Request) (int, []byte) {
		recorder.headers[request.Method+" "+request.Path] = request.Header
		switch {
		case strings.HasSuffix(request.Path, "/configs/sync/manifests"):
			return http.StatusOK, []byte(`{"source":"git:example.com/acme/app","items":[]}`)
		case strings.HasSuffix(request.Path, "/configs/sync/settings"):
			return http.StatusOK, []byte(`{"items":[]}`)
		case strings.Contains(request.Path, "/ai-tools"):
			return http.StatusOK, []byte(`{"key":"search","schema":{},"version":1}`)
		default:
			return http.StatusOK, []byte(`{"key":"support","mode":"agent","variations":[]}`)
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

	require.NoError(t, client.CreateVariation("project", "support", variation))
	require.NoError(t, client.UpdateVariation("project", "support", variation))
	require.NoError(t, client.CreateAttachment("project", tool))
	require.NoError(t, client.UpdateAttachment("project", tool))
	_, err := client.PatchSyncManifest("project", source, nil, nil)
	require.NoError(t, err)
	require.NoError(t, client.DeleteSourceOfTruth("project", ids))
	writes := []string{
		"POST /api/v2/projects/project/ai-configs/support/variations",
		"PATCH /api/v2/projects/project/ai-configs/support/variations/helpful",
		"POST /api/v2/projects/project/ai-tools",
		"PATCH /api/v2/projects/project/ai-tools/search",
		"PATCH /api/v2/projects/project/configs/sync/manifests",
		"PATCH /api/v2/projects/project/configs/sync/settings",
	}
	require.Len(t, recorder.headers, len(writes))
	for _, write := range writes {
		headers := recorder.headers[write]
		require.NotNil(t, headers, write)
		assert.Equal(t, []string{source}, headers.Values(SyncSourceHeader), write)
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
