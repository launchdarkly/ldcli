package local

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Creation records the result of a create. VariationPaths are the variation
// files to report to the user. CreatedPaths are every file that the create
// wrote, so that RollbackCreation can remove them.
type Creation struct {
	VariationPaths []string
	CreatedPaths   []string
}

// Render renders the files for a batch of variations without writing them.
// Tool and skill files come first, then the variation files that use them.
func (store Store) Render(files []VariationFile) ([]RenderedFile, error) {
	attachments, err := renderAttachmentFiles(files)
	if err != nil {
		return nil, err
	}

	rendered := attachments
	seen := make(map[string]struct{}, len(files))
	for _, file := range files {
		relativePath, err := variationPath(file.ProjectKey, file.ConfigKey, file.Variation.Key)
		if err != nil {
			return nil, err
		}
		if _, duplicate := seen[relativePath]; duplicate {
			return nil, fmt.Errorf("variation %q was selected more than once", file.Variation.Key)
		}
		seen[relativePath] = struct{}{}

		content, err := renderVariationFile(file)
		if err != nil {
			return nil, err
		}
		rendered = append(rendered, RenderedFile{Path: relativePath, Content: content})
	}
	return rendered, nil
}

// Bootstrap creates the managed directory with a first batch of variations.
// It builds the directory beside the final path and renames it into place,
// so that a failure leaves no partial directory.
func (store Store) Bootstrap(files []VariationFile) (Creation, error) {
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

	staged := Store{repositoryRoot: store.repositoryRoot, root: stagingDirectory}
	creation, err := staged.Add(files)
	if err != nil {
		return Creation{}, err
	}
	if err := os.Rename(stagingDirectory, store.root); err != nil {
		return Creation{}, fmt.Errorf("finish bootstrap: %w", err)
	}
	return creation, nil
}

// Add creates a batch of variation files and the tool and skill files that
// they use. Add never overwrites a file. An existing variation file is an
// error, and an existing tool or skill file stays as it is, so that a local
// edit is not lost. If one file fails, Add removes the files that it created.
func (store Store) Add(files []VariationFile) (Creation, error) {
	rendered, err := store.Render(files)
	if err != nil {
		return Creation{}, err
	}

	var creation Creation
	for _, file := range rendered {
		created, err := createFileIfAbsent(store.root, store.absolute(file.Path), file.Content)
		if err != nil {
			return Creation{}, errors.Join(err, store.RollbackCreation(creation))
		}
		if created {
			creation.CreatedPaths = append(creation.CreatedPaths, file.Path)
		}
		if strings.HasSuffix(file.Path, variationFileSuffix) {
			creation.VariationPaths = append(creation.VariationPaths, file.Path)
		}
	}
	return creation, nil
}

// RollbackCreation removes the files that one create wrote, in reverse order.
func (store Store) RollbackCreation(creation Creation) error {
	var failures []error
	for index := len(creation.CreatedPaths) - 1; index >= 0; index-- {
		relativePath := creation.CreatedPaths[index]
		if err := os.Remove(store.absolute(relativePath)); err != nil && !errors.Is(err, os.ErrNotExist) {
			failures = append(failures, fmt.Errorf("remove created resource %s: %w", relativePath, err))
		}
	}
	if err := store.RemoveEmptyDirectories(); err != nil {
		failures = append(failures, err)
	}
	return errors.Join(failures...)
}

// createFileIfAbsent creates one file and reports whether it did. An existing
// variation file is an error. An existing tool or skill file is kept.
func createFileIfAbsent(root, path string, data []byte) (bool, error) {
	_, err := os.Stat(path)
	switch {
	case err == nil:
		if err := rejectSymlinkedPath(root, path); err != nil {
			return false, err
		}
		if strings.HasSuffix(path, variationFileSuffix) {
			return false, fmt.Errorf("%w: %s", ErrVariationExists, path)
		}
		return false, nil
	case !errors.Is(err, os.ErrNotExist):
		return false, fmt.Errorf("inspect resource file %s: %w", filepath.Base(path), err)
	}
	if err := createFile(root, path, data); err != nil {
		return false, err
	}
	return true, nil
}

// createFile writes data to a temporary file beside path, and then hard-links
// the temporary file to path. The link fails if path exists, so createFile
// never overwrites a file that another process created.
func createFile(root, path string, data []byte) error {
	if err := rejectSymlinkedPath(root, path); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create resource directory: %w", err)
	}

	tempPath, err := writeTempFile(path, data, 0o644)
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(tempPath) }()

	if err := os.Link(tempPath, path); err != nil {
		if errors.Is(err, os.ErrExist) {
			return fmt.Errorf("%w: %s", ErrVariationExists, path)
		}
		return fmt.Errorf("create resource file %s: %w", filepath.Base(path), err)
	}
	return nil
}

// writeTempFile writes data to a new temporary file beside destination and
// flushes it to disk. The caller must rename or remove the temporary file.
func writeTempFile(destination string, data []byte, mode os.FileMode) (string, error) {
	name := filepath.Base(destination)
	temp, err := os.CreateTemp(filepath.Dir(destination), "."+name+".tmp-")
	if err != nil {
		return "", fmt.Errorf("stage file %s: %w", name, err)
	}
	fail := func(err error) (string, error) {
		_ = temp.Close()
		_ = os.Remove(temp.Name())
		return "", err
	}

	if err := temp.Chmod(mode); err != nil {
		return fail(fmt.Errorf("set permissions of staged file %s: %w", name, err))
	}
	if _, err := temp.Write(data); err != nil {
		return fail(fmt.Errorf("write staged file %s: %w", name, err))
	}
	if err := temp.Sync(); err != nil {
		return fail(fmt.Errorf("sync staged file %s: %w", name, err))
	}
	if err := temp.Close(); err != nil {
		_ = os.Remove(temp.Name())
		return "", fmt.Errorf("close staged file %s: %w", name, err)
	}
	return temp.Name(), nil
}
