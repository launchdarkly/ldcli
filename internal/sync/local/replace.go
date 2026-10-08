package local

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	syncreference "github.com/launchdarkly/ldcli/internal/sync/reference"
)

// stagedFile is one file in a replace transaction. The original content lets
// the transaction detect a concurrent edit and roll back a failure.
type stagedFile struct {
	displayPath     string
	path            string
	original        []byte
	originalExists  bool
	replacement     []byte
	mode            os.FileMode
	stagedPath      string
	isVariationFile bool
}

// ReplaceVariations replaces a batch of variation files in one transaction.
// The transaction also writes each linked file and each tool and skill file
// that the variations use. ReplaceVariations returns the replaced variation
// paths.
//
// The transaction has three steps. First, it writes every new file to a
// temporary path. Then it makes sure that no original changed during the
// sync. Last, it renames each temporary file into place, and restores the
// originals if a rename fails.
func (store Store) ReplaceVariations(replacements []VariationReplacement) ([]string, error) {
	files, err := store.prepareReplacements(replacements)
	if err != nil {
		return nil, err
	}
	if err := stageFiles(files); err != nil {
		return nil, err
	}
	defer removeStagedFiles(files)

	if err := verifyOriginals(files); err != nil {
		return nil, err
	}
	if err := commitFiles(files); err != nil {
		return nil, err
	}

	var paths []string
	for _, file := range files {
		if file.isVariationFile {
			paths = append(paths, file.displayPath)
		}
	}
	return paths, nil
}

// ReplaceFileAtomically replaces the file at path only if its content is
// still original. displayPath names the file in errors.
func ReplaceFileAtomically(path, displayPath string, original, replacement []byte, mode os.FileMode) error {
	file := stagedFile{
		displayPath: displayPath, path: path, original: original, originalExists: true,
		replacement: replacement, mode: mode,
	}
	files := []stagedFile{file}
	if err := stageFiles(files); err != nil {
		return err
	}
	defer removeStagedFiles(files)

	if err := verifyOriginals(files); err != nil {
		return err
	}
	if err := os.Rename(files[0].stagedPath, path); err != nil {
		return fmt.Errorf("replace file %s: %w", displayPath, err)
	}
	return nil
}

// prepareReplacements renders every file in the transaction before the
// transaction changes a file.
func (store Store) prepareReplacements(replacements []VariationReplacement) ([]stagedFile, error) {
	var files []stagedFile
	seen := make(map[string]struct{})
	add := func(file stagedFile) error {
		if _, duplicate := seen[file.path]; duplicate {
			return fmt.Errorf("file %q was selected more than once", file.displayPath)
		}
		seen[file.path] = struct{}{}
		files = append(files, file)
		return nil
	}

	attachmentSources := make([]VariationFile, 0, len(replacements))
	for _, replacement := range replacements {
		existing, err := store.readVariation(replacement.ProjectKey, replacement.ConfigKey, replacement.Variation.Key)
		if replacement.CreateIfMissing && errors.Is(err, os.ErrNotExist) {
			existing, err = missingVariation(replacement)
		}
		if err != nil {
			return nil, err
		}
		// Upsert and ref describe the local file, not LaunchDarkly. Keep them.
		content, err := renderVariationFile(VariationFile{
			ProjectKey: replacement.ProjectKey,
			ConfigKey:  replacement.ConfigKey,
			Upsert:     existing.frontMatter.Upsert,
			Ref:        existing.frontMatter.Ref,
			Variation:  replacement.Variation,
		})
		if err != nil {
			return nil, err
		}
		if err := add(stagedFile{
			displayPath: existing.relativePath, path: store.absolute(existing.relativePath),
			original: existing.content, originalExists: existing.exists, replacement: content, mode: existing.mode,
			isVariationFile: true,
		}); err != nil {
			return nil, err
		}

		if ref := existing.frontMatter.Ref; ref != nil {
			linked, err := store.prepareLinkedFile(*ref, replacement)
			if err != nil {
				return nil, err
			}
			if err := add(linked); err != nil {
				return nil, err
			}
		}
		attachmentSources = append(attachmentSources, VariationFile{
			ProjectKey: replacement.ProjectKey, Variation: replacement.Variation,
		})
	}

	attachments, err := store.prepareAttachmentFiles(attachmentSources)
	if err != nil {
		return nil, err
	}
	for _, attachment := range attachments {
		if err := add(attachment); err != nil {
			return nil, err
		}
	}
	return files, nil
}

// missingVariation describes a variation file that does not exist yet. A new
// file has upsert, so that a later sync can create the variation again, and
// it keeps the link of the variation that it restores.
func missingVariation(replacement VariationReplacement) (existingVariation, error) {
	relativePath, err := variationPath(replacement.ProjectKey, replacement.ConfigKey, replacement.Variation.Key)
	if err != nil {
		return existingVariation{}, err
	}
	return existingVariation{
		relativePath: relativePath,
		mode:         0o644,
		frontMatter:  variationFrontMatter{Upsert: true, Ref: replacement.Ref},
	}, nil
}

// prepareLinkedFile renders the new content of the external file that a
// linked variation uses.
func (store Store) prepareLinkedFile(ref Reference, replacement VariationReplacement) (stagedFile, error) {
	path, err := resolveReferencePath(store.repositoryRoot, ref)
	if err != nil {
		return stagedFile{}, err
	}
	content, err := syncreference.Render(ref.Format, replacement.Variation)
	if err != nil {
		return stagedFile{}, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return stagedFile{}, fmt.Errorf("inspect referenced file %q: %w", ref.File, err)
	}
	original, err := os.ReadFile(path)
	if err != nil {
		return stagedFile{}, fmt.Errorf("read referenced file %q: %w", ref.File, err)
	}
	return stagedFile{
		displayPath: ref.File, path: path, original: original, originalExists: true,
		replacement: content, mode: info.Mode().Perm(),
	}, nil
}

// prepareAttachmentFiles renders each tool and skill file that changed. A new
// file is created. A tool file keeps its local upsert flag.
func (store Store) prepareAttachmentFiles(files []VariationFile) ([]stagedFile, error) {
	rendered, err := renderAttachmentFiles(files)
	if err != nil {
		return nil, err
	}

	var staged []stagedFile
	for _, file := range rendered {
		path := store.absolute(file.Path)
		if err := rejectSymlinkedPath(store.root, path); err != nil {
			return nil, err
		}

		next := stagedFile{displayPath: file.Path, path: path, replacement: file.Content, mode: 0o644}
		original, err := os.ReadFile(path)
		switch {
		case errors.Is(err, os.ErrNotExist):
		case err != nil:
			return nil, fmt.Errorf("read attachment %q: %w", file.Path, err)
		default:
			info, err := os.Stat(path)
			if err != nil {
				return nil, fmt.Errorf("inspect attachment %q: %w", file.Path, err)
			}
			if next.replacement, err = preserveToolUpsert(file.Path, original, file.Content); err != nil {
				return nil, err
			}
			next.original, next.originalExists, next.mode = original, true, info.Mode().Perm()
		}
		if next.originalExists && bytes.Equal(next.original, next.replacement) {
			continue
		}
		staged = append(staged, next)
	}
	return staged, nil
}

// stageFiles writes each replacement to a temporary file beside its destination.
func stageFiles(files []stagedFile) error {
	for index := range files {
		if err := os.MkdirAll(filepath.Dir(files[index].path), 0o755); err != nil {
			removeStagedFiles(files)
			return fmt.Errorf("create resource directory %s: %w", files[index].displayPath, err)
		}
		stagedPath, err := writeTempFile(files[index].path, files[index].replacement, files[index].mode)
		if err != nil {
			removeStagedFiles(files)
			return err
		}
		files[index].stagedPath = stagedPath
	}
	return nil
}

// verifyOriginals makes sure that no file changed after the transaction read
// it. An editor can save a file while sync waits for review.
func verifyOriginals(files []stagedFile) error {
	for _, file := range files {
		current, err := os.ReadFile(file.path)
		if !file.originalExists && errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return fmt.Errorf("recheck variation %s: %w", file.displayPath, err)
		}
		if !bytes.Equal(current, file.original) {
			return fmt.Errorf("variation %s changed while syncing", file.displayPath)
		}
	}
	return nil
}

// commitFiles renames each staged file into place. Each rename is atomic, but
// the batch is not, so a failure restores the files that commitFiles replaced.
func commitFiles(files []stagedFile) error {
	for index, file := range files {
		if err := os.Rename(file.stagedPath, file.path); err != nil {
			return errors.Join(fmt.Errorf("replace variation %s: %w", file.displayPath, err), restoreFiles(files[:index]))
		}
	}
	return nil
}

// restoreFiles puts back the original content of each file, in reverse order.
func restoreFiles(files []stagedFile) error {
	var failures []error
	for index := len(files) - 1; index >= 0; index-- {
		file := files[index]
		if !file.originalExists {
			if err := os.Remove(file.path); err != nil && !errors.Is(err, os.ErrNotExist) {
				failures = append(failures, fmt.Errorf("roll back variation %s: %w", file.displayPath, err))
			}
			continue
		}
		tempPath, err := writeTempFile(file.path, file.original, file.mode)
		if err == nil {
			if err = os.Rename(tempPath, file.path); err != nil {
				_ = os.Remove(tempPath)
			}
		}
		if err != nil {
			failures = append(failures, fmt.Errorf("roll back variation %s: %w", file.displayPath, err))
		}
	}
	return errors.Join(failures...)
}

// removeStagedFiles removes the temporary files that remain after a commit or a failure.
func removeStagedFiles(files []stagedFile) {
	for _, file := range files {
		if file.stagedPath != "" {
			_ = os.Remove(file.stagedPath)
		}
	}
}
