package local

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
)

// lockFileName is the file that stores the sync baseline of the working copy.
// The manifest package owns its format.
const lockFileName = "sync.lock"

// ReadLock returns the content of the sync.lock file. The error matches
// fs.ErrNotExist when the file does not exist.
func (store Store) ReadLock() ([]byte, error) {
	path := filepath.Join(store.root, lockFileName)
	if err := rejectSymlinkedPath(store.root, path); err != nil {
		return nil, err
	}
	return os.ReadFile(path)
}

// WriteLock replaces the sync.lock file atomically. It does not write a file
// whose content is the same. Empty content removes the file.
func (store Store) WriteLock(content []byte) error {
	path := filepath.Join(store.root, lockFileName)
	if err := rejectSymlinkedPath(store.root, path); err != nil {
		return err
	}
	if len(content) == 0 {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}
	if current, err := os.ReadFile(path); err == nil && bytes.Equal(current, content) {
		return nil
	}

	tempPath, err := writeTempFile(path, content, 0o644)
	if err != nil {
		return err
	}
	if err := os.Rename(tempPath, path); err != nil {
		_ = os.Remove(tempPath)
		return err
	}
	return nil
}
