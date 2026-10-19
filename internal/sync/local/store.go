// Package local reads and writes the sync files in the .launchdarkly
// directory of a Git repository. Every write is atomic for each file and
// rolls back the batch when a later file fails.
package local

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
)

// ErrVariationExists reports that a create would overwrite a variation file.
var ErrVariationExists = errors.New("variation already exists locally")

// VariationFile is one variation file to create.
type VariationFile struct {
	ProjectKey string
	ConfigKey  string
	Upsert     bool
	Ref        *Reference
	Variation  syncdomain.Variation
}

// VariationReplacement is new content for a variation file. If
// CreateIfMissing is true and the file does not exist, the replace creates it
// with Ref as its link. An existing file keeps its own link.
type VariationReplacement struct {
	ProjectKey      string
	ConfigKey       string
	CreateIfMissing bool
	Ref             *Reference
	Variation       syncdomain.Variation
}

// VariationDeletion identifies an existing variation file to delete.
type VariationDeletion struct {
	ProjectKey   string
	ConfigKey    string
	VariationKey string
}

// RenderedFile is the content of one file and its path relative to the
// managed directory.
type RenderedFile struct {
	Path    string
	Content []byte
}

// Store reads and writes the files in the managed directory of one repository.
type Store struct {
	repositoryRoot string
	root           string
}

// NewStore creates a store for the repository at repositoryRoot.
func NewStore(repositoryRoot string) Store {
	return Store{repositoryRoot: repositoryRoot, root: filepath.Join(repositoryRoot, syncdomain.RootDir)}
}

// Exists reports whether the managed directory exists.
func (store Store) Exists() (bool, error) {
	info, err := os.Stat(store.root)
	switch {
	case errors.Is(err, os.ErrNotExist):
		return false, nil
	case err != nil:
		return false, fmt.Errorf("inspect %s: %w", store.root, err)
	case !info.IsDir():
		return false, fmt.Errorf("%s exists but is not a directory", store.root)
	default:
		return true, nil
	}
}

// VariationExists reports whether a variation file exists.
func (store Store) VariationExists(projectKey, configKey, variationKey string) (bool, error) {
	relativePath, err := variationPath(projectKey, configKey, variationKey)
	if err != nil {
		return false, err
	}
	_, err = os.Stat(store.absolute(relativePath))
	switch {
	case err == nil:
		return true, nil
	case errors.Is(err, os.ErrNotExist):
		return false, nil
	default:
		return false, fmt.Errorf("inspect variation %s: %w", variationKey, err)
	}
}

// RemoveEmptyDirectories removes each empty directory in the managed tree,
// including the managed directory itself.
func (store Store) RemoveEmptyDirectories() error {
	var directories []string
	err := filepath.WalkDir(store.root, func(path string, entry os.DirEntry, err error) error {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		if entry.IsDir() {
			directories = append(directories, path)
		}
		return nil
	})
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect empty sync directories: %w", err)
	}

	// Remove the deepest directories first, so that a parent is empty when
	// its turn comes. A directory that still has files returns ENOTEMPTY.
	for index := len(directories) - 1; index >= 0; index-- {
		err := os.Remove(directories[index])
		if err != nil && !errors.Is(err, os.ErrNotExist) && !errors.Is(err, syscall.ENOTEMPTY) {
			return fmt.Errorf("remove empty sync directory %s: %w", directories[index], err)
		}
	}
	return nil
}

// existingVariation is a variation file on disk and its decoded front matter.
// When exists is false, the file is new and has the default upsert flag.
type existingVariation struct {
	relativePath string
	content      []byte
	mode         os.FileMode
	exists       bool
	frontMatter  variationFrontMatter
}

func (store Store) readVariation(projectKey, configKey, variationKey string) (existingVariation, error) {
	relativePath, err := variationPath(projectKey, configKey, variationKey)
	if err != nil {
		return existingVariation{}, err
	}
	absolutePath := store.absolute(relativePath)

	info, err := os.Stat(absolutePath)
	if err != nil {
		return existingVariation{}, fmt.Errorf("inspect variation %s: %w", variationKey, err)
	}
	if !info.Mode().IsRegular() {
		return existingVariation{}, fmt.Errorf("variation %s is not a regular file", variationKey)
	}
	content, err := os.ReadFile(absolutePath)
	if err != nil {
		return existingVariation{}, fmt.Errorf("read variation %s: %w", variationKey, err)
	}
	frontMatter, _, err := parseVariationFrontMatter(relativePath, content)
	if err != nil {
		return existingVariation{}, fmt.Errorf("parse variation %s: %w", variationKey, err)
	}
	return existingVariation{
		relativePath: relativePath,
		content:      content,
		mode:         info.Mode().Perm(),
		exists:       true,
		frontMatter:  frontMatter,
	}, nil
}

// absolute converts a path relative to the managed directory to an absolute path.
func (store Store) absolute(relativePath string) string {
	return filepath.Join(store.root, filepath.FromSlash(relativePath))
}

// rejectSymlinkedPath makes sure that no component of target is a symbolic
// link, so that a write cannot leave the managed directory.
func rejectSymlinkedPath(root, target string) error {
	if !isWithin(root, target) {
		return fmt.Errorf("managed path %s is outside %s", target, root)
	}
	relative, _ := filepath.Rel(root, target)

	current := root
	for _, component := range append([]string{""}, strings.Split(relative, string(filepath.Separator))...) {
		current = filepath.Join(current, component)
		info, err := os.Lstat(current)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("inspect managed path %s: %w", current, err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("symbolic links are not supported: %s", current)
		}
	}
	return nil
}
