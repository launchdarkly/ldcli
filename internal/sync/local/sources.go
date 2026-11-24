package local

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
	syncadapter "github.com/launchdarkly/ldcli/internal/sync/adapter"
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
				files = append(files, sourcePaths(repositoryRoot, reference.File)...)
				if content, err := ReadSourceFile(repositoryRoot, reference.File); err == nil {
					dependencies, _ := syncadapter.DependencyPaths(
						reference.Format,
						syncadapter.Document{Path: reference.File, Data: content},
					)
					for _, dependency := range dependencies {
						files = append(files, sourcePaths(repositoryRoot, dependency)...)
					}
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

// sourcePaths returns a repository-relative source path with the path of the
// file that it resolves to, when a symbolic link makes the two differ. A
// compile and a publish read the resolved file, so an edit to it changes
// the plan.
func sourcePaths(repositoryRoot, file string) []string {
	paths := []string{file}
	resolved, err := filepath.EvalSymlinks(filepath.Join(repositoryRoot, filepath.FromSlash(file)))
	if err != nil {
		return paths
	}
	root, err := filepath.EvalSymlinks(repositoryRoot)
	if err != nil {
		return paths
	}
	relative, err := filepath.Rel(root, resolved)
	if err != nil || relative == "." || strings.HasPrefix(relative, "..") {
		return paths
	}
	// A link into the managed directory is not a source, so its target does
	// not go on the list: a write of the lock would start a sync again.
	if inManagedDirectory(relative) {
		return paths
	}
	if relative = filepath.ToSlash(relative); relative != file {
		paths = append(paths, relative)
	}
	return paths
}
