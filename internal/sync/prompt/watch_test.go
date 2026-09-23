package prompt

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

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
		done <- watchWorkspace(ctx, root, 5*time.Millisecond, 20*time.Millisecond, func() error {
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
	require.Contains(t, output.String(), "Watching prompt files:\n- .launchdarkly/project/configs/config/prompt.prompt.md\n")
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
		done <- watchWorkspace(ctx, root, 5*time.Millisecond, 15*time.Millisecond, func() error {
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
		done <- watchWorkspace(ctx, root, 5*time.Millisecond, 15*time.Millisecond, func() error {
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

func TestWatchedConflictPromptRefreshesWhenSourceChanges(t *testing.T) {
	root := t.TempDir()
	wrapper := filepath.Join(root, syncdomain.RootDir, "project", "configs", "config", "prompt.prompt.md")
	require.NoError(t, os.MkdirAll(filepath.Dir(wrapper), 0o755))
	require.NoError(t, os.WriteFile(wrapper, []byte("initial"), 0o644))
	snapshot, err := sourceSnapshot(root)
	require.NoError(t, err)

	input, inputWriter := io.Pipe()
	defer input.Close()
	defer inputWriter.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	results := make(chan conflictChoice, 1)
	errs := make(chan error, 1)
	go func() {
		result, err := promptWatchedConflictResolution(ctx, input, io.Discard, watchedSources{repositoryRoot: root, snapshot: snapshot})
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
		done <- watchWorkspace(ctx, root, 5*time.Millisecond, 15*time.Millisecond, func() error {
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
