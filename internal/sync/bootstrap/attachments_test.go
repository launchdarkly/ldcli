package bootstrap

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
	syncapi "github.com/launchdarkly/ldcli/internal/sync/api"
	synclocal "github.com/launchdarkly/ldcli/internal/sync/local"
	syncmanifest "github.com/launchdarkly/ldcli/internal/sync/manifest"
)

func TestRunAddsToolsAndSkillsFromLaunchDarkly(t *testing.T) {
	for _, test := range []struct {
		kind       syncdomain.Kind
		initial    bool
		wantOutput string
	}{
		{kind: syncdomain.KindTool, initial: true, wantOutput: "Bootstrapped 1 tool file in .launchdarkly.\n"},
		{kind: syncdomain.KindSkill, wantOutput: "Added 1 skill file in .launchdarkly.\n"},
	} {
		t.Run(string(test.kind), func(t *testing.T) {
			root := t.TempDir()
			attachment := testAttachment(test.kind)
			id := attachment.ID("project")
			baselines := &memoryManifestStore{manifest: syncmanifest.New()}
			var output bytes.Buffer

			err := Run(Options{
				Kind: test.kind, Attachments: &fakeAttachments{attachments: []syncdomain.Attachment{attachment}},
				Store: synclocal.NewStore(root), Baselines: baselines, Output: &output, Initial: test.initial,
				Selections: []syncdomain.ResourceID{id}, NoInput: true,
			})

			require.NoError(t, err)
			assert.Equal(t, test.wantOutput, output.String())
			local, err := synclocal.CompileAttachments(root, func(syncdomain.ResourceID) bool { return true })
			require.NoError(t, err)
			require.Len(t, local, 1)
			assert.True(t, syncdomain.SameAttachmentContent(attachment, local[0].Attachment))
			fingerprint, err := syncdomain.FingerprintAttachment("project", attachment)
			require.NoError(t, err)
			assert.Equal(t, []syncmanifest.Resource{{
				ResourceKind: test.kind, ProjectKey: "project", LookupKey: id.LookupKey, Fingerprint: fingerprint,
			}}, baselines.manifest.Resources)
		})
	}
}

func TestRunRefusesAToolBeforeAnyWrite(t *testing.T) {
	search := testAttachment(syncdomain.KindTool)
	id := search.ID("project")
	tests := map[string]struct {
		local        bool
		tracked      bool
		launchDarkly []syncdomain.Attachment
		want         string
	}{
		"missing in LaunchDarkly": {want: "tool project/search does not exist in LaunchDarkly."},
		"already synced": {
			local: true, launchDarkly: []syncdomain.Attachment{search},
			want: "tool project/search is already synced. Edit .launchdarkly/project/tools/search.json and run ldcli sync tools or ldcli sync all.",
		},
		"lock entry without a file": {
			tracked: true, launchDarkly: []syncdomain.Attachment{search},
			want: "tool project/search has a sync.lock entry but no local file. Run ldcli sync tools or ldcli sync all to restore it.",
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			store := synclocal.NewStore(root)
			if test.local {
				_, err := store.ReplaceAttachments([]syncdomain.SyncedAttachment{{ProjectKey: "project", Attachment: search}})
				require.NoError(t, err)
			}
			if test.tracked {
				require.NoError(t, os.MkdirAll(filepath.Join(root, syncdomain.RootDir), 0o755))
				lock := syncmanifest.New()
				require.NoError(t, lock.SetAttachments("project", []syncdomain.Attachment{search}))
				require.NoError(t, syncmanifest.WriteLock(store, lock))
			}
			baselines := &memoryManifestStore{manifest: syncmanifest.New()}

			err := Run(Options{
				Kind: syncdomain.KindTool, Attachments: &fakeAttachments{attachments: test.launchDarkly},
				Store: store, Baselines: baselines, Output: &bytes.Buffer{}, Selections: []syncdomain.ResourceID{id},
			})

			require.EqualError(t, err, test.want)
			assert.Empty(t, baselines.manifest.Resources)
			files, err := synclocal.SourceFiles(root)
			require.NoError(t, err)
			if !test.local {
				assert.NotContains(t, files, ".launchdarkly/project/tools/search.json")
			}
		})
	}
}

func TestRunPreviewsToolsAndSkillsInADryRun(t *testing.T) {
	root := t.TempDir()
	skill := testAttachment(syncdomain.KindSkill)
	var output bytes.Buffer

	err := Run(Options{
		Kind: syncdomain.KindSkill, Attachments: &fakeAttachments{attachments: []syncdomain.Attachment{skill}},
		Store: synclocal.NewStore(root), Baselines: &memoryManifestStore{manifest: syncmanifest.New()},
		Output: &output, DryRun: true, Diff: true, Selections: []syncdomain.ResourceID{skill.ID("project")},
	})

	require.NoError(t, err)
	assert.Equal(t, `1 file to create:
  .launchdarkly/project/skills/summarize.md   +6

.launchdarkly/project/skills/summarize.md
    ---
    key: summarize
    description: Summarize a ticket
    ---

    Write three bullets.
`, output.String())
	_, err = os.Stat(filepath.Join(root, syncdomain.RootDir))
	require.ErrorIs(t, err, os.ErrNotExist, "a dry run writes nothing")
}

func TestRunRemovesNewToolFilesWhenTheBaselineSaveFails(t *testing.T) {
	root := t.TempDir()
	search := testAttachment(syncdomain.KindTool)

	err := Run(Options{
		Kind: syncdomain.KindTool, Attachments: &fakeAttachments{attachments: []syncdomain.Attachment{search}},
		Store: synclocal.NewStore(root), Baselines: failingManifestStore{}, Output: &bytes.Buffer{},
		Selections: []syncdomain.ResourceID{search.ID("project")},
	})

	require.EqualError(t, err, "write manifest")
	_, err = os.Stat(filepath.Join(root, syncdomain.RootDir))
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestRunNamesTheToolFlagsWithoutInput(t *testing.T) {
	options := Options{
		Kind: syncdomain.KindTool, Attachments: &fakeAttachments{}, Store: synclocal.NewStore(t.TempDir()),
		Input: &bytes.Buffer{}, Output: &bytes.Buffer{}, NoInput: true,
	}
	require.EqualError(t, Run(options), "--project and --key are required with --no-input")

	options.ProjectKey = "project"
	require.EqualError(t, Run(options), "--key values are required with --no-input")
}

func testAttachment(kind syncdomain.Kind) syncdomain.Attachment {
	if kind == syncdomain.KindSkill {
		return syncdomain.Attachment{Kind: syncdomain.AttachmentSkill, Version: 2, Skill: &syncdomain.Skill{
			Key: "summarize", Name: "Summarize", Description: "Summarize a ticket", Markdown: "Write three bullets.\n",
		}}
	}
	description := "Search documentation"
	return syncdomain.Attachment{Kind: syncdomain.AttachmentTool, Version: 4, Tool: &syncdomain.Tool{
		Key: "search", Description: &description, Schema: map[string]any{"type": "object"},
	}}
}

// fakeAttachments is LaunchDarkly with the given tools and skills.
type fakeAttachments struct {
	attachments []syncdomain.Attachment
}

func (fake *fakeAttachments) ReadAttachment(_ string, kind syncdomain.AttachmentKind, key string) (syncdomain.Attachment, error) {
	for _, attachment := range fake.attachments {
		if attachment.Kind == kind && attachment.Key() == key {
			return attachment, nil
		}
	}
	return syncdomain.Attachment{}, errors.New(`{"code":"not_found","statusCode":404}`)
}

func (fake *fakeAttachments) SearchAttachments(
	string, syncdomain.AttachmentKind, string, int, int,
) (syncapi.Page[syncdomain.Attachment], error) {
	return syncapi.Page[syncdomain.Attachment]{Items: fake.attachments, TotalCount: len(fake.attachments)}, nil
}
