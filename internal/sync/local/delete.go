package local

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

type stagedDeletion struct {
	relativePath string
	originalPath string
	backupPath   string
}

// DeleteVariations preflights and stages a batch before removing any wrapper
// permanently, allowing staging failures to restore the original files.
func (store Store) DeleteVariations(resources []VariationDeletion) ([]string, error) {
	deletions, err := store.prepareDeletions(resources)
	if err != nil {
		return nil, err
	}

	// Move the complete batch to same-directory backups before removing
	// anything permanently. A staging failure can therefore restore every file.
	if err := stageDeletions(deletions); err != nil {
		return nil, err
	}
	if err := commitDeletions(store.root, deletions); err != nil {
		return nil, err
	}
	return deletionPaths(deletions), nil
}

// prepareDeletions validates the complete batch and rejects duplicate paths
// before filesystem state changes.
func (store Store) prepareDeletions(resources []VariationDeletion) ([]stagedDeletion, error) {
	deletions := make([]stagedDeletion, 0, len(resources))
	seenPaths := make(map[string]struct{}, len(resources))

	for _, resource := range resources {
		absolutePath, err := store.variationPath(resource.ProjectKey, resource.ConfigKey, resource.VariationKey)
		if err != nil {
			return nil, err
		}
		// Use Lstat so the regular-file check rejects symlink wrappers rather
		// than following them to a file outside the managed workspace.
		info, err := os.Lstat(absolutePath)
		if err != nil {
			return nil, fmt.Errorf("inspect variation %s: %w", resource.VariationKey, err)
		}
		if !info.Mode().IsRegular() {
			return nil, fmt.Errorf("variation %s is not a regular file", resource.VariationKey)
		}
		if _, duplicate := seenPaths[absolutePath]; duplicate {
			return nil, fmt.Errorf("variation %q was selected more than once", resource.VariationKey)
		}
		seenPaths[absolutePath] = struct{}{}
		relativePath, err := filepath.Rel(store.root, absolutePath)
		if err != nil {
			return nil, fmt.Errorf("resolve variation %s: %w", resource.VariationKey, err)
		}
		deletions = append(deletions, stagedDeletion{
			relativePath: filepath.ToSlash(relativePath),
			originalPath: absolutePath,
		})
	}
	return deletions, nil
}

// stageDeletions renames wrappers to same-directory backups and restores prior
// renames if any later rename fails.
func stageDeletions(deletions []stagedDeletion) error {
	var staged []stagedDeletion
	for index := range deletions {
		backupPath, err := reserveBackupPath(deletions[index].originalPath)
		if err == nil {
			err = os.Rename(deletions[index].originalPath, backupPath)
		}
		if err != nil {
			return errors.Join(fmt.Errorf("stage deletion %s: %w", deletions[index].relativePath, err), rollbackDeletions(staged))
		}

		deletions[index].backupPath = backupPath
		staged = append(staged, deletions[index])
	}
	return nil
}

// reserveBackupPath obtains a collision-free backup name beside a wrapper.
func reserveBackupPath(originalPath string) (string, error) {
	temp, err := os.CreateTemp(filepath.Dir(originalPath), "."+filepath.Base(originalPath)+".deleted-")
	if err != nil {
		return "", err
	}

	backupPath := temp.Name()
	// CreateTemp reserves a collision-free name. Removing the placeholder lets
	// Rename move the original into that exact same-directory location.
	if err := temp.Close(); err != nil {
		_ = os.Remove(backupPath)
		return "", err
	}
	if err := os.Remove(backupPath); err != nil {
		return "", err
	}
	return backupPath, nil
}

// commitDeletions removes staged backups and restores every remaining backup
// if cleanup cannot continue.
func commitDeletions(root string, deletions []stagedDeletion) error {
	for index, deletion := range deletions {
		if err := os.Remove(deletion.backupPath); err != nil {
			// Backups deleted earlier are already committed. Restore every
			// remaining backup so no additional resources are lost.
			return errors.Join(
				fmt.Errorf("finish deleting variation %s: %w", deletion.relativePath, err),
				rollbackDeletions(deletions[index:]),
			)
		}
		removeEmptyParentsThroughRoot(root, filepath.Dir(deletion.originalPath))
	}
	return nil
}

// deletionPaths returns the stable repository-relative paths reported to callers.
func deletionPaths(deletions []stagedDeletion) []string {
	paths := make([]string, len(deletions))
	for index, deletion := range deletions {
		paths[index] = deletion.relativePath
	}
	return paths
}

// rollbackDeletions restores staged wrappers in reverse order.
func rollbackDeletions(deletions []stagedDeletion) error {
	var rollbackErr error
	for index := len(deletions) - 1; index >= 0; index-- {
		if err := os.Rename(deletions[index].backupPath, deletions[index].originalPath); err != nil {
			rollbackErr = errors.Join(rollbackErr, fmt.Errorf("restore variation %s: %w", deletions[index].relativePath, err))
		}
	}
	return rollbackErr
}

// removeEmptyParentsThroughRoot removes empty variation, config, and project
// directories. It also removes the .launchdarkly root when the store is empty.
func removeEmptyParentsThroughRoot(root, current string) {
	for {
		if err := os.Remove(current); err != nil {
			return
		}
		if current == root {
			return
		}
		current = filepath.Dir(current)
	}
}
