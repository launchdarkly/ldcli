package local

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
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

// DeleteResources deletes the files of variations, tools, and skills in one
// transaction, so that a failure deletes none of them. It returns their paths.
func (store Store) DeleteResources(ids []syncdomain.ResourceID) ([]string, error) {
	paths := make([]string, 0, len(ids))
	for _, id := range ids {
		relativePath, err := resourcePath(id)
		if err != nil {
			return nil, err
		}
		paths = append(paths, relativePath)
	}
	return store.deleteFiles(paths)
}

// DeleteDefinitions deletes each descriptor when present, or its managed file.
// It never deletes the external file that a descriptor references.
func (store Store) DeleteDefinitions(ids []syncdomain.ResourceID) ([]string, error) {
	paths := make([]string, 0, len(ids))
	for _, id := range ids {
		reference, err := ReferencePath(id)
		if err != nil {
			return nil, err
		}
		reference = strings.TrimPrefix(reference, syncdomain.RootDir+"/")
		referenced, err := store.fileExists(reference, id.Label()+" descriptor")
		if err != nil {
			return nil, err
		}
		managed, err := resourcePath(id)
		if err != nil {
			return nil, err
		}
		// A leftover link at the managed path is not a definition, as it is
		// not for compile, so the descriptor beside it is the one to delete.
		managedExists, err := store.fileExists(managed, id.Label())
		if err != nil {
			return nil, err
		}
		switch {
		case referenced && managedExists:
			return nil, definitionConflict(id, id.Label()+" has a managed file and a descriptor")
		case referenced:
			paths = append(paths, reference)
			// A leftover link beside the descriptor goes with it, in the
			// same transaction, so that the link does not stand alone, where
			// compile rejects it.
			if link, err := store.isLink(managed); err != nil {
				return nil, err
			} else if link {
				paths = append(paths, managed)
			}
		case managedExists:
			paths = append(paths, managed)
		default:
			return nil, fmt.Errorf("inspect definition %s: %w", id.Label(), os.ErrNotExist)
		}
	}
	return store.deleteFiles(paths)
}

// isLink reports whether a managed path is a symbolic link.
func (store Store) isLink(relativePath string) (bool, error) {
	info, err := os.Lstat(store.absolute(relativePath))
	switch {
	case err == nil:
		return info.Mode()&os.ModeSymlink != 0, nil
	case errors.Is(err, os.ErrNotExist):
		return false, nil
	default:
		return false, fmt.Errorf("inspect %s: %w", relativePath, err)
	}
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
		// Lstat does not follow a symbolic link, so a link is not a regular
		// file. A link itself can go, as a rename moves the link and not its
		// target, but no parent of a managed file is a link.
		info, err := os.Lstat(path)
		if err != nil {
			return nil, fmt.Errorf("inspect %s: %w", relativePath, err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			if err := rejectSymlinkedPath(store.root, filepath.Dir(path)); err != nil {
				return nil, err
			}
		} else {
			if err := rejectSymlinkedPath(store.root, path); err != nil {
				return nil, err
			}
			if !info.Mode().IsRegular() {
				return nil, fmt.Errorf("%s is not a regular file", relativePath)
			}
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
