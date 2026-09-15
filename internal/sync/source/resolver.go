package source

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"

	"github.com/launchdarkly/ldcli/internal/config"
	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
	"github.com/launchdarkly/ldcli/internal/sync/repository"
)

type Workspace struct {
	Root   string
	Source syncdomain.Source
}

type Resolver struct {
	configFile           string
	findGitSource        func(string) (repository.GitRepository, bool, error)
	ensureInstallationID func(string) (string, error)
}

func NewResolver(configFile string) Resolver {
	return Resolver{
		configFile:           configFile,
		findGitSource:        repository.FindGitSource,
		ensureInstallationID: config.EnsureInstallationID,
	}
}

func (resolver Resolver) Resolve(dir string) (Workspace, error) {
	gitRepository, found, err := resolver.findGitSource(dir)
	if err != nil {
		return Workspace{}, err
	}
	if found {
		root, err := canonicalPath(gitRepository.Root)
		if err != nil {
			return Workspace{}, err
		}

		return Workspace{
			Root:   root,
			Source: gitRepository.Source,
		}, nil
	}

	root, err := localWorkspaceRoot(dir)
	if err != nil {
		return Workspace{}, err
	}

	installationID, err := resolver.ensureInstallationID(resolver.configFile)
	if err != nil {
		return Workspace{}, err
	}

	source, err := syncdomain.NewSource(
		syncdomain.SourceTypeLocal,
		localSourceIdentifier(installationID, root),
	)
	if err != nil {
		return Workspace{}, err
	}

	return Workspace{Root: root, Source: source}, nil
}

func localWorkspaceRoot(dir string) (string, error) {
	root, err := canonicalPath(dir)
	if err != nil {
		return "", err
	}

	for current := root; ; current = filepath.Dir(current) {
		info, err := os.Stat(filepath.Join(current, syncdomain.RootDir))
		switch {
		case err == nil && info.IsDir():
			return current, nil
		case err != nil && !os.IsNotExist(err):
			return "", fmt.Errorf("inspect workspace root: %w", err)
		}

		parent := filepath.Dir(current)
		if parent == current {
			return root, nil
		}
	}
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

func localSourceIdentifier(installationID, root string) string {
	sum := sha256.Sum256([]byte(installationID + "\x00" + root))

	return "sha256." + hex.EncodeToString(sum[:])
}
