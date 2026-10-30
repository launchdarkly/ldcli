package local

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
)

// SourceFiles returns every file that can change the sync plan, relative to
// the repository root. The list has each file in a project directory and
// each managed file. A file that is not valid stays in the list, so that a
// fix to the file starts a new sync in watch mode.
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
