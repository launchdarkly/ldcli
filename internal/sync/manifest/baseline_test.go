package manifest

import (
	"io/fs"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
	syncapi "github.com/launchdarkly/ldcli/internal/sync/api"
)

const testSource = "git:example/repo"

func TestLockFormatIsStable(t *testing.T) {
	content, err := encodeLock(Manifest{Resources: []Resource{
		{ResourceKind: syncdomain.KindVariation, ProjectKey: "production", LookupKey: "support/default", Fingerprint: fingerprint("a"), Version: 6},
		{ResourceKind: syncdomain.KindTool, ProjectKey: "production", LookupKey: "search", Fingerprint: fingerprint("b"), Version: 2},
	}})

	require.NoError(t, err)
	assert.Equal(t, `# Written by ldcli sync. Commit this file with the .launchdarkly files.
formatVersion: 1
resources:
  - kind: tool
    project: production
    key: search
    fingerprint: `+fingerprint("b")+`
  - kind: variation
    project: production
    key: support/default
    fingerprint: `+fingerprint("a")+`
`, string(content))

	decoded, err := decodeLock(content)
	require.NoError(t, err)
	assert.Len(t, decoded.Resources, 2)
}

func TestDecodeLockRejectsInvalidContent(t *testing.T) {
	for name, content := range map[string]string{
		"unknown field":   "formatVersion: 1\nresources: []\nextra: true\n",
		"unknown version": "formatVersion: 2\nresources: []\n",
		"bad fingerprint": "formatVersion: 1\nresources:\n  - kind: tool\n    project: p\n    key: k\n    fingerprint: x\n",
	} {
		_, err := decodeLock([]byte(content))
		assert.Error(t, err, name)
	}
}

// Other branches add entries to the shared remote manifest. A working copy
// without a sync.lock file must not track them.
func TestBaselineStoreWithoutLockFileTracksNothing(t *testing.T) {
	client := &manifestClient{
		manifests:      map[string]syncapi.SyncManifest{"production": remoteManifest(fingerprint("a"), 3)},
		patchResponses: []syncapi.SyncManifest{remoteManifest(fingerprint("b"), 4)},
	}
	lock := &memoryLock{}
	store := NewBaselineStore(NewStore(client, testSource), lock)

	baseline, err := store.Load([]string{"production"})

	require.NoError(t, err)
	assert.Empty(t, baseline.Lock.Resources)
	assert.False(t, baseline.Stale(variationID()))

	// A new entry for a resource that the remote manifest has must send the
	// remote version that sync read.
	next := baseline.Lock.Clone()
	next.SetFingerprint(variationID(), fingerprint("b"))
	_, err = store.Save(baseline, next)
	require.NoError(t, err)
	require.Len(t, client.patches, 1)
	assert.Equal(t, []syncapi.SyncManifestUpsert{
		{ResourceKind: syncdomain.KindVariation, ResourceLookupKey: "support/default", Fingerprint: fingerprint("b"), Version: 3},
	}, client.patches[0].upserts)
	assert.NotNil(t, lock.content)
}

func TestBaselineStoreUsesLockFileAsBaselineAndReportsStaleness(t *testing.T) {
	client := &manifestClient{manifests: map[string]syncapi.SyncManifest{"production": remoteManifest(fingerprint("b"), 5)}}
	lock := lockWith(t, fingerprint("a"), 4)

	baseline, err := NewBaselineStore(NewStore(client, testSource), lock).Load(nil)

	require.NoError(t, err)
	assert.Equal(t, fingerprint("a"), baseline.Lock.Resources[0].Fingerprint)
	assert.True(t, baseline.Stale(variationID()))
}

func TestBaselineStaleComparesFingerprints(t *testing.T) {
	tests := map[string]struct {
		remote syncapi.SyncManifest
		stale  bool
	}{
		"newer version with a different fingerprint": {remote: remoteManifest(fingerprint("b"), 5), stale: true},
		"newer version with the same fingerprint":    {remote: remoteManifest(fingerprint("a"), 5)},
		"same version with a different fingerprint":  {remote: remoteManifest(fingerprint("b"), 4), stale: true},
		"remote entry removed":                       {remote: syncapi.SyncManifest{Source: testSource}},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			client := &manifestClient{manifests: map[string]syncapi.SyncManifest{"production": test.remote}}
			baseline, err := NewBaselineStore(NewStore(client, testSource), lockWith(t, fingerprint("a"), 4)).Load(nil)

			require.NoError(t, err)
			assert.Equal(t, test.stale, baseline.Stale(variationID()))
		})
	}
}

// The version in each request must be the version that sync read from
// LaunchDarkly, so that LaunchDarkly rejects a save that races another save.
func TestBaselineStoreSaveSendsRemoteVersionsAndWritesLockWithoutThem(t *testing.T) {
	client := &manifestClient{
		manifests: map[string]syncapi.SyncManifest{"production": remoteManifest(fingerprint("b"), 5)},
		patchResponses: []syncapi.SyncManifest{{
			Source: testSource,
			Items: []syncapi.SyncManifestResource{
				{ResourceKind: syncdomain.KindVariation, ResourceLookupKey: "support/default", Fingerprint: fingerprint("c"), Version: 6},
				{ResourceKind: syncdomain.KindTool, ResourceLookupKey: "search", Fingerprint: fingerprint("d"), Version: 1},
			},
		}},
	}
	lock := lockWith(t, fingerprint("a"), 4)
	store := NewBaselineStore(NewStore(client, testSource), lock)
	baseline, err := store.Load(nil)
	require.NoError(t, err)

	next := baseline.Lock.Clone()
	next.SetFingerprint(variationID(), fingerprint("c"))
	next.SetFingerprint(syncdomain.ResourceID{Kind: syncdomain.KindTool, ProjectKey: "production", LookupKey: "search"}, fingerprint("d"))
	saved, err := store.Save(baseline, next)

	require.NoError(t, err)
	require.Len(t, client.patches, 1)
	assert.Equal(t, []syncapi.SyncManifestUpsert{
		{ResourceKind: syncdomain.KindTool, ResourceLookupKey: "search", Fingerprint: fingerprint("d"), Version: 0},
		{ResourceKind: syncdomain.KindVariation, ResourceLookupKey: "support/default", Fingerprint: fingerprint("c"), Version: 5},
	}, client.patches[0].upserts)
	written, err := ReadLock(lock)
	require.NoError(t, err)
	assert.Equal(t, saved.Lock, written)
	assert.NotContains(t, string(lock.content), "version:")
	assert.False(t, saved.Stale(variationID()))
}

func TestBaselineStoreSaveKeepsRemoteEntryThatThisCopyDidNotChange(t *testing.T) {
	client := &manifestClient{manifests: map[string]syncapi.SyncManifest{"production": remoteManifest(fingerprint("b"), 5)}}
	lock := lockWith(t, fingerprint("a"), 4)
	store := NewBaselineStore(NewStore(client, testSource), lock)
	baseline, err := store.Load(nil)
	require.NoError(t, err)

	_, err = store.Save(baseline, baseline.Lock)

	require.NoError(t, err)
	assert.Empty(t, client.patches)
}

func TestBaselineStoreSaveDoesNotWriteLockWhenRemoteSaveFails(t *testing.T) {
	client := &manifestClient{
		manifests: map[string]syncapi.SyncManifest{"production": remoteManifest(fingerprint("a"), 4)},
		patchErr:  uncertainPatchError(t),
	}
	lock := lockWith(t, fingerprint("a"), 4)
	before := string(lock.content)
	store := NewBaselineStore(NewStore(client, testSource), lock)
	baseline, err := store.Load(nil)
	require.NoError(t, err)

	next := baseline.Lock.Clone()
	next.SetFingerprint(variationID(), fingerprint("c"))
	_, err = store.Save(baseline, next)

	require.Error(t, err)
	assert.Equal(t, before, string(lock.content))
}

func TestBaselineStoreSaveRemovesEmptyLock(t *testing.T) {
	client := &manifestClient{
		manifests:      map[string]syncapi.SyncManifest{"production": remoteManifest(fingerprint("a"), 4)},
		patchResponses: []syncapi.SyncManifest{{Source: testSource}},
	}
	lock := lockWith(t, fingerprint("a"), 4)
	store := NewBaselineStore(NewStore(client, testSource), lock)
	baseline, err := store.Load(nil)
	require.NoError(t, err)

	_, err = store.Save(baseline, New())

	require.NoError(t, err)
	assert.Nil(t, lock.content)
	assert.Equal(t, []syncapi.SyncManifestDeletion{{
		ResourceKind: syncdomain.KindVariation, ResourceLookupKey: "support/default", Version: 4,
	}}, client.patches[0].deletions)
}

func variationID() syncdomain.ResourceID {
	return syncdomain.VariationID("production", "support", "default")
}

func remoteManifest(fingerprint string, version int) syncapi.SyncManifest {
	return syncapi.SyncManifest{Source: testSource, Items: []syncapi.SyncManifestResource{{
		ResourceKind: syncdomain.KindVariation, ResourceLookupKey: "support/default", Fingerprint: fingerprint, Version: version,
	}}}
}

func lockWith(t *testing.T, fingerprint string, version int) *memoryLock {
	t.Helper()
	content, err := encodeLock(Manifest{Resources: []Resource{{
		ResourceKind: syncdomain.KindVariation, ProjectKey: "production", LookupKey: "support/default",
		Fingerprint: fingerprint, Version: version,
	}}})
	require.NoError(t, err)
	return &memoryLock{content: content}
}

type memoryLock struct {
	content []byte
}

func (lock *memoryLock) ReadLock() ([]byte, error) {
	if lock.content == nil {
		return nil, fs.ErrNotExist
	}
	return lock.content, nil
}

func (lock *memoryLock) WriteLock(content []byte) error {
	lock.content = content
	return nil
}

func TestLockKeepsTheLinkOfAVariation(t *testing.T) {
	manifest := Manifest{Resources: []Resource{{
		ResourceKind: syncdomain.KindVariation, ProjectKey: "production", LookupKey: "support/default",
		Fingerprint: fingerprint("a"), Version: 1,
	}}}
	ref := &syncdomain.Reference{File: "prompts/support.md", Format: "plain-markdown"}
	manifest.SetRefs([]syncdomain.SyncedResource{{
		Kind: syncdomain.KindVariation, ProjectKey: "production", LookupKey: "support/default", Ref: ref,
	}})

	content, err := encodeLock(manifest)
	require.NoError(t, err)
	assert.Contains(t, string(content), "    ref:\n      file: prompts/support.md\n      format: plain-markdown\n")
	decoded, err := decodeLock(content)
	require.NoError(t, err)
	assert.Equal(t, ref, decoded.Ref(variationID()))

	// A variation without a local file keeps its recorded link.
	manifest.SetRefs(nil)
	assert.Equal(t, ref, manifest.Ref(variationID()))
}
