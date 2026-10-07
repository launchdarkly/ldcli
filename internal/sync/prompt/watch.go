package prompt

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
	syncconsole "github.com/launchdarkly/ldcli/internal/sync/console"
	synclocal "github.com/launchdarkly/ldcli/internal/sync/local"
)

const watchDebounce = 300 * time.Millisecond

var errRefreshWatchPlan = errors.New("refresh watch plan")

type watchRunner func(context.Context, string, time.Duration, func(*sourceWatcher) error, io.Writer) error

// watchWorkspace waits for a stable source change before running sync. The
// initial filesystem state is only a baseline and never triggers a sync.
func watchWorkspace(
	ctx context.Context,
	repositoryRoot string,
	debounce time.Duration,
	syncWorkspace func(*sourceWatcher) error,
	output io.Writer,
) error {
	// Show the exact initial watch set, but do not treat it as a change. Watch
	// mode intentionally waits for an edit before running its first sync.
	files, err := synclocal.SourceFiles(repositoryRoot)
	if err != nil {
		return err
	}
	console := syncconsole.New(output)
	_ = console.Line("Watching files for sync:")
	for _, file := range files {
		_ = console.Printf("- %s\n", file)
	}

	watcher, err := newSourceWatcher(repositoryRoot)
	if err != nil {
		return err
	}
	defer watcher.Close()

	// Filesystem APIs commonly emit duplicate events for one editor save. A
	// content snapshot lets us ignore events that do not change sync inputs.
	handledSnapshot, err := sourceSnapshot(repositoryRoot)
	if err != nil {
		return err
	}

	for {
		if err := watcher.WaitForChange(ctx, debounce); err != nil {
			if errors.Is(err, context.Canceled) {
				return nil
			}
			_ = console.Printf("Watch error: %s\n", err)
			continue
		}

		current, err := sourceSnapshot(repositoryRoot)
		if err != nil {
			_ = console.Printf("Watch error: %s\n", err)
			continue
		}
		if current == handledSnapshot {
			continue
		}

		_ = console.Line("A watched file changed; syncing...")
		// Keep rebuilding until one sync attempt covers a stable source
		// snapshot. This closes the gap where an edit arrives during sync.
		for {
			err = syncWorkspace(watcher)
			if errors.Is(err, context.Canceled) {
				return nil
			}
			if errors.Is(err, errRefreshWatchPlan) {
				// A source changed during review or the reviewed server state
				// became stale. Rebuild immediately instead of waiting for a
				// second filesystem event that may never arrive.
				if err := watcher.Refresh(); err != nil {
					return err
				}
				current, err = sourceSnapshot(repositoryRoot)
				if err != nil {
					return err
				}
				continue
			}
			if err != nil {
				_ = console.Printf("Sync failed: %s\n", err)
			}

			// Record the exact state used by this attempt. If sync or the user
			// changed another source while it ran, rebuild immediately.
			handledSnapshot = current
			if err := watcher.Refresh(); err != nil {
				return err
			}
			latestSnapshot, err := sourceSnapshot(repositoryRoot)
			if err != nil {
				return err
			}
			if latestSnapshot != current {
				current = latestSnapshot
				continue
			}
			break
		}
	}
}

type sourceWatcher struct {
	root        string
	managedRoot string
	files       map[string]struct{}
	directories map[string]struct{}
	watcher     *fsnotify.Watcher
}

// newSourceWatcher creates the fsnotify watcher and registers the initial
// managed and referenced source directories.
func newSourceWatcher(repositoryRoot string) (*sourceWatcher, error) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("create sync file watcher: %w", err)
	}

	result := &sourceWatcher{
		root:        repositoryRoot,
		managedRoot: filepath.Join(repositoryRoot, syncdomain.RootDir),
		files:       make(map[string]struct{}),
		directories: make(map[string]struct{}),
		watcher:     watcher,
	}
	if err := result.Refresh(); err != nil {
		_ = watcher.Close()
		return nil, err
	}
	return result, nil
}

// Close releases the underlying operating-system watcher.
func (watcher *sourceWatcher) Close() error {
	return watcher.watcher.Close()
}

// Refresh discovers referenced files and adds any newly relevant directories.
func (watcher *sourceWatcher) Refresh() error {
	files, err := synclocal.SourceFiles(watcher.root)
	if err != nil {
		return err
	}

	// Rebuild registrations from source-of-truth state because a wrapper edit
	// may add, remove, or redirect an external reference.
	watcher.resetDirectories()
	watcher.files = make(map[string]struct{}, len(files))
	// Watching the repository root lets us observe recreation of a deleted
	// .launchdarkly tree and creation of a missing referenced-file parent.
	if err := watcher.addDirectory(watcher.root); err != nil {
		return err
	}
	for _, file := range files {
		absolute := filepath.Join(watcher.root, filepath.FromSlash(file))
		watcher.files[filepath.Clean(absolute)] = struct{}{}
		if err := watcher.addClosestExistingDirectory(filepath.Dir(absolute)); err != nil {
			return err
		}
	}
	// fsnotify is not recursive, so every existing managed directory needs its
	// own registration.
	return watcher.addDirectoryTree(watcher.managedRoot)
}

// resetDirectories removes stale OS registrations before rebuilding the
// desired watch set from current workspace state.
func (watcher *sourceWatcher) resetDirectories() {
	for directory := range watcher.directories {
		_ = watcher.watcher.Remove(directory)
	}
	watcher.directories = make(map[string]struct{})
}

// WaitForChange waits until relevant filesystem events have been quiet for the
// debounce period.
func (watcher *sourceWatcher) WaitForChange(
	ctx context.Context,
	debounce time.Duration,
) error {
	var timer *time.Timer
	// A nil channel disables the timer select case until the first relevant
	// event starts the debounce window.
	var timerChannel <-chan time.Time
	defer func() {
		if timer != nil {
			timer.Stop()
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return context.Canceled
		case err, ok := <-watcher.watcher.Errors:
			if !ok {
				return errors.New("sync file watcher closed")
			}
			return fmt.Errorf("watch sync files: %w", err)
		case event, ok := <-watcher.watcher.Events:
			if !ok {
				return errors.New("sync file watcher closed")
			}
			// Maintain directory registrations for all events, even ones that
			// do not represent a source-content change.
			watcher.forgetRemovedDirectories(event)
			if err := watcher.addCreatedDirectory(event); err != nil {
				return err
			}
			if !watcher.relevant(event) {
				continue
			}
			if timer == nil {
				timer = time.NewTimer(debounce)
				timerChannel = timer.C
				continue
			}
			// Editors often save through several writes or a temp-file rename.
			// Restart the timer until that burst has gone quiet.
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			timer.Reset(debounce)
		case <-timerChannel:
			return nil
		}
	}
}

// addCreatedDirectory recursively watches a newly created directory when it
// belongs to the managed tree or leads to a referenced source.
func (watcher *sourceWatcher) addCreatedDirectory(event fsnotify.Event) error {
	if !event.Has(fsnotify.Create) {
		return nil
	}
	// The path may disappear between the event and Stat when an editor uses a
	// short-lived temporary directory. There is nothing left to register.
	info, err := os.Stat(event.Name)
	if errors.Is(err, os.ErrNotExist) || err == nil && !info.IsDir() {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect watched path %q: %w", event.Name, err)
	}
	if !watcher.shouldWatchDirectory(event.Name) {
		return nil
	}
	return watcher.addDirectoryTree(event.Name)
}

// forgetRemovedDirectories drops registrations invalidated by rename or removal.
func (watcher *sourceWatcher) forgetRemovedDirectories(event fsnotify.Event) {
	if event.Op&(fsnotify.Remove|fsnotify.Rename) == 0 {
		return
	}

	removed := filepath.Clean(event.Name)
	for directory := range watcher.directories {
		if directory == removed || pathWithin(removed, directory) {
			// Remove descendants from our bookkeeping as well; their OS watches
			// are no longer useful after an ancestor moves or disappears.
			_ = watcher.watcher.Remove(directory)
			delete(watcher.directories, directory)
		}
	}
}

// shouldWatchDirectory reports whether a directory contains managed files or
// is an ancestor of a referenced file that may not exist yet.
func (watcher *sourceWatcher) shouldWatchDirectory(directory string) bool {
	if watcher.insideManagedRoot(directory) {
		return true
	}
	for file := range watcher.files {
		if pathWithin(directory, file) {
			return true
		}
	}
	return false
}

// relevant filters noisy filesystem events down to tracked sources and newly
// created managed resources.
func (watcher *sourceWatcher) relevant(event fsnotify.Event) bool {
	if event.Op&(fsnotify.Write|fsnotify.Create|fsnotify.Remove|fsnotify.Rename) == 0 {
		return false
	}

	name := filepath.Clean(event.Name)
	if _, tracked := watcher.files[name]; tracked {
		// Tracked paths remain relevant even after remove or rename events, when
		// the path can no longer be inspected.
		return true
	}
	if event.Has(fsnotify.Create) {
		if info, err := os.Stat(name); err == nil && info.IsDir() && watcher.shouldWatchDirectory(name) {
			return true
		}
		// New resource kinds may use different filenames. Treat any new file in
		// a project subtree as relevant so watch mode does not need to know each
		// resource format. Root-level files are sync metadata such as the manifest.
		if watcher.insideManagedRoot(name) {
			relative, err := filepath.Rel(watcher.managedRoot, name)
			return err == nil && filepath.Dir(relative) != "."
		}
	}
	return false
}

// insideManagedRoot reports whether a path belongs to .launchdarkly.
func (watcher *sourceWatcher) insideManagedRoot(name string) bool {
	return pathWithin(watcher.managedRoot, name)
}

// pathWithin performs a path-aware containment check without prefix ambiguity.
func pathWithin(parent, child string) bool {
	relative, err := filepath.Rel(parent, child)
	return err == nil &&
		relative != ".." &&
		!strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

// addDirectoryTree registers every existing directory below a root.
func (watcher *sourceWatcher) addDirectoryTree(root string) error {
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return watcher.addDirectory(path)
		}
		return nil
	})
	// A missing managed tree is valid while a user deletes or recreates it. The
	// repository-root watch will report its next creation.
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("watch sync directories: %w", err)
	}
	return nil
}

// addDirectory registers one directory once and records the OS watch.
func (watcher *sourceWatcher) addDirectory(path string) error {
	path = filepath.Clean(path)
	if _, exists := watcher.directories[path]; exists {
		return nil
	}
	if err := watcher.watcher.Add(path); err != nil {
		return fmt.Errorf("watch sync directory %q: %w", path, err)
	}
	watcher.directories[path] = struct{}{}
	return nil
}

// addClosestExistingDirectory walks toward the repository root until it finds
// a directory that can observe creation of the missing descendants.
func (watcher *sourceWatcher) addClosestExistingDirectory(path string) error {
	path = filepath.Clean(path)
	// A referenced file may not exist yet. Its nearest existing ancestor is
	// enough to observe creation of the next missing path component.
	for pathWithin(watcher.root, path) {
		info, err := os.Stat(path)
		if err == nil {
			if !info.IsDir() {
				return fmt.Errorf("watch sync directory %q: not a directory", path)
			}
			return watcher.addDirectory(path)
		}
		if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("inspect sync directory %q: %w", path, err)
		}
		if path == watcher.root {
			break
		}
		path = filepath.Dir(path)
	}
	return watcher.addDirectory(watcher.root)
}

// sourceSnapshot hashes sorted source paths, bytes, and read failures so watch
// can distinguish meaningful state changes from duplicate fsnotify events.
func sourceSnapshot(repositoryRoot string) ([sha256.Size]byte, error) {
	files, err := synclocal.SourceFiles(repositoryRoot)
	if err != nil {
		return [sha256.Size]byte{}, err
	}

	hash := sha256.New()
	for _, file := range files {
		// Include both the path and bytes with separators. This distinguishes
		// renames and avoids ambiguous concatenations across adjacent files.
		_, _ = io.WriteString(hash, file)
		_, _ = hash.Write([]byte{0})
		content, err := os.ReadFile(filepath.Join(repositoryRoot, filepath.FromSlash(file)))
		if err != nil {
			// Missing or unreadable files are still meaningful states. Hash the
			// error so deletion and later recreation produce different snapshots.
			_, _ = io.WriteString(hash, "!"+err.Error())
		} else {
			_, _ = hash.Write(content)
		}
		_, _ = hash.Write([]byte{0})
	}

	var snapshot [sha256.Size]byte
	copy(snapshot[:], hash.Sum(nil))
	return snapshot, nil
}
