package prompt

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/stretchr/testify/require"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
	synclocal "github.com/launchdarkly/ldcli/internal/sync/local"
	syncreference "github.com/launchdarkly/ldcli/internal/sync/reference"
)

func TestWatchWorkspaceWaitsForDebouncedChange(t *testing.T) {
	root := t.TempDir()
	wrapper := filepath.Join(root, syncdomain.RootDir, "project", "configs", "config", "prompt.prompt.md")
	require.NoError(t, os.MkdirAll(filepath.Dir(wrapper), 0o755))
	require.NoError(t, os.WriteFile(wrapper, []byte("initial"), 0o644))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var syncCount atomic.Int32
	synced := make(chan struct{}, 2)
	done := make(chan error, 1)
	var output bytes.Buffer
	go func() {
		done <- watchWorkspace(ctx, root, 20*time.Millisecond, func(*sourceWatcher) error {
			syncCount.Add(1)
			synced <- struct{}{}
			return nil
		}, &output)
	}()

	select {
	case <-synced:
		t.Fatal("watch ran an initial sync")
	case <-time.After(40 * time.Millisecond):
	}

	require.NoError(t, os.WriteFile(wrapper, []byte("first"), 0o644))
	require.NoError(t, os.WriteFile(wrapper, []byte("second"), 0o644))
	select {
	case <-synced:
	case <-time.After(time.Second):
		t.Fatal("watch did not sync changed file")
	}
	require.Equal(t, int32(1), syncCount.Load())

	cancel()
	require.NoError(t, <-done)
	require.Contains(t, output.String(), "Watching files for sync:\n- .launchdarkly/project/configs/config/prompt.prompt.md\n")
}

func TestWatchWorkspaceKeepsWatchingAfterSyncError(t *testing.T) {
	root := t.TempDir()
	wrapper := filepath.Join(root, syncdomain.RootDir, "project", "configs", "config", "prompt.prompt.md")
	require.NoError(t, os.MkdirAll(filepath.Dir(wrapper), 0o755))
	require.NoError(t, os.WriteFile(wrapper, []byte("initial"), 0o644))

	ctx, cancel := context.WithCancel(context.Background())
	var attempts atomic.Int32
	attempted := make(chan struct{}, 2)
	done := make(chan error, 1)
	go func() {
		done <- watchWorkspace(ctx, root, 15*time.Millisecond, func(*sourceWatcher) error {
			attempt := attempts.Add(1)
			attempted <- struct{}{}
			if attempt == 1 {
				return errors.New("temporary failure")
			}
			return nil
		}, io.Discard)
	}()

	time.Sleep(30 * time.Millisecond)
	require.NoError(t, os.WriteFile(wrapper, []byte("first"), 0o644))
	select {
	case <-attempted:
	case <-time.After(time.Second):
		t.Fatal("watch did not attempt first sync")
	}
	require.NoError(t, os.WriteFile(wrapper, []byte("second"), 0o644))
	select {
	case <-attempted:
	case <-time.After(time.Second):
		t.Fatal("watch did not attempt second sync")
	}
	require.Equal(t, int32(2), attempts.Load())

	cancel()
	require.NoError(t, <-done)
}

func TestWatchWorkspaceRefreshesPlanAfterSourceChangesDuringSync(t *testing.T) {
	root := t.TempDir()
	wrapper := filepath.Join(root, syncdomain.RootDir, "project", "configs", "config", "prompt.prompt.md")
	require.NoError(t, os.MkdirAll(filepath.Dir(wrapper), 0o755))
	require.NoError(t, os.WriteFile(wrapper, []byte("initial"), 0o644))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var attempts atomic.Int32
	attempted := make(chan struct{}, 2)
	done := make(chan error, 1)
	go func() {
		done <- watchWorkspace(ctx, root, 15*time.Millisecond, func(*sourceWatcher) error {
			attempt := attempts.Add(1)
			attempted <- struct{}{}
			if attempt == 1 {
				return errRefreshWatchPlan
			}
			return nil
		}, io.Discard)
	}()

	time.Sleep(30 * time.Millisecond)
	require.NoError(t, os.WriteFile(wrapper, []byte("changed"), 0o644))
	for attempt := 0; attempt < 2; attempt++ {
		select {
		case <-attempted:
		case <-time.After(time.Second):
			t.Fatal("watch did not refresh the changed plan")
		}
	}
	require.Equal(t, int32(2), attempts.Load())

	cancel()
	require.NoError(t, <-done)
}

func TestWatchWorkspaceRefreshesReferencedFilesBeforeRebuildingPlan(t *testing.T) {
	root := t.TempDir()
	store := synclocal.NewStore(root)
	_, err := store.Add([]synclocal.VariationFile{{
		ProjectKey: "project",
		ConfigKey:  "config",
		Variation: syncdomain.Variation{
			Mode: syncdomain.VariationModeAgent, Key: "prompt", Name: "Prompt", Instructions: "Initial",
		},
	}})
	require.NoError(t, err)
	wrapper := filepath.Join(root, syncdomain.RootDir, "project", "configs", "config", "prompt.prompt.md")
	referencePath := filepath.Join(root, "prompts", "prompt.md")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var attempts atomic.Int32
	attempted := make(chan struct{}, 3)
	done := make(chan error, 1)
	go func() {
		done <- watchWorkspace(ctx, root, 15*time.Millisecond, func(*sourceWatcher) error {
			attempt := attempts.Add(1)
			if attempt == 1 {
				if err := os.MkdirAll(filepath.Dir(referencePath), 0o755); err != nil {
					return err
				}
				if err := os.WriteFile(referencePath, []byte("Referenced"), 0o644); err != nil {
					return err
				}
				content := `---
formatVersion: 1
upsert: true
ref:
  file: prompts/prompt.md
  format: plain-markdown
mode: agent
key: prompt
name: Prompt
---
`
				if err := os.WriteFile(wrapper, []byte(content), 0o644); err != nil {
					return err
				}
				attempted <- struct{}{}
				return errRefreshWatchPlan
			}
			attempted <- struct{}{}
			return nil
		}, io.Discard)
	}()

	time.Sleep(30 * time.Millisecond)
	require.NoError(t, os.WriteFile(wrapper, []byte("trigger"), 0o644))
	for attempt := 0; attempt < 2; attempt++ {
		select {
		case <-attempted:
		case <-time.After(time.Second):
			t.Fatal("watch did not rebuild the plan")
		}
	}

	require.NoError(t, os.WriteFile(referencePath, []byte("Changed"), 0o644))
	select {
	case <-attempted:
	case <-time.After(time.Second):
		t.Fatal("watch did not track the newly referenced file")
	}

	cancel()
	require.NoError(t, <-done)
}

func TestWatchWorkspaceRetriesEditsMadeDuringSuccessfulSync(t *testing.T) {
	root := t.TempDir()
	wrapper := filepath.Join(root, syncdomain.RootDir, "project", "configs", "config", "prompt.prompt.md")
	require.NoError(t, os.MkdirAll(filepath.Dir(wrapper), 0o755))
	require.NoError(t, os.WriteFile(wrapper, []byte("initial"), 0o644))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var attempts atomic.Int32
	attempted := make(chan struct{}, 2)
	done := make(chan error, 1)
	go func() {
		done <- watchWorkspace(ctx, root, 15*time.Millisecond, func(*sourceWatcher) error {
			if attempts.Add(1) == 1 {
				if err := os.WriteFile(wrapper, []byte("changed during sync"), 0o644); err != nil {
					return err
				}
			}
			attempted <- struct{}{}
			return nil
		}, io.Discard)
	}()

	time.Sleep(30 * time.Millisecond)
	require.NoError(t, os.WriteFile(wrapper, []byte("trigger"), 0o644))
	for attempt := 0; attempt < 2; attempt++ {
		select {
		case <-attempted:
		case <-time.After(time.Second):
			t.Fatal("watch dropped a source edit made during sync")
		}
	}
	require.Equal(t, int32(2), attempts.Load())

	cancel()
	require.NoError(t, <-done)
}

func TestWatchedConflictPromptRefreshesWhenSourceChanges(t *testing.T) {
	root := t.TempDir()
	wrapper := filepath.Join(root, syncdomain.RootDir, "project", "configs", "config", "prompt.prompt.md")
	require.NoError(t, os.MkdirAll(filepath.Dir(wrapper), 0o755))
	require.NoError(t, os.WriteFile(wrapper, []byte("initial"), 0o644))
	snapshot, err := sourceSnapshot(root)
	require.NoError(t, err)
	watcher, err := newSourceWatcher(root)
	require.NoError(t, err)
	defer watcher.Close()

	input, inputWriter := io.Pipe()
	defer input.Close()
	defer inputWriter.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	results := make(chan conflictChoice, 1)
	errs := make(chan error, 1)
	go func() {
		result, err := promptWatchedConflictResolution(ctx, input, io.Discard, watchedSources{
			watcher: watcher, snapshot: snapshot, debounce: 15 * time.Millisecond,
		})
		results <- result
		errs <- err
	}()

	require.NoError(t, os.WriteFile(wrapper, []byte("changed"), 0o644))
	select {
	case result := <-results:
		require.NoError(t, <-errs)
		require.True(t, result.sourcesChanged)
	case <-ctx.Done():
		t.Fatal("conflict prompt did not refresh after the source changed")
	}
}

func TestWatchedConflictPromptCancellationStopsWatch(t *testing.T) {
	root := t.TempDir()
	wrapper := filepath.Join(root, syncdomain.RootDir, "project", "configs", "config", "prompt.prompt.md")
	require.NoError(t, os.MkdirAll(filepath.Dir(wrapper), 0o755))
	require.NoError(t, os.WriteFile(wrapper, []byte("initial"), 0o644))
	snapshot, err := sourceSnapshot(root)
	require.NoError(t, err)
	watcher, err := newSourceWatcher(root)
	require.NoError(t, err)
	defer watcher.Close()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, err = promptWatchedConflictResolution(
		ctx,
		strings.NewReader("\x03"),
		io.Discard,
		watchedSources{watcher: watcher, snapshot: snapshot, debounce: 15 * time.Millisecond},
	)

	require.ErrorIs(t, err, context.Canceled)
}

func TestWatchWorkspaceTracksMissingReferencedFileParent(t *testing.T) {
	root := t.TempDir()
	_, err := synclocal.NewStore(root).Add([]synclocal.VariationFile{{
		ProjectKey: "project", ConfigKey: "config",
		Ref: &synclocal.Reference{File: "prompts/nested/prompt.md", Format: syncreference.PlainMarkdown},
		Variation: syncdomain.Variation{
			Mode: syncdomain.VariationModeAgent, Key: "prompt", Name: "Prompt",
		},
	}})
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	synced := make(chan struct{}, 1)
	done := make(chan error, 1)
	go func() {
		done <- watchWorkspace(ctx, root, 15*time.Millisecond, func(*sourceWatcher) error {
			synced <- struct{}{}
			return nil
		}, io.Discard)
	}()

	time.Sleep(30 * time.Millisecond)
	referencePath := filepath.Join(root, "prompts", "nested", "prompt.md")
	require.NoError(t, os.MkdirAll(filepath.Dir(referencePath), 0o755))
	require.NoError(t, os.WriteFile(referencePath, []byte("created"), 0o644))
	select {
	case <-synced:
	case <-time.After(time.Second):
		t.Fatal("watch did not detect a referenced file created under a missing parent")
	}

	cancel()
	require.NoError(t, <-done)
}

func TestWatchWorkspaceRewatchesRecreatedManagedTree(t *testing.T) {
	root := t.TempDir()
	wrapper := filepath.Join(root, syncdomain.RootDir, "project", "configs", "config", "prompt.prompt.md")
	require.NoError(t, os.MkdirAll(filepath.Dir(wrapper), 0o755))
	require.NoError(t, os.WriteFile(wrapper, []byte("initial"), 0o644))

	ctx, cancel := context.WithCancel(context.Background())
	attempted := make(chan struct{}, 2)
	done := make(chan error, 1)
	go func() {
		done <- watchWorkspace(ctx, root, 15*time.Millisecond, func(*sourceWatcher) error {
			attempted <- struct{}{}
			return nil
		}, io.Discard)
	}()

	time.Sleep(30 * time.Millisecond)
	require.NoError(t, os.RemoveAll(filepath.Join(root, syncdomain.RootDir)))
	select {
	case <-attempted:
	case <-time.After(time.Second):
		t.Fatal("watch did not detect the managed tree deletion")
	}

	require.NoError(t, os.MkdirAll(filepath.Dir(wrapper), 0o755))
	require.NoError(t, os.WriteFile(wrapper, []byte("recreated"), 0o644))
	select {
	case <-attempted:
	case <-time.After(time.Second):
		t.Fatal("watch did not detect the recreated managed tree")
	}

	cancel()
	require.NoError(t, <-done)
}

func TestWatchWorkspaceDetectsReferencedFileChange(t *testing.T) {
	root := t.TempDir()
	referencePath := filepath.Join(root, "prompt.md")
	require.NoError(t, os.WriteFile(referencePath, []byte("initial"), 0o644))
	_, err := synclocal.NewStore(root).Add([]synclocal.VariationFile{{
		ProjectKey: "project", ConfigKey: "config",
		Ref: &synclocal.Reference{File: "prompt.md", Format: syncreference.PlainMarkdown},
		Variation: syncdomain.Variation{
			Mode: syncdomain.VariationModeAgent, Key: "prompt", Name: "Prompt",
		},
	}})
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	synced := make(chan struct{}, 1)
	done := make(chan error, 1)
	var output bytes.Buffer
	go func() {
		done <- watchWorkspace(ctx, root, 15*time.Millisecond, func(*sourceWatcher) error {
			synced <- struct{}{}
			return nil
		}, &output)
	}()

	time.Sleep(30 * time.Millisecond)
	require.NoError(t, os.WriteFile(referencePath, []byte("changed"), 0o644))
	select {
	case <-synced:
	case <-time.After(time.Second):
		t.Fatal("watch did not detect referenced file change")
	}
	cancel()
	require.NoError(t, <-done)
	require.Contains(t, output.String(), "- .launchdarkly/project/configs/config/prompt.prompt.md\n")
	require.Contains(t, output.String(), "- prompt.md\n")
}

func TestSourceWatcherRecognizesNewManagedResourceKinds(t *testing.T) {
	root := t.TempDir()
	managedRoot := filepath.Join(root, syncdomain.RootDir)
	resourceFile := filepath.Join(managedRoot, "project", "skills", "review.skill.md")
	require.NoError(t, os.MkdirAll(filepath.Dir(resourceFile), 0o755))
	require.NoError(t, os.WriteFile(resourceFile, []byte("Review carefully."), 0o644))

	watcher := sourceWatcher{
		managedRoot: managedRoot,
		files:       make(map[string]struct{}),
	}

	require.True(t, watcher.relevant(fsnotify.Event{Name: resourceFile, Op: fsnotify.Create}))
	require.False(t, watcher.relevant(fsnotify.Event{
		Name: filepath.Join(managedRoot, "manifest.yaml"),
		Op:   fsnotify.Create,
	}))
}
