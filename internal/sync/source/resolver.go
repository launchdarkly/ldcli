package source

import (
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"

	"github.com/launchdarkly/ldcli/internal/sync/repository"
)

// ErrGitRequired reports that sync was run outside an initialized Git repository.
var ErrGitRequired = errors.New("sync must run inside an initialized Git repository")

// Workspace identifies the repository root used by sync.
type Workspace struct {
	Root   string
	Source string
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

	source, err := sourceFromOrigin(gitRepository.Origin)
	if err != nil {
		return Workspace{}, fmt.Errorf("derive sync source from Git origin: %w", err)
	}
	return Workspace{Root: root, Source: source}, nil
}

func sourceFromOrigin(origin string) (string, error) {
	origin = strings.TrimSpace(origin)
	if origin == "" {
		return "", errors.New("origin is empty")
	}

	host, repositoryPath, ok := scpOrigin(origin)
	if !ok {
		parsed, err := url.Parse(origin)
		if err != nil || parsed.Scheme == "" || parsed.Host == "" {
			return "", fmt.Errorf("unsupported origin %q", origin)
		}
		host = normalizedHost(parsed)
		repositoryPath = parsed.Path
	}

	repositoryPath = strings.Trim(strings.TrimSuffix(repositoryPath, ".git"), "/")
	if host == "" || repositoryPath == "" {
		return "", fmt.Errorf("unsupported origin %q", origin)
	}
	return "git:" + strings.ToLower(host) + "/" + repositoryPath, nil
}

func scpOrigin(origin string) (string, string, bool) {
	if strings.Contains(origin, "://") {
		return "", "", false
	}
	userAndHost, repositoryPath, ok := strings.Cut(origin, ":")
	if !ok || repositoryPath == "" {
		return "", "", false
	}
	_, host, hasUser := strings.Cut(userAndHost, "@")
	if !hasUser {
		host = userAndHost
	}
	return host, repositoryPath, host != ""
}

func normalizedHost(origin *url.URL) string {
	host := origin.Host
	switch {
	case origin.Scheme == "ssh" && origin.Port() == "22":
		host = origin.Hostname()
	case origin.Scheme == "https" && origin.Port() == "443":
		host = origin.Hostname()
	case origin.Scheme == "http" && origin.Port() == "80":
		host = origin.Hostname()
	}
	return host
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
