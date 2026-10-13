package local

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// stagedDeletion is one file in a delete transaction. The file first moves to
// backupPath, so that a failure can move it back.
type stagedDeletion struct {
	relativePath string
	path         string
	backupPath   string
}

// DeleteVariations deletes a batch of variation files and returns their paths.
func (store Store) DeleteVariations(deletions []VariationDeletion) ([]string, error) {
	paths := make([]string, 0, len(deletions))
	for _, deletion := range deletions {
		relativePath, err := variationPath(deletion.ProjectKey, deletion.ConfigKey, deletion.VariationKey)
		if err != nil {
			return nil, err
		}
		paths = append(paths, relativePath)
	}
	return store.deleteFiles(paths)
}

// deleteFiles deletes a batch of managed files in one transaction. It first
// moves every file to a backup beside it. If a move fails, it moves the
// files back. When every file has moved, it removes the backups.
func (store Store) deleteFiles(relativePaths []string) ([]string, error) {
	deletions := make([]stagedDeletion, 0, len(relativePaths))
	seen := make(map[string]struct{}, len(relativePaths))
	for _, relativePath := range relativePaths {
		if _, duplicate := seen[relativePath]; duplicate {
			return nil, fmt.Errorf("%s was selected more than once", relativePath)
		}
		seen[relativePath] = struct{}{}

		path := store.absolute(relativePath)
		if err := rejectSymlinkedPath(store.root, path); err != nil {
			return nil, err
		}
		// Lstat does not follow a symbolic link, so a link is not a regular file.
		info, err := os.Lstat(path)
		if err != nil {
			return nil, fmt.Errorf("inspect %s: %w", relativePath, err)
		}
		if !info.Mode().IsRegular() {
			return nil, fmt.Errorf("%s is not a regular file", relativePath)
		}
		deletions = append(deletions, stagedDeletion{relativePath: relativePath, path: path})
	}

	if err := stageDeletions(deletions); err != nil {
		return nil, err
	}
	// When every file is in its backup, the delete is complete. Removing a
	// backup can fail, but restoring only some files is worse than a backup
	// that remains.
	for _, deletion := range deletions {
		if err := os.Remove(deletion.backupPath); err == nil {
			removeEmptyParents(store.root, filepath.Dir(deletion.path))
		}
	}
	return relativePaths, nil
}

// stageDeletions moves each file to a backup. If a move fails, it moves the
// earlier files back.
func stageDeletions(deletions []stagedDeletion) error {
	for index := range deletions {
		backupPath, err := reserveBackupPath(deletions[index].path)
		if err == nil {
			err = os.Rename(deletions[index].path, backupPath)
		}
		if err != nil {
			return errors.Join(
				fmt.Errorf("stage deletion %s: %w", deletions[index].relativePath, err),
				restoreDeletions(deletions[:index]),
			)
		}
		deletions[index].backupPath = backupPath
	}
	return nil
}

// restoreDeletions moves each backup to its original path, in reverse order.
func restoreDeletions(deletions []stagedDeletion) error {
	var failures []error
	for index := len(deletions) - 1; index >= 0; index-- {
		if err := os.Rename(deletions[index].backupPath, deletions[index].path); err != nil {
			failures = append(failures, fmt.Errorf("restore %s: %w", deletions[index].relativePath, err))
		}
	}
	return errors.Join(failures...)
}

// reserveBackupPath returns an unused name beside path. It creates a
// temporary file to reserve the name, and then removes the file so that a
// rename can use the name.
func reserveBackupPath(path string) (string, error) {
	temp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".deleted-")
	if err != nil {
		return "", err
	}
	backupPath := temp.Name()
	if err := temp.Close(); err != nil {
		_ = os.Remove(backupPath)
		return "", err
	}
	if err := os.Remove(backupPath); err != nil {
		return "", err
	}
	return backupPath, nil
}

// removeEmptyParents removes directory and each empty parent, up to and
// including root.
func removeEmptyParents(root, directory string) {
	for isWithin(root, directory) {
		if err := os.Remove(directory); err != nil || directory == root {
			return
		}
		directory = filepath.Dir(directory)
	}
}
