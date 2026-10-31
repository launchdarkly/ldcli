package local

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
	syncreference "github.com/launchdarkly/ldcli/internal/sync/reference"
)

// SourceFiles returns every file that can change the sync plan, relative to
// the repository root. The list has each managed or referenced file. A file
// that is not valid stays in the list, so a fix starts a new sync in watch mode.
func SourceFiles(repositoryRoot string) ([]string, error) {
	managedRoot := filepath.Join(repositoryRoot, syncdomain.RootDir)
	var files []string
	err := filepath.WalkDir(managedRoot, func(filePath string, entry os.DirEntry, err error) error {
		switch {
		case errors.Is(err, os.ErrNotExist):
			return nil
		case err != nil:
			return err
		case entry.IsDir():
			return nil
		}

		relative, err := filepath.Rel(repositoryRoot, filePath)
		if err != nil {
			return err
		}
		// A file directly in the managed directory is not in a project.
		if filepath.Dir(filePath) != managedRoot {
			files = append(files, filepath.ToSlash(relative))
		}
		relative = filepath.ToSlash(relative)
		if id, ok := ParseReferencePath(relative); ok {
			if reference, err := ReadDescriptor(os.DirFS(repositoryRoot), relative, id); err == nil {
				files = append(files, reference.File)
				if content, err := ReadReference(repositoryRoot, reference); err == nil {
					dependencies, _ := syncreference.DependencyPaths(
						reference.Format,
						syncreference.Document{Path: reference.File, Data: content},
					)
					files = append(files, dependencies...)
				}
			}
		}
		return nil
	})
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find sync source files: %w", err)
	}

	slices.Sort(files)
	return slices.Compact(files), nil
}
