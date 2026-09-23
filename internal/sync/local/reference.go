package local

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
)

// Reference identifies an external prompt file and the adapter used to parse
// it into LaunchDarkly's variation model.
type Reference struct {
	File   string `yaml:"file"`
	Format string `yaml:"format"`
}

// NewReference validates a user-supplied path and converts it to the
// repository-relative path stored in frontmatter.
func NewReference(repositoryRoot, workingDirectory, file, format string) (Reference, error) {
	if file == "" {
		return Reference{}, fmt.Errorf("linked file path is required")
	}
	if !filepath.IsAbs(file) {
		file = filepath.Join(workingDirectory, file)
	}
	target, err := filepath.EvalSymlinks(file)
	if err != nil {
		return Reference{}, fmt.Errorf("resolve linked file %q: %w", file, err)
	}
	root, err := filepath.EvalSymlinks(repositoryRoot)
	if err != nil {
		return Reference{}, fmt.Errorf("resolve repository root: %w", err)
	}
	relative, err := filepath.Rel(root, target)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return Reference{}, fmt.Errorf("linked file must be inside the Git repository")
	}

	reference := Reference{File: filepath.ToSlash(relative), Format: format}
	if _, err := resolveReferencePath(root, reference); err != nil {
		return Reference{}, err
	}
	return reference, nil
}

func readReferenceFromFS(fsys fs.FS, reference Reference) ([]byte, error) {
	if err := validateReference(reference); err != nil {
		return nil, err
	}
	data, err := fs.ReadFile(fsys, reference.File)
	if err != nil {
		return nil, fmt.Errorf("read referenced file %q: %w", reference.File, err)
	}
	return data, nil
}

func readWorkspaceReference(repositoryRoot string, reference Reference) ([]byte, error) {
	target, err := resolveReferencePath(repositoryRoot, reference)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(target)
	if err != nil {
		return nil, fmt.Errorf("read referenced file %q: %w", reference.File, err)
	}
	return data, nil
}

func resolveReferencePath(repositoryRoot string, reference Reference) (string, error) {
	if err := validateReference(reference); err != nil {
		return "", err
	}

	root, err := filepath.EvalSymlinks(repositoryRoot)
	if err != nil {
		return "", fmt.Errorf("resolve repository root: %w", err)
	}
	target, err := filepath.EvalSymlinks(filepath.Join(root, filepath.FromSlash(reference.File)))
	if err != nil {
		return "", fmt.Errorf("resolve referenced file %q: %w", reference.File, err)
	}
	relative, err := filepath.Rel(root, target)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("referenced file %q resolves outside the Git repository", reference.File)
	}
	info, err := os.Stat(target)
	if err != nil {
		return "", fmt.Errorf("inspect referenced file %q: %w", reference.File, err)
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("referenced file %q is not a regular file", reference.File)
	}
	return target, nil
}

func validateReference(reference Reference) error {
	switch {
	case reference.File == "":
		return fmt.Errorf("ref.file is required")
	case reference.Format == "":
		return fmt.Errorf("ref.format is required")
	case !fs.ValidPath(reference.File):
		return fmt.Errorf("ref.file %q must be a repository-relative path", reference.File)
	case reference.File == syncdomain.RootDir || strings.HasPrefix(reference.File, syncdomain.RootDir+"/"):
		return fmt.Errorf("ref.file %q must be outside %s", reference.File, syncdomain.RootDir)
	case path.Clean(reference.File) != reference.File:
		return fmt.Errorf("ref.file %q must be a clean repository-relative path", reference.File)
	default:
		return nil
	}
}

// SourceFiles returns managed variation wrappers and their referenced prompt
// files. Invalid wrappers remain watched so fixing them triggers sync.
func SourceFiles(repositoryRoot string) ([]string, error) {
	managedRoot := filepath.Join(repositoryRoot, syncdomain.RootDir)
	var files []string
	err := filepath.WalkDir(managedRoot, func(filePath string, entry os.DirEntry, walkErr error) error {
		if errors.Is(walkErr, os.ErrNotExist) {
			return nil
		}
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), variationFileSuffix) {
			return nil
		}

		relative, err := filepath.Rel(repositoryRoot, filePath)
		if err != nil {
			return err
		}
		files = append(files, filepath.ToSlash(relative))

		content, err := os.ReadFile(filePath)
		if err != nil {
			return nil
		}
		var metadata variationFrontMatter
		if _, err := parseYAMLFrontMatter(content, &metadata); err == nil && metadata.Ref != nil && validateReference(*metadata.Ref) == nil {
			files = append(files, metadata.Ref.File)
		}
		return nil
	})
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find prompt source files: %w", err)
	}

	slices.Sort(files)
	return slices.Compact(files), nil
}
