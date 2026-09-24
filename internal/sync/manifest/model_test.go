package manifest

import (
	"testing"

	"github.com/stretchr/testify/assert"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
)

func TestManifestSetFingerprintAndRemove(t *testing.T) {
	first := syncdomain.ResourceID{Kind: syncdomain.KindVariation, ProjectKey: "project", LookupKey: "config/first"}
	second := syncdomain.ResourceID{Kind: syncdomain.KindVariation, ProjectKey: "project", LookupKey: "config/second"}
	manifest := New()

	manifest.SetFingerprint(first, "sha256:first")
	manifest.SetFingerprint(first, "sha256:updated")
	manifest.SetFingerprint(second, "sha256:second")
	manifest.Remove(first)

	assert.Equal(t, []Resource{{
		ResourceKind: second.Kind,
		ProjectKey:   second.ProjectKey,
		LookupKey:    second.LookupKey,
		Fingerprint:  "sha256:second",
	}}, manifest.Resources)
}
