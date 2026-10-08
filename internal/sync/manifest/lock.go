package manifest

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"

	"gopkg.in/yaml.v3"
)

// The sync.lock file stores the baseline of one working copy. Engineers commit
// it with the .launchdarkly files, so that the baseline moves with the files.
const (
	lockFormatVersion = 1
	lockHeader        = "# Written by ldcli sync. Commit this file with the .launchdarkly files.\n"
)

// LockFile reads and writes the sync.lock file of a working copy. ReadLock
// returns an error that matches fs.ErrNotExist when the file does not exist.
// WriteLock with empty content removes the file.
type LockFile interface {
	ReadLock() ([]byte, error)
	WriteLock(content []byte) error
}

type lockDocument struct {
	FormatVersion int        `yaml:"formatVersion"`
	Resources     []Resource `yaml:"resources"`
}

// ReadLock decodes the sync.lock file. The bool result is false when the file
// does not exist.
func ReadLock(lock LockFile) (Manifest, bool, error) {
	content, err := lock.ReadLock()
	if errors.Is(err, fs.ErrNotExist) {
		return New(), false, nil
	}
	if err != nil {
		return Manifest{}, false, fmt.Errorf("read sync.lock: %w", err)
	}
	manifest, err := decodeLock(content)
	if err != nil {
		return Manifest{}, false, fmt.Errorf("read sync.lock: %w", err)
	}
	return manifest, true, nil
}

// writeLock encodes the manifest to the sync.lock file. A manifest without
// entries removes the file, so that a workspace without resources has no
// .launchdarkly directory.
func writeLock(lock LockFile, manifest Manifest) error {
	content, err := encodeLock(manifest)
	if err != nil {
		return err
	}
	if err := lock.WriteLock(content); err != nil {
		return fmt.Errorf("write sync.lock: %w", err)
	}
	return nil
}

// encodeLock returns the lock content, with the entries in identity order so
// that a Git diff shows only the entries that changed.
func encodeLock(manifest Manifest) ([]byte, error) {
	if len(manifest.Resources) == 0 {
		return nil, nil
	}
	manifest = manifest.Clone()
	manifest.Sort()

	var content bytes.Buffer
	content.WriteString(lockHeader)
	encoder := yaml.NewEncoder(&content)
	encoder.SetIndent(2)
	if err := encoder.Encode(lockDocument{FormatVersion: lockFormatVersion, Resources: manifest.Resources}); err != nil {
		return nil, fmt.Errorf("encode sync.lock: %w", err)
	}
	if err := encoder.Close(); err != nil {
		return nil, fmt.Errorf("encode sync.lock: %w", err)
	}
	return content.Bytes(), nil
}

func decodeLock(content []byte) (Manifest, error) {
	var document lockDocument
	decoder := yaml.NewDecoder(bytes.NewReader(content))
	decoder.KnownFields(true)
	if err := decoder.Decode(&document); err != nil {
		return Manifest{}, fmt.Errorf("decode: %w", err)
	}
	if document.FormatVersion != lockFormatVersion {
		return Manifest{}, fmt.Errorf("unsupported formatVersion %d", document.FormatVersion)
	}

	manifest := Manifest{Resources: document.Resources}
	if manifest.Resources == nil {
		manifest = New()
	}
	if err := manifest.Validate(); err != nil {
		return Manifest{}, err
	}
	manifest.Sort()
	return manifest, nil
}
