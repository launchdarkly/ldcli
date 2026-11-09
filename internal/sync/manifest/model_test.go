package manifest

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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

func TestManifestTracksEachSharedAttachmentOnce(t *testing.T) {
	description := "Search documentation"
	attachment := syncdomain.Attachment{
		Kind: syncdomain.AttachmentTool,
		Tool: &syncdomain.Tool{Key: "search", Description: &description, Schema: map[string]any{"type": "object"}},
	}
	manifest := New()

	require.NoError(t, manifest.SetAttachments("project", []syncdomain.Attachment{attachment, attachment}))

	require.Len(t, manifest.Resources, 1)
	assert.Equal(t, syncdomain.KindTool, manifest.Resources[0].ResourceKind)
	assert.Equal(t, "search", manifest.Resources[0].LookupKey)
}

func TestManifestPreservesExistingAttachmentBaselineWhenAddingConsumer(t *testing.T) {
	description := "Current server content"
	attachment := syncdomain.Attachment{
		Kind: syncdomain.AttachmentTool,
		Tool: &syncdomain.Tool{Key: "search", Description: &description, Schema: map[string]any{"type": "object"}},
	}
	manifest := New()
	manifest.SetFingerprint(
		syncdomain.ResourceID{Kind: syncdomain.KindTool, ProjectKey: "project", LookupKey: "search"},
		"sha256:existing",
	)

	require.NoError(t, manifest.SetAttachmentsIfMissing("project", []syncdomain.Attachment{attachment}))

	require.Len(t, manifest.Resources, 1)
	assert.Equal(t, "sha256:existing", manifest.Resources[0].Fingerprint)
}

func TestWithChangesCopiesSourceMetadata(t *testing.T) {
	id := syncdomain.VariationID("project", "config", "default")
	fingerprint := "sha256:" + strings.Repeat("a", 64)
	before := Manifest{Resources: []Resource{{
		ResourceKind: id.Kind, ProjectKey: id.ProjectKey, LookupKey: id.LookupKey, Fingerprint: fingerprint,
	}}}
	remote := Manifest{Resources: []Resource{{
		ResourceKind: id.Kind, ProjectKey: id.ProjectKey, LookupKey: id.LookupKey, Fingerprint: fingerprint, Version: 3,
	}}}
	after := before.Clone()
	require.NoError(t, after.SetSource(id, &syncdomain.Reference{File: "prompt.md", Format: "markdown"}, "markdown/1", nil))

	result := remote.WithChanges(before, after)

	require.Len(t, result.Resources, 1)
	assert.Equal(t, "markdown/1", result.Resources[0].AdapterBehavior)
	assert.Equal(t, syncdomain.ProjectionFingerprintSchema, result.Resources[0].FingerprintSchema)
	assert.NotEmpty(t, result.Resources[0].ReferenceDigest)
	assert.Equal(t, 3, result.Resources[0].Version, "the remote version survives a content update")
}
