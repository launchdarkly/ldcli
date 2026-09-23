package prompt

import (
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
