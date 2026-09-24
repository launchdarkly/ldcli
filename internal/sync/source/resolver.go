package source

import (
	"errors"
	"fmt"
	"path/filepath"

	"github.com/launchdarkly/ldcli/internal/sync/repository"
)

// ErrGitRequired reports that sync was run outside an initialized Git repository.
var ErrGitRequired = errors.New("git is required to use sync; initialize a Git repository")

// Workspace identifies the repository root used by sync.
type Workspace struct {
	Root string
}

// Resolver finds the Git workspace containing a requested directory.
type Resolver struct {
	findGitRepository func(string) (repository.GitRepository, bool, error)
}

// NewResolver creates a Git-backed workspace resolver.
func NewResolver() Resolver {
	return Resolver{
		findGitRepository: repository.FindGitRepository,
	}
}

// Resolve returns the canonical root of the containing Git repository.
func (resolver Resolver) Resolve(dir string) (Workspace, error) {
	gitRepository, found, err := resolver.findGitRepository(dir)
	if err != nil {
		return Workspace{}, err
	}
	if !found {
		return Workspace{}, ErrGitRequired
	}

	root, err := canonicalPath(gitRepository.Root)
	if err != nil {
		return Workspace{}, err
	}

	return Workspace{Root: root}, nil
}

// canonicalPath resolves symlinks and returns an absolute, clean path so every
// sync component agrees on one repository identity.
func canonicalPath(path string) (string, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve absolute workspace path: %w", err)
	}

	resolved, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return "", fmt.Errorf("resolve workspace symlinks: %w", err)
	}

	return filepath.Clean(resolved), nil
}
