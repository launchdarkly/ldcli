package prompt

import (
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

	_, updatedManifest, err := executePlan("", synclocal.Store{}, syncapi.Client{}, reviewedManifest, plan)

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

	err := applyServerChange(syncapi.NewClient(transport, "token", "https://example.com"), resource)

	require.ErrorContains(t, err, `"statusCode":400`)
	assert.Zero(t, transport.reads)
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
