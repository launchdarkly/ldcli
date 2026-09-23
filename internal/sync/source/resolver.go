package source

import (
	"errors"
	"fmt"
	"path/filepath"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
	"github.com/launchdarkly/ldcli/internal/sync/repository"
)

var ErrGitRequired = errors.New(
	"git is required to use sync; initialize a Git repository and configure an origin remote",
)

type Workspace struct {
	Root   string
	Source syncdomain.Source
}

type Resolver struct {
	findGitSource func(string) (repository.GitRepository, bool, error)
}

func NewResolver() Resolver {
	return Resolver{
		findGitSource: repository.FindGitSource,
	}
}

func (resolver Resolver) Resolve(dir string) (Workspace, error) {
	gitRepository, found, err := resolver.findGitSource(dir)
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

	return Workspace{Root: root, Source: gitRepository.Source}, nil
}

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
