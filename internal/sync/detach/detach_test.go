package detach

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
	syncapi "github.com/launchdarkly/ldcli/internal/sync/api"
	synclocal "github.com/launchdarkly/ldcli/internal/sync/local"
	syncmanifest "github.com/launchdarkly/ldcli/internal/sync/manifest"
	syncreference "github.com/launchdarkly/ldcli/internal/sync/reference"
)

func TestLoadResourcesUnionsLocalAndManifestResources(t *testing.T) {
	root := t.TempDir()
	store := synclocal.NewStore(root)
	_, err := store.Add([]synclocal.VariationFile{{
		ProjectKey: "project", ConfigKey: "config", Variation: testVariation("local"),
	}})
	require.NoError(t, err)

	manifestStore := newMemoryManifestStore()
	require.NoError(t, manifestStore.Write(syncmanifest.Manifest{
		Resources: []syncmanifest.Resource{
			{
				ResourceKind: syncdomain.KindVariation, ProjectKey: "project", LookupKey: "config/manifest-only",
				Fingerprint: testFingerprint(),
			},
			{
				ResourceKind: syncdomain.KindTool, ProjectKey: "project", LookupKey: "search",
				Fingerprint: testFingerprint(),
			},
		},
	}))

	resources, _, err := loadResources(root, manifestStore, []string{"project"})

	require.NoError(t, err)
	assert.Equal(t, []syncdomain.ResourceID{
		{Kind: syncdomain.KindTool, ProjectKey: "project", LookupKey: "search"},
		{Kind: syncdomain.KindVariation, ProjectKey: "project", LookupKey: "config/local"},
		{Kind: syncdomain.KindVariation, ProjectKey: "project", LookupKey: "config/manifest-only"},
	}, resources)
}

// A tool keeps syncing on its own after the variation that used it is detached.
func TestDetachResourcesKeepsAttachmentManifestEntries(t *testing.T) {
	root := t.TempDir()
	store := synclocal.NewStore(root)
	description := "Search documentation"
	variation := testVariation("prompt")
	variation.Tools = []syncdomain.AttachmentRef{{Key: "search"}}
	variation.Attachments = []syncdomain.Attachment{{
		Kind: syncdomain.AttachmentTool,
		Tool: &syncdomain.Tool{Key: "search", Description: &description, Schema: map[string]any{"type": "object"}},
	}}
	_, err := store.Add([]synclocal.VariationFile{{ProjectKey: "project", ConfigKey: "config", Variation: variation}})
	require.NoError(t, err)

	manifestStore := newMemoryManifestStore()
	original := syncmanifest.Manifest{
		Resources: []syncmanifest.Resource{
			{
				ResourceKind: syncdomain.KindVariation, ProjectKey: "project", LookupKey: "config/prompt",
				Fingerprint: testFingerprint(),
			},
			{
				ResourceKind: syncdomain.KindTool, ProjectKey: "project", LookupKey: "search",
				Fingerprint: testFingerprint(),
			},
		},
	}
	require.NoError(t, manifestStore.Write(original))

	resource := syncdomain.ResourceID{Kind: syncdomain.KindVariation, ProjectKey: "project", LookupKey: "config/prompt"}
	err = detachResources(
		Options{RepositoryRoot: root, Store: store, Baselines: manifestStore},
		syncmanifest.Baseline{Lock: original},
		[]syncdomain.ResourceID{resource},
	)

	require.NoError(t, err)
	baseline, err := manifestStore.Load([]string{"project"})
	require.NoError(t, err)
	assert.Equal(t, []syncmanifest.Resource{original.Resources[1]}, baseline.Lock.Resources)
	_, err = os.Stat(filepath.Join(root, syncdomain.RootDir, "project", "tools", "search.json"))
	require.NoError(t, err, "the tool file stays")
}

func TestDetachResourcesRemovesWrapperAndManifestButKeepsReferencedFile(t *testing.T) {
	root := t.TempDir()
	referencePath := filepath.Join(root, "prompts", "prompt.md")
	require.NoError(t, os.MkdirAll(filepath.Dir(referencePath), 0o755))
	require.NoError(t, os.WriteFile(referencePath, []byte("Keep me.\n"), 0o644))

	store := synclocal.NewStore(root)
	variation := testVariation("prompt")
	_, err := store.Add([]synclocal.VariationFile{{
		ProjectKey: "project", ConfigKey: "config", Variation: variation,
		Ref: &synclocal.Reference{File: "prompts/prompt.md", Format: syncreference.PlainMarkdown},
	}})
	require.NoError(t, err)

	manifestStore := newMemoryManifestStore()
	original := syncmanifest.Manifest{
		Resources: []syncmanifest.Resource{{
			ResourceKind: syncdomain.KindVariation, ProjectKey: "project", LookupKey: "config/prompt",
			Fingerprint: testFingerprint(),
		}},
	}
	require.NoError(t, manifestStore.Write(original))

	resource := syncdomain.ResourceID{Kind: syncdomain.KindVariation, ProjectKey: "project", LookupKey: "config/prompt"}
	err = detachResources(
		Options{RepositoryRoot: root, Store: store, Baselines: manifestStore},
		syncmanifest.Baseline{Lock: original},
		[]syncdomain.ResourceID{resource},
	)

	require.NoError(t, err)
	exists, err := store.VariationExists("project", "config", "prompt")
	require.NoError(t, err)
	assert.False(t, exists)
	_, err = os.Stat(referencePath)
	require.NoError(t, err)
	baseline, err := manifestStore.Load([]string{"project"})
	manifest := baseline.Lock
	require.NoError(t, err)
	assert.Empty(t, manifest.Resources)
}

func TestDetachResourcesRemovesManifestEntryWhenWrapperWasAlreadyDeleted(t *testing.T) {
	root := t.TempDir()
	store := synclocal.NewStore(root)
	manifestStore := newMemoryManifestStore()
	original := syncmanifest.Manifest{
		Resources: []syncmanifest.Resource{{
			ResourceKind: syncdomain.KindVariation, ProjectKey: "project", LookupKey: "config/deleted",
			Fingerprint: testFingerprint(),
		}},
	}
	require.NoError(t, manifestStore.Write(original))

	resource := syncdomain.ResourceID{Kind: syncdomain.KindVariation, ProjectKey: "project", LookupKey: "config/deleted"}
	err := detachResources(
		Options{RepositoryRoot: root, Store: store, Baselines: manifestStore},
		syncmanifest.Baseline{Lock: original},
		[]syncdomain.ResourceID{resource},
	)

	require.NoError(t, err)
	baseline, err := manifestStore.Load([]string{"project"})
	manifest := baseline.Lock
	require.NoError(t, err)
	assert.Empty(t, manifest.Resources)
}

func TestDetachResourcesDeletesUnreadableWrapper(t *testing.T) {
	root := t.TempDir()
	wrapper := filepath.Join(root, syncdomain.RootDir, "project", "configs", "config", "broken.prompt.md")
	require.NoError(t, os.MkdirAll(filepath.Dir(wrapper), 0o755))
	require.NoError(t, os.WriteFile(wrapper, []byte("not front matter"), 0o644))

	store := synclocal.NewStore(root)
	manifestStore := newMemoryManifestStore()
	resource := syncdomain.ResourceID{Kind: syncdomain.KindVariation, ProjectKey: "project", LookupKey: "config/broken"}

	err := detachResources(
		Options{RepositoryRoot: root, Store: store, Baselines: manifestStore},
		syncmanifest.Baseline{Lock: syncmanifest.New()},
		[]syncdomain.ResourceID{resource},
	)

	require.NoError(t, err)
	baseline, loadErr := manifestStore.Load([]string{"project"})
	manifest := baseline.Lock
	require.NoError(t, loadErr)
	assert.Empty(t, manifest.Resources)
	_, statErr := os.Stat(wrapper)
	require.ErrorIs(t, statErr, os.ErrNotExist)
}

func TestRunRequiresTerminalWhenResourcesExist(t *testing.T) {
	root := t.TempDir()
	store := synclocal.NewStore(root)
	_, err := store.Add([]synclocal.VariationFile{{
		ProjectKey: "project", ConfigKey: "config", Variation: testVariation("prompt"),
	}})
	require.NoError(t, err)

	err = Run(Options{
		RepositoryRoot: root,
		Store:          store,
		Baselines:      newMemoryManifestStore(),
		Input:          bytes.NewBuffer(nil),
		Output:         bytes.NewBuffer(nil),
		Kind:           syncdomain.KindVariation,
	})

	require.ErrorContains(t, err, "requires a terminal")
}

func TestRunUsesExplicitSelectionsWithoutTerminal(t *testing.T) {
	root := t.TempDir()
	store := synclocal.NewStore(root)
	_, err := store.Add([]synclocal.VariationFile{{
		ProjectKey: "project", ConfigKey: "config", Variation: testVariation("prompt"),
	}})
	require.NoError(t, err)
	selection := syncdomain.ResourceID{
		Kind: syncdomain.KindVariation, ProjectKey: "project", LookupKey: "config/prompt",
	}
	manifestStore := newMemoryManifestStore()

	err = Run(Options{
		RepositoryRoot: root,
		Store:          store,
		Baselines:      manifestStore,
		Input:          bytes.NewBuffer(nil),
		Output:         bytes.NewBuffer(nil),
		Selections:     []syncdomain.ResourceID{selection},
		NoInput:        true,
		Yes:            true,
	})

	require.NoError(t, err)
	exists, err := store.VariationExists("project", "config", "prompt")
	require.NoError(t, err)
	assert.False(t, exists)
	assert.Equal(t, []string{"project"}, manifestStore.loadedProjectKeys)
}

func TestRunListsOnlyTheResourcesThatTheFilterKeeps(t *testing.T) {
	root := t.TempDir()
	store := synclocal.NewStore(root)
	_, err := store.Add([]synclocal.VariationFile{{
		ProjectKey: "project", ConfigKey: "config", Variation: testVariation("prompt"),
	}})
	require.NoError(t, err)
	options := Options{
		RepositoryRoot: root, Store: store, Baselines: newMemoryManifestStore(),
		Input: bytes.NewBuffer(nil), NoInput: true, Kind: syncdomain.KindVariation,
	}

	var otherProject bytes.Buffer
	options.Output, options.Filter = &otherProject, syncdomain.Filter{Project: "other"}
	require.NoError(t, Run(options))
	assert.Equal(t, "No resources are currently synced.\n", otherProject.String())

	options.Output, options.Filter = bytes.NewBuffer(nil), syncdomain.Filter{Project: "project"}
	require.EqualError(t, Run(options), "--key values are required with --no-input")

	options.Filter = syncdomain.Filter{}
	require.EqualError(t, Run(options), "--project and --key are required with --no-input")
}

func TestRunReportsWhenNoResourcesAreSynced(t *testing.T) {
	root := t.TempDir()
	var output bytes.Buffer

	err := Run(Options{
		RepositoryRoot: root,
		Store:          synclocal.NewStore(root),
		Baselines:      newMemoryManifestStore(),
		Input:          bytes.NewBuffer(nil),
		Output:         &output,
		Kind:           syncdomain.KindVariation,
	})

	require.NoError(t, err)
	assert.Equal(t, "No resources are currently synced.\n", output.String())
}

func TestRunRejectsExplicitSelectionWhenNoResourcesAreSynced(t *testing.T) {
	root := t.TempDir()
	var output bytes.Buffer

	err := Run(Options{
		RepositoryRoot: root,
		Store:          synclocal.NewStore(root),
		Baselines:      newMemoryManifestStore(),
		Input:          bytes.NewBuffer(nil),
		Output:         &output,
		Selections:     []syncdomain.ResourceID{syncdomain.VariationID("production", "support", "default")},
		NoInput:        true,
	})

	require.ErrorContains(t, err, "variation production/support/default is not synced")
	assert.Empty(t, output.String())
}

func TestRunArchive(t *testing.T) {
	notFound := errors.New(`{"code":"not_found","statusCode":404}`)
	tests := map[string]struct {
		archive      bool
		yes          bool
		archiver     recordingArchiver
		wantError    string
		wantArchived []string
		wantDetached bool
	}{
		"detach without archive keeps the variation in LaunchDarkly": {yes: true, wantDetached: true},
		"archive with yes":             {archive: true, yes: true, wantArchived: []string{"project/config/prompt"}, wantDetached: true},
		"variation already archived":   {archive: true, yes: true, archiver: recordingArchiver{preArchived: true}, wantDetached: true},
		"config gone in LaunchDarkly":  {archive: true, yes: true, archiver: recordingArchiver{readErr: notFound}, wantDetached: true},
		"detach needs a confirmation":  {wantError: "rerun with --yes"},
		"archive needs a confirmation": {archive: true, wantError: "rerun with --yes"},
		"archive failure stops detach": {
			archive: true, yes: true, archiver: recordingArchiver{err: errors.New("forbidden")},
			wantError: "archive variation project/config/prompt",
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			workspace := newArchiveWorkspace(t, "prompt")
			archiver := &test.archiver
			options := workspace.options(archiver)
			options.Archive, options.Yes = test.archive, test.yes

			err := Run(options)

			if test.wantError != "" {
				require.ErrorContains(t, err, test.wantError)
			} else {
				require.NoError(t, err)
			}
			assert.Equal(t, test.wantArchived, archiver.archived)
			workspace.requireTracked(t, "prompt", !test.wantDetached)
		})
	}
}

// A failure on the second variation stops the detach of both. The next run
// skips the variation that is already archived and finishes.
func TestRunArchiveWithSeveralSelectionsCanRunAgainAfterAFailure(t *testing.T) {
	workspace := newArchiveWorkspace(t, "first", "second")
	archiver := &recordingArchiver{errFor: map[string]error{"project/config/second": errors.New("forbidden")}}
	options := workspace.options(archiver)
	options.Archive, options.Yes = true, true

	require.ErrorContains(t, Run(options), "archive variation project/config/second: forbidden")
	assert.Equal(t, []string{"project/config/first"}, archiver.archived)
	workspace.requireTracked(t, "first", true)
	workspace.requireTracked(t, "second", true)

	archiver.errFor = nil
	require.NoError(t, Run(options))
	assert.Equal(t, []string{"project/config/first", "project/config/second"}, archiver.archived)
	workspace.requireTracked(t, "first", false)
	workspace.requireTracked(t, "second", false)
}

// The file delete runs after the archive and the baseline save. When it
// fails, detach restores the baseline, and the next run finishes.
func TestRunArchiveCanRunAgainAfterTheDeleteFails(t *testing.T) {
	workspace := newArchiveWorkspace(t, "prompt")
	archiver := &recordingArchiver{}
	options := workspace.options(archiver)
	options.Archive, options.Yes = true, true

	// A directory in place of the variation file makes the delete fail.
	variationFile := filepath.Join(workspace.root, syncdomain.RootDir, "project", "configs", "config", "prompt.prompt.md")
	content, err := os.ReadFile(variationFile)
	require.NoError(t, err)
	require.NoError(t, os.Remove(variationFile))
	require.NoError(t, os.Mkdir(variationFile, 0o755))

	require.ErrorContains(t, Run(options), "is not a regular file")
	assert.Equal(t, []string{"project/config/prompt"}, archiver.archived)
	assert.NotEmpty(t, workspace.baselines.manifest.Resources)

	require.NoError(t, os.Remove(variationFile))
	require.NoError(t, os.WriteFile(variationFile, content, 0o644))
	require.NoError(t, Run(options))
	assert.Equal(t, []string{"project/config/prompt"}, archiver.archived)
	workspace.requireTracked(t, "prompt", false)
}

// One question covers the detach and the archive.
func TestRunArchiveAsksForConfirmation(t *testing.T) {
	tests := map[string]struct {
		answer       string
		wantArchived []string
		wantOutput   string
	}{
		"yes archives":         {answer: "y\n", wantArchived: []string{"project/config/prompt"}, wantOutput: "Detached and archived resources:"},
		"no changes nothing":   {answer: "n\n", wantOutput: "Detach canceled."},
		"empty answer is a no": {answer: "\n", wantOutput: "Detach canceled."},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			workspace := newArchiveWorkspace(t, "prompt")
			archiver := &recordingArchiver{}
			var output bytes.Buffer
			options := workspace.options(archiver)
			options.Archive, options.NoInput = true, false
			options.Input, options.Output = strings.NewReader(test.answer), &output
			options.isTerminal = func(io.Reader, io.Writer) bool { return true }

			require.NoError(t, Run(options))

			assert.Contains(t, output.String(), "Resources to detach and archive in LaunchDarkly:\n  variation project/config/prompt")
			assert.Equal(t, 1, strings.Count(output.String(), "[y/N]"))
			assert.Contains(t, output.String(), "Detach these resources? [y/N]")
			assert.Contains(t, output.String(), test.wantOutput)
			assert.Equal(t, test.wantArchived, archiver.archived)
			workspace.requireTracked(t, "prompt", test.wantArchived == nil)
		})
	}
}

func TestRunDetachesToolsAndSkills(t *testing.T) {
	workspace := newArchiveWorkspace(t, "prompt")
	tool, skill := testTool(), testSkill()
	workspace.addAttachments(t, tool, skill)
	archiver := &recordingArchiver{}
	options := workspace.options(archiver)
	options.Selections = []syncdomain.ResourceID{tool.ID("project"), skill.ID("project")}
	options.Yes = true

	require.NoError(t, Run(options))

	assert.Empty(t, archiver.archived, "LaunchDarkly keeps the tool and the skill")
	assert.Equal(t, "Detached resources:\n- tool project/search\n- skill project/summarize\n", workspace.output(options))
	local, err := synclocal.CompileAttachments(workspace.root, func(syncdomain.ResourceID) bool { return true })
	require.NoError(t, err)
	assert.Empty(t, local)
	assert.Equal(t, []syncdomain.ResourceID{syncdomain.VariationID("project", "config", "prompt")}, lockIDs(workspace.baselines.manifest))
}

func TestRunRefusesAToolThatAVariationStillUses(t *testing.T) {
	workspace := newArchiveWorkspace(t)
	tool := testTool()
	variation := testVariation("prompt")
	variation.Tools = []syncdomain.AttachmentRef{{Key: "search"}}
	variation.Attachments = []syncdomain.Attachment{tool}
	_, err := workspace.store.Add([]synclocal.VariationFile{{ProjectKey: "project", ConfigKey: "config", Variation: variation}})
	require.NoError(t, err)
	prompt := syncdomain.VariationID("project", "config", "prompt")
	workspace.baselines.manifest.SetFingerprint(prompt, testFingerprint())
	require.NoError(t, workspace.baselines.manifest.SetAttachments("project", variation.Attachments))
	options := workspace.options(&recordingArchiver{})
	options.Yes = true

	options.Selections = []syncdomain.ResourceID{tool.ID("project")}
	require.EqualError(t, Run(options),
		"tool project/search is used by variation project/config/prompt. Remove it from that variation first.")
	assert.Len(t, workspace.baselines.manifest.Resources, 2, "nothing changes")

	options.Selections = []syncdomain.ResourceID{prompt, tool.ID("project")}
	require.NoError(t, Run(options), "a detach of the variation too frees the tool")
	assert.Empty(t, workspace.baselines.manifest.Resources)
}

func TestRunMarksResourcesWithLocalChanges(t *testing.T) {
	workspace := newArchiveWorkspace(t, "edited", "synced")
	tool := testTool()
	workspace.addAttachments(t, tool)
	variations, err := synclocal.CompileWorkspace(workspace.root)
	require.NoError(t, err)
	synced := syncdomain.VariationID("project", "config", "synced")
	fingerprint, err := syncdomain.FingerprintVariation("project", synced.LookupKey, variations[1].Variation)
	require.NoError(t, err)
	workspace.baselines.manifest.SetFingerprint(synced, fingerprint)
	lockOnly := syncdomain.VariationID("project", "config", "lock-only")
	workspace.baselines.manifest.SetFingerprint(lockOnly, testFingerprint())

	var output bytes.Buffer
	options := workspace.options(&recordingArchiver{})
	options.Selections = append(options.Selections, tool.ID("project"), lockOnly)
	options.NoInput, options.Input, options.Output = false, strings.NewReader("n\n"), &output
	options.isTerminal = func(io.Reader, io.Writer) bool { return true }

	require.NoError(t, Run(options))

	assert.Equal(t, "Resources to detach:\n"+
		"  variation project/config/edited (local changes will be lost)\n"+
		"  variation project/config/synced\n"+
		"  tool project/search\n"+
		"  variation project/config/lock-only\n"+
		"\nDetach these resources? [y/N] Detach canceled.\n", output.String())
	assert.Len(t, workspace.baselines.manifest.Resources, 4, "a no changes nothing")
}

// archiveWorkspace is a workspace whose variations have local files and
// baseline entries.
type archiveWorkspace struct {
	root      string
	store     synclocal.Store
	baselines *memoryManifestStore
	keys      []string
}

func newArchiveWorkspace(t *testing.T, keys ...string) archiveWorkspace {
	t.Helper()
	workspace := archiveWorkspace{root: t.TempDir(), baselines: newMemoryManifestStore(), keys: keys}
	workspace.store = synclocal.NewStore(workspace.root)
	for _, key := range keys {
		_, err := workspace.store.Add([]synclocal.VariationFile{{
			ProjectKey: "project", ConfigKey: "config", Variation: testVariation(key),
		}})
		require.NoError(t, err)
		workspace.baselines.manifest.SetFingerprint(syncdomain.VariationID("project", "config", key), testFingerprint())
	}
	return workspace
}

func (workspace archiveWorkspace) options(archiver Archiver) Options {
	selections := make([]syncdomain.ResourceID, 0, len(workspace.keys))
	for _, key := range workspace.keys {
		selections = append(selections, syncdomain.VariationID("project", "config", key))
	}
	return Options{
		RepositoryRoot: workspace.root,
		Store:          workspace.store,
		Baselines:      workspace.baselines,
		Input:          bytes.NewBuffer(nil),
		Output:         bytes.NewBuffer(nil),
		Selections:     selections,
		NoInput:        true,
		Archiver:       archiver,
	}
}

// addAttachments writes standalone tool and skill files with baseline entries
// that match them.
func (workspace archiveWorkspace) addAttachments(t *testing.T, attachments ...syncdomain.Attachment) {
	t.Helper()
	synced := make([]syncdomain.SyncedAttachment, 0, len(attachments))
	for _, attachment := range attachments {
		synced = append(synced, syncdomain.SyncedAttachment{ProjectKey: "project", Attachment: attachment})
	}
	_, err := workspace.store.ReplaceAttachments(synced)
	require.NoError(t, err)
	require.NoError(t, workspace.baselines.manifest.SetAttachments("project", attachments))
}

// output returns what Run wrote to the output of options.
func (archiveWorkspace) output(options Options) string {
	return options.Output.(*bytes.Buffer).String()
}

func lockIDs(manifest syncmanifest.Manifest) []syncdomain.ResourceID {
	ids := make([]syncdomain.ResourceID, 0, len(manifest.Resources))
	for _, resource := range manifest.Resources {
		ids = append(ids, resource.ID())
	}
	return ids
}

func testTool() syncdomain.Attachment {
	description := "Search documentation"
	return syncdomain.Attachment{
		Kind: syncdomain.AttachmentTool,
		Tool: &syncdomain.Tool{Key: "search", Description: &description, Schema: map[string]any{"type": "object"}},
	}
}

func testSkill() syncdomain.Attachment {
	return syncdomain.Attachment{
		Kind:  syncdomain.AttachmentSkill,
		Skill: &syncdomain.Skill{Key: "summarize", Description: "Summarize", Markdown: "# Summarize\n"},
	}
}

// requireTracked checks both the local file and the baseline entry.
func (workspace archiveWorkspace) requireTracked(t *testing.T, key string, tracked bool) {
	t.Helper()
	exists, err := workspace.store.VariationExists("project", "config", key)
	require.NoError(t, err)
	assert.Equal(t, tracked, exists, "local file of %s", key)
	id := syncdomain.VariationID("project", "config", key)
	inBaseline := slices.ContainsFunc(workspace.baselines.manifest.Resources, func(resource syncmanifest.Resource) bool {
		return resource.ID() == id
	})
	assert.Equal(t, tracked, inBaseline, "baseline entry of %s", key)
}

// The archive runs before the baseline save. When the save fails, the same
// command must finish on its next run, although the variation is archived.
func TestRunArchiveCanRunAgainAfterALaterStepFails(t *testing.T) {
	workspace := newArchiveWorkspace(t, "prompt")
	archiver := &recordingArchiver{}
	options := workspace.options(archiver)
	options.Archive, options.Yes = true, true

	workspace.baselines.saveErr = errors.New("save baseline")
	require.ErrorContains(t, Run(options), "save baseline")
	assert.Equal(t, []string{"project/config/prompt"}, archiver.archived)
	workspace.requireTracked(t, "prompt", true)

	workspace.baselines.saveErr = nil
	require.NoError(t, Run(options))
	assert.Equal(t, []string{"project/config/prompt"}, archiver.archived)
	workspace.requireTracked(t, "prompt", false)
}

func TestRunArchiveStopsWhenTheContextEnds(t *testing.T) {
	workspace := newArchiveWorkspace(t, "prompt")
	archiver := &recordingArchiver{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	options := workspace.options(archiver)
	options.Archive, options.Yes, options.Context = true, true, ctx

	require.ErrorIs(t, Run(options), context.Canceled)
	assert.Empty(t, archiver.archived)
	workspace.requireTracked(t, "prompt", true)
}

// recordingArchiver behaves like the API: an archived variation reads as
// absent, and a second archive of it fails.
type recordingArchiver struct {
	archived    []string
	err         error
	errFor      map[string]error
	readErr     error
	preArchived bool
}

func (archiver *recordingArchiver) ReadVariation(projectKey, configKey, variationKey string) (syncapi.VariationState, error) {
	exists := !archiver.preArchived && !slices.Contains(archiver.archived, projectKey+"/"+configKey+"/"+variationKey)
	return syncapi.VariationState{Exists: exists}, archiver.readErr
}

func (archiver *recordingArchiver) ArchiveVariation(projectKey, configKey, variationKey string) error {
	id := projectKey + "/" + configKey + "/" + variationKey
	if archiver.preArchived || slices.Contains(archiver.archived, id) {
		return errors.New(`{"code":"invalid_request","message":"cannot archive an archived variation","statusCode":400}`)
	}
	if archiver.err != nil {
		return archiver.err
	}
	if err := archiver.errFor[id]; err != nil {
		return err
	}
	archiver.archived = append(archiver.archived, id)
	return nil
}

type memoryManifestStore struct {
	manifest          syncmanifest.Manifest
	loadedProjectKeys []string
	saveErr           error
}

func newMemoryManifestStore() *memoryManifestStore {
	return &memoryManifestStore{manifest: syncmanifest.New()}
}

func (store *memoryManifestStore) Load(projectKeys []string) (syncmanifest.Baseline, error) {
	store.loadedProjectKeys = append([]string(nil), projectKeys...)
	return syncmanifest.Baseline{Lock: store.manifest}, nil
}

func (store *memoryManifestStore) Save(_ syncmanifest.Baseline, next syncmanifest.Manifest) (syncmanifest.Baseline, error) {
	if store.saveErr != nil {
		return syncmanifest.Baseline{}, store.saveErr
	}
	store.manifest = next
	return syncmanifest.Baseline{Lock: next}, nil
}

func (store *memoryManifestStore) Write(manifest syncmanifest.Manifest) error {
	store.manifest = manifest
	return nil
}

func testVariation(key string) syncdomain.Variation {
	return syncdomain.Variation{Mode: syncdomain.VariationModeAgent, Key: key, Name: key, Instructions: "Help."}
}

func testFingerprint() string {
	return "sha256:" + strings.Repeat("0", 64)
}
