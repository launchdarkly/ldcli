package prompt

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	synclocal "github.com/launchdarkly/ldcli/internal/sync/local"
)

const (
	watchPollInterval = 100 * time.Millisecond
	watchDebounce     = 300 * time.Millisecond
)

var errRefreshWatchPlan = errors.New("refresh watch plan")

type watchRunner func(context.Context, string, time.Duration, time.Duration, func() error, io.Writer) error

// watchWorkspace waits for a stable source change before running sync. The
// initial snapshot is only a baseline and never triggers a sync.
func watchWorkspace(
	ctx context.Context,
	repositoryRoot string,
	pollInterval time.Duration,
	debounce time.Duration,
	syncWorkspace func() error,
	output io.Writer,
) error {
	files, err := synclocal.SourceFiles(repositoryRoot)
	if err != nil {
		return err
	}
	_, _ = fmt.Fprintln(output, "Watching prompt files:")
	for _, file := range files {
		_, _ = fmt.Fprintf(output, "- %s\n", file)
	}

	handledSnapshot, err := sourceSnapshot(repositoryRoot)
	if err != nil {
		return err
	}

	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()
	var candidateSnapshot [sha256.Size]byte
	var pendingSince time.Time

	for {
		select {
		case <-ctx.Done():
			return nil
		case now := <-ticker.C:
			current, err := sourceSnapshot(repositoryRoot)
			if err != nil {
				_, _ = fmt.Fprintf(output, "Watch error: %s\n", err)
				continue
			}
			if current == handledSnapshot {
				pendingSince = time.Time{}
				continue
			}
			if pendingSince.IsZero() || current != candidateSnapshot {
				candidateSnapshot, pendingSince = current, now
				continue
			}
			if now.Sub(pendingSince) < debounce {
				continue
			}

			_, _ = fmt.Fprintln(output, "Prompt files changed; syncing...")
			err = syncWorkspace()
			if errors.Is(err, errRefreshWatchPlan) {
				pendingSince = time.Time{}
				continue
			}
			if err != nil {
				_, _ = fmt.Fprintf(output, "Sync failed: %s\n", err)
				// Treat this snapshot as handled. Another file change will
				// trigger a new attempt without repeatedly reporting the same failure.
				handledSnapshot = current
			} else {
				handledSnapshot, err = sourceSnapshot(repositoryRoot)
				if err != nil {
					return err
				}
			}
			pendingSince = time.Time{}
		}
	}
}

func sourceSnapshot(repositoryRoot string) ([sha256.Size]byte, error) {
	files, err := synclocal.SourceFiles(repositoryRoot)
	if err != nil {
		return [sha256.Size]byte{}, err
	}

	hash := sha256.New()
	for _, file := range files {
		_, _ = io.WriteString(hash, file)
		_, _ = hash.Write([]byte{0})
		content, err := os.ReadFile(filepath.Join(repositoryRoot, filepath.FromSlash(file)))
		if err != nil {
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
