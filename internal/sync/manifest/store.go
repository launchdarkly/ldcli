package manifest

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
	"gopkg.in/yaml.v3"
)

// FileName is the repository-local sync manifest filename.
const FileName = "manifest.yaml"

// Store reads and atomically writes the committed synchronization manifest.
type Store struct {
	path string
}

// NewStore creates a manifest store rooted at the Git repository.
func NewStore(repositoryRoot string) Store {
	return Store{path: filepath.Join(repositoryRoot, syncdomain.RootDir, FileName)}
}

// Load returns the manifest and whether it already exists.
func (store Store) Load() (Manifest, bool, error) {
	data, err := os.ReadFile(store.path)
	if errors.Is(err, os.ErrNotExist) {
		return New(), false, nil
	}
	if err != nil {
		return Manifest{}, false, fmt.Errorf("read sync manifest: %w", err)
	}

	decoder := yaml.NewDecoder(bytes.NewReader(data))
	// A committed manifest is an API between CLI versions. Reject unknown
	// fields instead of silently discarding data written by a newer schema.
	decoder.KnownFields(true)
	var manifest Manifest
	if err := decoder.Decode(&manifest); err != nil {
		return Manifest{}, false, fmt.Errorf("decode sync manifest: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			err = fmt.Errorf("multiple YAML documents are not supported")
		}
		return Manifest{}, false, fmt.Errorf("decode sync manifest: %w", err)
	}
	if err := manifest.Validate(); err != nil {
		return Manifest{}, false, fmt.Errorf("validate sync manifest: %w", err)
	}
	manifest.Sort()
	return manifest, true, nil
}

// Write atomically replaces the manifest with deterministic YAML.
func (store Store) Write(manifest Manifest) error {
	manifest.FormatVersion = FormatVersion
	if manifest.Resources == nil {
		manifest.Resources = []Resource{}
	}
	manifest.Sort()
	if err := manifest.Validate(); err != nil {
		return fmt.Errorf("validate sync manifest: %w", err)
	}

	var data bytes.Buffer
	encoder := yaml.NewEncoder(&data)
	encoder.SetIndent(2)
	if err := encoder.Encode(manifest); err != nil {
		return fmt.Errorf("encode sync manifest: %w", err)
	}
	if err := encoder.Close(); err != nil {
		return fmt.Errorf("encode sync manifest: %w", err)
	}

	directory := filepath.Dir(store.path)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return fmt.Errorf("create sync directory: %w", err)
	}
	temporary, err := os.CreateTemp(directory, ".manifest-*.yaml")
	if err != nil {
		return fmt.Errorf("create temporary sync manifest: %w", err)
	}
	temporaryPath := temporary.Name()
	defer func() { _ = os.Remove(temporaryPath) }()

	// Flush and close the complete temporary file before the single rename
	// commit point, so readers observe either the old or the new manifest.
	if err := temporary.Chmod(0o644); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("set sync manifest permissions: %w", err)
	}
	if _, err := temporary.Write(data.Bytes()); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("write sync manifest: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("sync manifest contents: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close sync manifest: %w", err)
	}
	if err := os.Rename(temporaryPath, store.path); err != nil {
		return fmt.Errorf("replace sync manifest: %w", err)
	}

	// The rename above is the commit point. Directory syncing improves crash
	// durability where the platform supports it, but must not turn a committed
	// replacement into a reported failure.
	if directoryHandle, err := os.Open(directory); err == nil {
		_ = directoryHandle.Sync()
		_ = directoryHandle.Close()
	}
	return nil
}

// Remove deletes the manifest when rolling back creation of a new manifest.
func (store Store) Remove() error {
	if err := os.Remove(store.path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove sync manifest: %w", err)
	}
	return nil
}
