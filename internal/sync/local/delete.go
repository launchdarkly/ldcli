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

// DeleteVariations atomically removes a batch of local variation wrappers.
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

func (store Store) prepareDeletions(resources []VariationDeletion) ([]stagedDeletion, error) {
	deletions := make([]stagedDeletion, 0, len(resources))
	seenPaths := make(map[string]struct{}, len(resources))

	for _, resource := range resources {
		existing, err := store.inspectVariation(resource.ProjectKey, resource.ConfigKey, resource.VariationKey)
		if err != nil {
			return nil, err
		}
		if _, duplicate := seenPaths[existing.absolutePath]; duplicate {
			return nil, fmt.Errorf("variation %q was selected more than once", resource.VariationKey)
		}
		seenPaths[existing.absolutePath] = struct{}{}
		deletions = append(deletions, stagedDeletion{
			relativePath: existing.relativePath,
			originalPath: existing.absolutePath,
		})
	}
	return deletions, nil
}

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

func reserveBackupPath(originalPath string) (string, error) {
	temp, err := os.CreateTemp(filepath.Dir(originalPath), "."+filepath.Base(originalPath)+".deleted-")
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

func commitDeletions(root string, deletions []stagedDeletion) error {
	for _, deletion := range deletions {
		if err := os.Remove(deletion.backupPath); err != nil {
			return fmt.Errorf("finish deleting variation %s: %w", deletion.relativePath, err)
		}
		removeEmptyParentsThroughRoot(root, filepath.Dir(deletion.originalPath))
	}
	return nil
}

func deletionPaths(deletions []stagedDeletion) []string {
	paths := make([]string, len(deletions))
	for index, deletion := range deletions {
		paths[index] = deletion.relativePath
	}
	return paths
}

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
