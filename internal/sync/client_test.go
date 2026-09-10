package sync

import (
	"encoding/json"
	"errors"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/launchdarkly/ldcli/internal/resources"
)

type apiRequest struct {
	AccessToken string
	Method      string
	Path        string
	Body        []byte
}

type apiClientStub struct {
	Requests  []apiRequest
	Responses [][]byte
	Err       error
}

var _ resources.Client = &apiClientStub{}

func (c *apiClientStub) MakeRequest(
	accessToken string,
	method string,
	path string,
	_ string,
	_ url.Values,
	body []byte,
	_ bool,
) ([]byte, error) {
	c.Requests = append(c.Requests, apiRequest{
		AccessToken: accessToken,
		Method:      method,
		Path:        path,
		Body:        append([]byte(nil), body...),
	})
	if c.Err != nil {
		return nil, c.Err
	}

	return c.Responses[len(c.Requests)-1], nil
}

func (*apiClientStub) MakeUnauthenticatedRequest(string, string, []byte) ([]byte, error) {
	return nil, nil
}

func TestAPIClient_Status(t *testing.T) {
	transport := &apiClientStub{
		Responses: [][]byte{
			[]byte(`[{"resourceKind":"variation","lookupKey":"config/first","status":"local_changed","syncDirection":"code_canonical"}]`),
			[]byte(`[{"resourceKind":"tool","lookupKey":"search/2","status":"in_sync","syncDirection":"both"}]`),
		},
	}
	client := NewAPIClient(transport)

	statuses, err := client.Status(
		"token",
		"https://example.com",
		"launchdarkly/ldcli",
		[]SyncedResource{
			{
				ProjectKey:  "alpha",
				Kind:        KindVariation,
				LookupKey:   "config/first",
				Fingerprint: "sha256.first",
				Upsert:      true,
			},
			{
				ProjectKey:  "zeta",
				Kind:        KindTool,
				LookupKey:   "search/2",
				Fingerprint: "sha256.second",
			},
		},
	)
	require.NoError(t, err)
	require.Len(t, transport.Requests, 2)
	require.Len(t, statuses, 2)

	assert.Equal(t, "POST", transport.Requests[0].Method)
	assert.Equal(t, "token", transport.Requests[0].AccessToken)
	assert.Equal(t, "https://example.com/api/v2/projects/alpha/ai-configs/sync/status", transport.Requests[0].Path)
	assert.Equal(t, "alpha", statuses[0].ProjectKey)
	assert.Equal(t, "zeta", statuses[1].ProjectKey)

	var request statusRequest
	require.NoError(t, json.Unmarshal(transport.Requests[0].Body, &request))
	assert.Equal(t, "launchdarkly/ldcli", request.RepoIdentifier)
	require.Len(t, request.Resources, 1)
	assert.Equal(t, KindVariation, request.Resources[0].ResourceKind)
	assert.Equal(t, "config/first", request.Resources[0].LookupKey)
	assert.Equal(t, Fingerprint("sha256.first"), request.Resources[0].Fingerprint)
	assert.True(t, request.Resources[0].Upsert)
}

func TestAPIClient_StatusTransportError(t *testing.T) {
	client := NewAPIClient(&apiClientStub{Err: errors.New("unavailable")})

	_, err := client.Status(
		"token",
		"https://example.com",
		"launchdarkly/ldcli",
		[]SyncedResource{{ProjectKey: "proj"}},
	)
	require.ErrorContains(t, err, "unavailable")
}

func TestAPIClient_StatusInvalidResponse(t *testing.T) {
	client := NewAPIClient(&apiClientStub{Responses: [][]byte{[]byte(`not json`)}})

	_, err := client.Status(
		"token",
		"https://example.com",
		"launchdarkly/ldcli",
		[]SyncedResource{{ProjectKey: "proj"}},
	)
	require.ErrorContains(t, err, "decode status response")
}
