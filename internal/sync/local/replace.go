package local

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
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
	// verifyOnly marks a file that the transaction reads but does not
	// write: the commit makes sure that it still has its original content.
	verifyOnly bool
	// sources holds each repository-relative name that a descriptor gives a
	// source file, when the file is one, with root the repository root. The
	// commit makes sure that every name still resolves to path, so that a
	// link retargeted after the snapshot does not get the edit of another
	// file.
	sources []string
	root    string
}

// ReplaceVariations replaces a batch of variation files in one transaction.
// The transaction also writes each tool and skill file that the variations
// use. ReplaceVariations returns the replaced variation paths.
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
	if err := replaceFiles(files); err != nil {
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

// ReplaceAttachments writes the content of each tool and skill file in one
// transaction, like ReplaceVariations. A missing file is created, and an
// existing file keeps its local fields. It returns the paths that it wrote.
func (store Store) ReplaceAttachments(attachments []syncdomain.SyncedAttachment) ([]string, error) {
	rendered := make([]RenderedFile, 0, len(attachments))
	for _, attachment := range attachments {
		file, err := RenderAttachment(attachment)
		if err != nil {
			return nil, err
		}
		rendered = append(rendered, file)
	}
	files, err := store.prepareAttachmentFiles(rendered)
	if err != nil {
		return nil, err
	}
	if err := replaceFiles(files); err != nil {
		return nil, err
	}

	paths := make([]string, 0, len(files))
	for _, file := range files {
		paths = append(paths, file.displayPath)
	}
	return paths, nil
}

// replaceFiles writes the staged files in one transaction. It writes every
// file to a temporary path, makes sure that no original changed, and then
// renames each file into place.
func replaceFiles(files []stagedFile) error {
	seen := make(map[string]struct{}, len(files))
	for _, file := range files {
		if _, duplicate := seen[file.path]; duplicate {
			return fmt.Errorf("file %q is staged more than once", file.displayPath)
		}
		seen[file.path] = struct{}{}
	}
	if err := stageFiles(files); err != nil {
		return err
	}
	defer removeStagedFiles(files)

	if err := verifyOriginals(files); err != nil {
		return err
	}
	return commitFiles(files)
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
		id := syncdomain.VariationID(replacement.ProjectKey, replacement.ConfigKey, replacement.Variation.Key)
		managedPath, pathErr := resourcePath(id)
		if pathErr != nil {
			return nil, pathErr
		}
		linked, linkedErr := store.hasDescriptor(managedPath)
		if linkedErr != nil {
			return nil, linkedErr
		}
		switch {
		case replacement.CreateIfMissing && errors.Is(err, os.ErrNotExist) && linked:
			// A variation that a descriptor defines has that one definition.
			// A managed file beside it would be a second one.
			return nil, definitionConflict(id, id.Label()+" already has a descriptor")
		case replacement.CreateIfMissing && errors.Is(err, os.ErrNotExist):
			existing, err = missingVariation(replacement)
		case err == nil && linked:
			// A managed file beside a descriptor is a conflict, so the
			// replace does not keep two definitions that drift apart.
			return nil, definitionConflict(id, id.Label()+" has a managed file and a descriptor")
		}
		if err != nil {
			return nil, err
		}
		// Upsert describes the local file, not LaunchDarkly. Keep it.
		content, err := renderVariationFile(VariationFile{
			ProjectKey: replacement.ProjectKey,
			ConfigKey:  replacement.ConfigKey,
			Upsert:     existing.frontMatter.Upsert,
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

		attachmentSources = append(attachmentSources, VariationFile{
			ProjectKey: replacement.ProjectKey, Variation: replacement.Variation,
		})
	}

	rendered, err := renderAttachmentFiles(attachmentSources)
	if err != nil {
		return nil, err
	}
	attachments, err := store.prepareAttachmentFiles(rendered)
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
// file has upsert, so that a later sync can create the variation again.
func missingVariation(replacement VariationReplacement) (existingVariation, error) {
	relativePath, err := variationPath(replacement.ProjectKey, replacement.ConfigKey, replacement.Variation.Key)
	if err != nil {
		return existingVariation{}, err
	}
	return existingVariation{
		relativePath: relativePath,
		mode:         0o644,
		frontMatter:  variationFrontMatter{Upsert: true},
	}, nil
}

// prepareAttachmentFiles stages each rendered tool and skill file that
// changed. A new file is created. An existing file keeps its local fields.
func (store Store) prepareAttachmentFiles(rendered []RenderedFile) ([]stagedFile, error) {
	var staged []stagedFile
	for _, file := range rendered {
		path := store.absolute(file.Path)
		if info, err := os.Lstat(path); err == nil && info.Mode()&os.ModeSymlink != 0 {
			// A leftover link beside a descriptor is not a definition, so
			// the descriptor keeps the one definition. A link alone is
			// rejected below.
			if linked, err := store.hasDescriptor(file.Path); err != nil {
				return nil, err
			} else if linked {
				continue
			}
		}
		if err := rejectSymlinkedPath(store.root, path); err != nil {
			return nil, err
		}

		next := stagedFile{displayPath: file.Path, path: path, replacement: file.Content, mode: 0o644}
		original, err := os.ReadFile(path)
		switch {
		case errors.Is(err, os.ErrNotExist):
			// A tool or skill that a descriptor defines keeps that one
			// definition. A second, managed one would be a conflict.
			if linked, linkedErr := store.hasDescriptor(file.Path); linkedErr != nil {
				return nil, linkedErr
			} else if linked {
				continue
			}
		case err != nil:
			return nil, fmt.Errorf("read attachment %q: %w", file.Path, err)
		default:
			// A managed file beside a descriptor is a conflict, so the
			// replace does not keep two definitions that drift apart.
			if err := store.rejectDescriptorBeside(file.Path); err != nil {
				return nil, err
			}
			info, err := os.Stat(path)
			if err != nil {
				return nil, fmt.Errorf("inspect attachment %q: %w", file.Path, err)
			}
			if next.replacement, err = preserveLocalFields(file.Path, original, file.Content); err != nil {
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

// hasDescriptor reports whether the resource of a managed path has a
// descriptor in the store.
func (store Store) hasDescriptor(managedPath string) (bool, error) {
	id, ok := ParseManagedPath(path.Join(syncdomain.RootDir, managedPath))
	if !ok {
		return false, nil
	}
	referencePath, err := ReferencePath(id)
	if err != nil {
		return false, err
	}
	return store.fileExists(strings.TrimPrefix(referencePath, syncdomain.RootDir+"/"), id.Label()+" descriptor")
}

// rejectDescriptorBeside returns the definition conflict of a managed file
// that has a descriptor beside it.
func (store Store) rejectDescriptorBeside(managedPath string) error {
	linked, err := store.hasDescriptor(managedPath)
	if err != nil || !linked {
		return err
	}
	id, _ := ParseManagedPath(path.Join(syncdomain.RootDir, managedPath))
	return definitionConflict(id, id.Label()+" has a managed file and a descriptor")
}

// stageFiles writes each replacement to a temporary file beside its destination.
func stageFiles(files []stagedFile) error {
	for index := range files {
		if files[index].verifyOnly {
			continue
		}
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
		for _, source := range file.sources {
			resolved, err := resolveSourcePath(file.root, source)
			if err != nil || resolved != file.path {
				return fmt.Errorf("source %s changed while syncing: the name resolves to another file", source)
			}
		}
		if !file.originalExists {
			// A create replaces a leftover link at the path, which is not a
			// definition, so the content behind the link is not an edit.
			if info, err := os.Lstat(file.path); err == nil && info.Mode()&os.ModeSymlink != 0 {
				continue
			}
		}
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
	return commitFilesWithRename(files, os.Rename)
}

func commitFilesWithRename(files []stagedFile, rename func(string, string) error) error {
	for index, file := range files {
		if file.verifyOnly {
			continue
		}
		if err := rename(file.stagedPath, file.path); err != nil {
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
		if file.verifyOnly {
			continue
		}
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
