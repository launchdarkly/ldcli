package local

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
)

// Reference is the "ref" field of a linked variation. It names a file in the
// repository and the format of that file.
type Reference = syncdomain.Reference

// NewReference converts a path that the user entered to a reference. The
// path can be relative to workingDirectory. The file must be in the repository.
func NewReference(repositoryRoot, workingDirectory, file, format string) (Reference, error) {
	if file == "" {
		return Reference{}, errors.New("linked file path is required")
	}
	if !filepath.IsAbs(file) {
		file = filepath.Join(workingDirectory, file)
	}
	// Compare resolved paths. A path that looks like it is in the repository
	// can go out of it through a symbolic link.
	target, err := filepath.EvalSymlinks(file)
	if err != nil {
		return Reference{}, fmt.Errorf("resolve linked file %q: %w", file, err)
	}
	root, err := filepath.EvalSymlinks(repositoryRoot)
	if err != nil {
		return Reference{}, fmt.Errorf("resolve repository root: %w", err)
	}
	if !isWithin(root, target) {
		return Reference{}, errors.New("linked file must be inside the Git repository")
	}

	relative, _ := filepath.Rel(root, target)
	reference := Reference{File: filepath.ToSlash(relative), Format: format}
	if _, err := resolveReferencePath(root, reference); err != nil {
		return Reference{}, err
	}
	return reference, nil
}

// validateReference makes sure that the file is a clean path in the
// repository and outside the managed directory.
func validateReference(reference Reference) error {
	switch {
	case reference.File == "":
		return errors.New("ref.file is required")
	case reference.Format == "":
		return errors.New("ref.format is required")
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

// resolveReferencePath returns the absolute path of a linked file. It
// resolves symbolic links on each call, because a link can change after the
// variation was linked.
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
	if !isWithin(root, target) {
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

// ReadReference reads a linked file. It resolves symbolic links and makes sure
// that the file is a regular file in the repository.
func ReadReference(repositoryRoot string, reference Reference) ([]byte, error) {
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
