package local

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
)

// ErrVariationExists reports that a create would overwrite a local wrapper.
var ErrVariationExists = errors.New("variation already exists locally")

// VariationFile contains everything needed to write one local variation wrapper.
type VariationFile struct {
	ProjectKey string
	ConfigKey  string
	Upsert     bool
	Ref        *Reference
	Variation  syncdomain.Variation
}

// VariationReplacement identifies a wrapper and the state to write.
type VariationReplacement struct {
	ProjectKey      string
	ConfigKey       string
	CreateIfMissing bool
	Variation       syncdomain.Variation
}

// VariationDeletion identifies an existing wrapper to remove.
type VariationDeletion struct {
	ProjectKey   string
	ConfigKey    string
	VariationKey string
}

// RenderedVariationFile is a repository-relative wrapper ready to write.
type RenderedVariationFile struct {
	Path    string
	Content []byte
}

// Creation records the variation paths returned to users and every file that
// can be safely removed if a later manifest write fails.
type Creation struct {
	VariationPaths []string
	CreatedPaths   []string
}

// Store reads and writes resources under a repository's .launchdarkly directory.
type Store struct {
	repositoryRoot string
	root           string
}

// NewStore creates a local resource store rooted at a Git repository.
func NewStore(repositoryRoot string) Store {
	return Store{repositoryRoot: repositoryRoot, root: filepath.Join(repositoryRoot, syncdomain.RootDir)}
}

// Exists reports whether the repository has a .launchdarkly directory.
func (store Store) Exists() (bool, error) {
	info, err := os.Stat(store.root)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("inspect %s: %w", store.root, err)
	}
	if !info.IsDir() {
		return false, fmt.Errorf("%s exists but is not a directory", store.root)
	}
	return true, nil
}

// ProjectKeys returns locally managed project keys in deterministic order.
func (store Store) ProjectKeys() ([]string, error) {
	entries, err := os.ReadDir(store.root)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", store.root, err)
	}

	var keys []string
	for _, entry := range entries {
		if entry.IsDir() {
			keys = append(keys, entry.Name())
		}
	}
	slices.Sort(keys)
	return keys, nil
}

// VariationExists reports whether one local variation wrapper exists.
func (store Store) VariationExists(projectKey, configKey, variationKey string) (bool, error) {
	path, err := store.variationPath(projectKey, configKey, variationKey)
	if err != nil {
		return false, err
	}

	_, err = os.Stat(path)
	switch {
	case err == nil:
		return true, nil
	case errors.Is(err, os.ErrNotExist):
		return false, nil
	default:
		return false, fmt.Errorf("inspect variation %s: %w", variationKey, err)
	}
}

// Bootstrap atomically creates a new .launchdarkly directory.
func (store Store) Bootstrap(resources []VariationFile) ([]string, error) {
	creation, err := store.BootstrapResources(resources)
	return creation.VariationPaths, err
}

// BootstrapResources creates a workspace and returns its rollback record.
func (store Store) BootstrapResources(resources []VariationFile) (Creation, error) {
	if _, err := os.Stat(store.root); err == nil {
		return Creation{}, fmt.Errorf("%s already exists", store.root)
	} else if !errors.Is(err, os.ErrNotExist) {
		return Creation{}, fmt.Errorf("inspect %s: %w", store.root, err)
	}

	stagingDirectory, err := os.MkdirTemp(filepath.Dir(store.root), ".launchdarkly.tmp-")
	if err != nil {
		return Creation{}, fmt.Errorf("create bootstrap staging directory: %w", err)
	}
	defer func() { _ = os.RemoveAll(stagingDirectory) }()

	// Build the entire workspace in a sibling directory. The final rename is a
	// single commit point because source and destination share a filesystem.
	stagedStore := Store{repositoryRoot: store.repositoryRoot, root: stagingDirectory}
	creation, err := stagedStore.createVariations(resources)
	if err != nil {
		return Creation{}, err
	}
	if err := os.Rename(stagingDirectory, store.root); err != nil {
		return Creation{}, fmt.Errorf("finish bootstrap: %w", err)
	}
	return creation, nil
}

// Add creates a batch of variation wrappers without overwriting existing files.
func (store Store) Add(resources []VariationFile) ([]string, error) {
	creation, err := store.AddResources(resources)
	return creation.VariationPaths, err
}

// AddResources creates resources and returns the exact files published by the call.
func (store Store) AddResources(resources []VariationFile) (Creation, error) {
	return store.createVariations(resources)
}

// RollbackCreation removes only files published by the corresponding create.
func (store Store) RollbackCreation(creation Creation) error {
	var failures []error
	for index := len(creation.CreatedPaths) - 1; index >= 0; index-- {
		filePath := filepath.Join(store.root, filepath.FromSlash(creation.CreatedPaths[index]))
		if err := os.Remove(filePath); err != nil && !errors.Is(err, os.ErrNotExist) {
			failures = append(failures, fmt.Errorf("remove created resource %s: %w", creation.CreatedPaths[index], err))
		}
	}
	if err := store.RemoveEmptyDirectories(); err != nil {
		failures = append(failures, err)
	}
	return errors.Join(failures...)
}

type existingVariation struct {
	relativePath string
	absolutePath string
	content      []byte
	mode         os.FileMode
	exists       bool
	frontMatter  variationFrontMatter
}

// inspectVariation reads the wrapper metadata needed by replace and delete
// transactions.
func (store Store) inspectVariation(projectKey, configKey, variationKey string) (existingVariation, error) {
	absolutePath, err := store.variationPath(projectKey, configKey, variationKey)
	if err != nil {
		return existingVariation{}, err
	}

	content, err := os.ReadFile(absolutePath)
	if err != nil {
		return existingVariation{}, fmt.Errorf("read variation %s: %w", variationKey, err)
	}
	info, err := os.Stat(absolutePath)
	if err != nil {
		return existingVariation{}, fmt.Errorf("stat variation %s: %w", variationKey, err)
	}
	if !info.Mode().IsRegular() {
		return existingVariation{}, fmt.Errorf("variation %s is not a regular file", variationKey)
	}

	var frontMatter variationFrontMatter
	if _, err := parseYAMLFrontMatter(content, &frontMatter); err != nil {
		return existingVariation{}, fmt.Errorf("parse variation %s: %w", variationKey, err)
	}
	if err := validateVariation(filepath.Base(absolutePath), frontMatter); err != nil {
		return existingVariation{}, fmt.Errorf("validate variation %s: %w", variationKey, err)
	}

	return existingVariation{
		relativePath: filepath.ToSlash(strings.TrimPrefix(absolutePath, store.root+string(filepath.Separator))),
		absolutePath: absolutePath,
		content:      content,
		mode:         info.Mode().Perm(),
		exists:       true,
		frontMatter:  frontMatter,
	}, nil
}

// RemoveEmptyDirectories removes empty resource directories left by deletes.
func (store Store) RemoveEmptyDirectories() error {
	var directories []string
	err := filepath.WalkDir(store.root, func(path string, entry os.DirEntry, walkErr error) error {
		if errors.Is(walkErr, os.ErrNotExist) {
			return nil
		}
		if walkErr != nil {
			return walkErr
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

	// Remove deepest-first so parent directories become empty as their children
	// disappear. ENOTEMPTY is expected when a directory still owns resources.
	for index := len(directories) - 1; index >= 0; index-- {
		err := os.Remove(directories[index])
		if err == nil || errors.Is(err, os.ErrNotExist) || errors.Is(err, syscall.ENOTEMPTY) {
			continue
		}
		return fmt.Errorf("remove empty sync directory %s: %w", directories[index], err)
	}
	return nil
}

// variationPath validates every identity component before constructing a path
// beneath the managed workspace.
func (store Store) variationPath(projectKey, configKey, variationKey string) (string, error) {
	segments := []struct {
		name  string
		value string
	}{
		{name: "project key", value: projectKey},
		{name: "config key", value: configKey},
		{name: "variation key", value: variationKey},
	}
	for _, segment := range segments {
		if err := validatePathSegment(segment.value); err != nil {
			return "", fmt.Errorf("invalid %s %q: %w", segment.name, segment.value, err)
		}
	}
	return filepath.Join(store.root, projectKey, configsDir, configKey, variationKey+variationFileSuffix), nil
}

// validatePathSegment rejects traversal, separators, and null bytes before a
// resource identity reaches filesystem APIs.
func validatePathSegment(value string) error {
	if value == "" {
		return errors.New("must not be empty")
	}
	if value == "." || value == ".." || strings.ContainsAny(value, `/\`) {
		return errors.New("must be a single path segment")
	}
	if strings.IndexByte(value, 0) >= 0 {
		return errors.New("must not contain a null byte")
	}
	return nil
}
