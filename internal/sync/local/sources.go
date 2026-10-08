package local

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
)

// SourceFiles returns every file that can change the sync plan, relative to
// the repository root. The list has each file in a project directory and
// each file that a variation links to. A file that is not valid stays in the
// list, so that a fix to the file starts a new sync in watch mode.
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
		if strings.HasSuffix(entry.Name(), variationFileSuffix) {
			if reference, ok := linkedFile(filePath); ok {
				files = append(files, reference)
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

// linkedFile returns the file that a variation file links to. It ignores a
// variation file that it cannot parse, because compile reports that error.
func linkedFile(file string) (string, bool) {
	content, err := os.ReadFile(file)
	if err != nil {
		return "", false
	}
	var metadata variationFrontMatter
	if _, err := parseYAMLFrontMatter(content, &metadata); err != nil {
		return "", false
	}
	if metadata.Ref == nil || validateReference(*metadata.Ref) != nil {
		return "", false
	}
	return metadata.Ref.File, true
}
