package prompt

import (
	"context"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/launchdarkly/ldcli/internal/resources"
	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
	syncapi "github.com/launchdarkly/ldcli/internal/sync/api"
	syncbootstrap "github.com/launchdarkly/ldcli/internal/sync/bootstrap"
	syncdetach "github.com/launchdarkly/ldcli/internal/sync/detach"
	synclink "github.com/launchdarkly/ldcli/internal/sync/link"
	synclocal "github.com/launchdarkly/ldcli/internal/sync/local"
	syncreference "github.com/launchdarkly/ldcli/internal/sync/reference"
	syncsource "github.com/launchdarkly/ldcli/internal/sync/source"
)

func TestRunnerBootstrapsMissingWorkspaceAndAddsToExistingWorkspace(t *testing.T) {
	tests := map[string]struct {
		createDirectory bool
		add             bool
		dryRun          bool
		wantInitial     bool
	}{
		"missing workspace": {
			wantInitial: true,
		},
		"missing workspace dry run": {
			dryRun:      true,
			wantInitial: true,
		},
		"add to existing workspace": {
			createDirectory: true,
			add:             true,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			root := initGitRepository(t)
			if test.createDirectory {
				require.NoError(t, os.Mkdir(filepath.Join(root, syncdomain.RootDir), 0o755))
			}

			called := false
			runner := NewRunner(noopResourceClient{})
			runner.bootstrap = func(options syncbootstrap.Options) error {
				called = true
				assert.Equal(t, test.wantInitial, options.Initial)
				assert.Equal(t, test.dryRun, options.DryRun)
				assert.NotNil(t, options.Catalog)
				assert.NotNil(t, options.Input)
				assert.NotNil(t, options.Output)
				return nil
			}

			err := runner.Run(Options{
				WorkingDirectory: root,
				AccessToken:      "token",
				BaseURI:          "https://example.com",
				Add:              test.add,
				DryRun:           test.dryRun,
				Input:            os.Stdin,
				Output:           io.Discard,
				ErrorOutput:      io.Discard,
			})

			require.NoError(t, err)
			assert.True(t, called)
		})
	}
}

func TestRunnerRequiresGit(t *testing.T) {
	runner := NewRunner(noopResourceClient{})
	called := false
	runner.bootstrap = func(syncbootstrap.Options) error {
		called = true
		return nil
	}

	err := runner.Run(Options{
		WorkingDirectory: t.TempDir(),
		Input:            os.Stdin,
		Output:           io.Discard,
		ErrorOutput:      io.Discard,
	})

	require.ErrorIs(t, err, syncsource.ErrGitRequired)
	assert.False(t, called)
}

func TestRunnerWatchDoesNotRunInitialSync(t *testing.T) {
	root := initGitRepository(t)
	require.NoError(t, os.Mkdir(filepath.Join(root, syncdomain.RootDir), 0o755))
	runner := NewRunner(noopResourceClient{})
	watchCalled := false
	runner.watch = func(
		_ context.Context,
		repositoryRoot string,
		_ time.Duration,
		_ func(*sourceWatcher) error,
		_ io.Writer,
	) error {
		watchCalled = true
		expectedRoot, err := filepath.EvalSymlinks(root)
		require.NoError(t, err)
		assert.Equal(t, expectedRoot, repositoryRoot)
		return nil
	}

	err := runner.Run(Options{
		WorkingDirectory: root,
		Watch:            true,
		Input:            os.Stdin,
		Output:           io.Discard,
		ErrorOutput:      io.Discard,
	})

	require.NoError(t, err)
	assert.True(t, watchCalled)
}

func TestOptionsForWatchSyncPreservesYes(t *testing.T) {
	ctx := context.Background()
	for _, yes := range []bool{false, true} {
		options := optionsForWatchSync(ctx, Options{
			Add: true, Format: syncreference.PlainMarkdown, Link: "prompt.md", Yes: yes,
		})

		assert.False(t, options.Add)
		assert.Empty(t, options.Format)
		assert.Empty(t, options.Link)
		assert.Equal(t, yes, options.Yes)
		assert.Equal(t, ctx, options.Context)
	}
}

func TestRunnerLinksBeforeWatching(t *testing.T) {
	root := initGitRepository(t)
	runner := NewRunner(noopResourceClient{})
	linkCalled := false
	runner.link = func(options synclink.Options) (string, error) {
		linkCalled = true
		assert.Equal(t, "prompt.md", options.File)
		assert.Equal(t, syncreference.PlainMarkdown, options.Format)
		require.NoError(t, os.Mkdir(filepath.Join(root, syncdomain.RootDir), 0o755))
		return "project/configs/config/prompt.prompt.md", nil
	}
	watchCalled := false
	runner.watch = func(context.Context, string, time.Duration, func(*sourceWatcher) error, io.Writer) error {
		watchCalled = true
		return nil
	}

	err := runner.Run(Options{
		WorkingDirectory: root,
		Link:             "prompt.md",
		Format:           syncreference.PlainMarkdown,
		Watch:            true,
		Input:            os.Stdin,
		Output:           io.Discard,
		ErrorOutput:      io.Discard,
	})

	require.NoError(t, err)
	assert.True(t, linkCalled)
	assert.True(t, watchCalled)
}

func TestRunnerDetachesWithoutCallingTheAPI(t *testing.T) {
	root := initGitRepository(t)
	runner := NewRunner(noopResourceClient{})
	called := false
	runner.detach = func(options syncdetach.Options) error {
		called = true
		assert.NotZero(t, options.Store)
		assert.NotZero(t, options.Manifest)
		assert.Equal(t, os.Stdin, options.Input)
		assert.Equal(t, io.Discard, options.Output)
		return nil
	}

	err := runner.Run(Options{
		WorkingDirectory: root,
		Detach:           true,
		Input:            os.Stdin,
		Output:           io.Discard,
		ErrorOutput:      io.Discard,
	})

	require.NoError(t, err)
	assert.True(t, called)
}

func TestValidateOptions(t *testing.T) {
	require.ErrorContains(t, validateOptions(Options{Format: syncreference.PlainMarkdown}), "--format requires --link")
	require.ErrorContains(t, validateOptions(Options{Link: "prompt.md"}), "--link requires --format")
	require.ErrorContains(t, validateOptions(Options{Watch: true, DryRun: true}), "--watch cannot be used with --dry-run")
	require.ErrorContains(t, validateOptions(Options{Detach: true, Add: true}), "--detach cannot be combined")
	require.ErrorContains(t, validateOptions(Options{
		Attachment: &AttachmentRequest{Kind: syncdomain.AttachmentTool}, Watch: true,
	}), "attachment options cannot be combined")
	require.ErrorContains(t, validateOptions(Options{
		Attachment: &AttachmentRequest{Kind: "invalid"},
	}), "attachment kind")
	require.NoError(t, validateOptions(Options{
		Attachment: &AttachmentRequest{
			Kind: syncdomain.AttachmentTool, Key: "search", ProjectKey: "project", Variation: "config/default",
		},
		Yes: true,
	}))
}

func TestSamePlannedResourceStateDetectsChangedAttachmentPins(t *testing.T) {
	reviewedVariation := testVariation("reviewed")
	reviewedVariation.Tools = []syncdomain.AttachmentRef{{Key: "search", Version: 1}}
	currentVariation := reviewedVariation
	currentVariation.Tools = []syncdomain.AttachmentRef{{Key: "search", Version: 2}}
	reviewed := PlannedResource{
		ID:     ResourceID{Kind: syncdomain.KindVariation, ProjectKey: "project", LookupKey: "config/default"},
		Action: ActionUpdateServer, Server: &reviewedVariation, ServerHasStaleAttachmentPins: true,
	}
	current := reviewed
	current.Server = &currentVariation

	assert.False(t, samePlannedResourceState(reviewed, current))
}

func TestAttachToVariationPreservesExistingSharedAttachmentEdits(t *testing.T) {
	root := t.TempDir()
	store := synclocal.NewStore(root)
	localDescription, serverDescription := "Local edit", "Server content"
	tool := syncdomain.Tool{Key: "search", Description: &localDescription, Schema: map[string]any{"type": "object"}}
	first := testVariation("first")
	first.Key = "first"
	first.Tools = []syncdomain.AttachmentRef{{Key: "search"}}
	first.Attachments = []syncdomain.Attachment{{Kind: syncdomain.AttachmentTool, Tool: &tool}}
	second := testVariation("second")
	second.Key = "second"
	_, err := store.Add([]synclocal.VariationFile{
		{ProjectKey: "project", ConfigKey: "config", Variation: first},
		{ProjectKey: "project", ConfigKey: "config", Variation: second},
	})
	require.NoError(t, err)

	transport := &attachmentMutationAPI{
		current: syncdomain.Tool{Key: "search", Description: &serverDescription, Schema: map[string]any{"type": "object"}},
		version: 2,
	}
	client := syncapi.NewClient(transport, "token", "https://example.com")
	err = attachToVariation(store, client, attachOptions{
		RepositoryRoot: root,
		ProjectKey:     "project",
		VariationID:    "config/second",
		Kind:           syncdomain.AttachmentTool,
		Key:            "search",
	})

	require.NoError(t, err)
	resources, err := synclocal.CompileWorkspace(root)
	require.NoError(t, err)
	for _, resource := range resources {
		require.Len(t, resource.Attachments, 1)
		assert.Equal(t, localDescription, *resource.Attachments[0].Tool.Description)
	}
}

func TestEmptyAttachmentSearchReturnsNoResults(t *testing.T) {
	client := syncapi.NewClient(emptyAttachmentClient{}, "token", "https://example.com")

	attachments, totalCount, err := searchAttachmentPage(client, "project", syncdomain.AttachmentTool, "missing", 25, 0)

	require.NoError(t, err)
	assert.Empty(t, attachments)
	assert.Zero(t, totalCount)
}

func TestToolSearchPageReturnsTrueLatestVersion(t *testing.T) {
	transport := &toolSearchClient{}
	client := syncapi.NewClient(transport, "token", "https://example.com")

	attachments, totalCount, err := searchAttachmentPage(client, "project", syncdomain.AttachmentTool, "old description", 25, 0)

	require.NoError(t, err)
	require.Len(t, attachments, 1)
	assert.Equal(t, 3, attachments[0].Version)
	assert.Equal(t, 1, totalCount)
	assert.Equal(t, 2, transport.requests)
}

func TestAddAttachmentContentPreservesOtherKinds(t *testing.T) {
	skill := syncdomain.Skill{Key: "support", Markdown: "# Support"}
	variation := syncdomain.Variation{
		Skills:      []syncdomain.AttachmentRef{{Key: "support"}},
		Attachments: []syncdomain.Attachment{{Kind: syncdomain.AttachmentSkill, Skill: &skill}},
	}
	tool := syncdomain.Tool{Key: "search", Schema: map[string]any{"type": "object"}}

	addAttachmentContent(&variation, syncdomain.Attachment{Kind: syncdomain.AttachmentTool, Version: 3, Tool: &tool})

	assert.Equal(t, []syncdomain.AttachmentRef{{Key: "support"}}, variation.Skills)
	assert.Equal(t, []syncdomain.AttachmentRef{{Key: "search"}}, variation.Tools)
	assert.Len(t, variation.Attachments, 2)
}

type noopResourceClient struct{}

var _ resources.Client = noopResourceClient{}

func (noopResourceClient) MakeRequest(string, string, string, string, url.Values, []byte, bool) ([]byte, error) {
	return nil, nil
}

func (noopResourceClient) MakeUnauthenticatedRequest(string, string, []byte) ([]byte, error) {
	return nil, nil
}

type emptyAttachmentClient struct{}

func (emptyAttachmentClient) MakeRequest(string, string, string, string, url.Values, []byte, bool) ([]byte, error) {
	return []byte(`{"items":[],"totalCount":0}`), nil
}

func (emptyAttachmentClient) MakeUnauthenticatedRequest(string, string, []byte) ([]byte, error) {
	return nil, nil
}

type toolSearchClient struct {
	requests int
}

func (client *toolSearchClient) MakeRequest(_ string, _ string, path string, _ string, _ url.Values, _ []byte, _ bool) ([]byte, error) {
	client.requests++
	if strings.HasSuffix(path, "/ai-tools") {
		return []byte(`{
			"items":[{"key":"search","description":"old description","schema":{},"version":2}],
			"totalCount":1
		}`), nil
	}
	return []byte(`{"key":"search","description":"new description","schema":{},"version":3}`), nil
}

func (*toolSearchClient) MakeUnauthenticatedRequest(string, string, []byte) ([]byte, error) {
	return nil, nil
}

func initGitRepository(t *testing.T) string {
	t.Helper()

	root := t.TempDir()
	for _, args := range [][]string{
		{"init", "--quiet"},
		{"remote", "add", "origin", "git@github.com:launchdarkly/example.git"},
	} {
		command := exec.Command("git", args...)
		command.Dir = root
		output, err := command.CombinedOutput()
		require.NoError(t, err, "%s", output)
	}
	return root
}
