package local

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	syncreference "github.com/launchdarkly/ldcli/internal/sync/reference"
)

type stagedVariation struct {
	relativePath       string
	destinationPath    string
	originalContent    []byte
	replacementContent []byte
	mode               os.FileMode
	stagedPath         string
	report             bool
}

// ReplaceVariations transactionally replaces a batch of wrappers and
// referenced files, rolling back committed paths if a later rename fails.
func (store Store) ReplaceVariations(resources []VariationReplacement) ([]string, error) {
	replacements, err := store.prepareReplacements(resources)
	if err != nil {
		return nil, err
	}
	if err := stageReplacements(replacements); err != nil {
		return nil, err
	}
	defer removeStagedVariations(replacements)

	// Staging can take long enough for an editor to change a source file.
	// Recheck every source before replacing any of them.
	if err := verifyReplacementSources(replacements); err != nil {
		return nil, err
	}
	if err := commitReplacements(replacements); err != nil {
		return nil, err
	}
	return replacementPaths(replacements), nil
}

// prepareReplacements renders every destination before any file is changed.
func (store Store) prepareReplacements(resources []VariationReplacement) ([]stagedVariation, error) {
	replacements := make([]stagedVariation, 0, len(resources))
	seenPaths := make(map[string]struct{}, len(resources))

	for _, resource := range resources {
		existing, err := store.inspectVariation(resource.ProjectKey, resource.ConfigKey, resource.Variation.Key)
		if err != nil {
			return nil, err
		}
		if _, duplicate := seenPaths[existing.absolutePath]; duplicate {
			return nil, fmt.Errorf("variation %q was selected more than once", resource.Variation.Key)
		}
		seenPaths[existing.absolutePath] = struct{}{}

		// Upsert and ref describe the local mapping, not server state. Preserve
		// them while replacing only the synchronized variation fields.
		content, err := marshalVariationFile(VariationFile{
			ProjectKey: resource.ProjectKey,
			ConfigKey:  resource.ConfigKey,
			Upsert:     existing.frontMatter.Upsert,
			Ref:        existing.frontMatter.Ref,
			Variation:  resource.Variation,
		})
		if err != nil {
			return nil, err
		}
		replacements = append(replacements, stagedVariation{
			relativePath:       existing.relativePath,
			destinationPath:    existing.absolutePath,
			originalContent:    existing.content,
			replacementContent: content,
			mode:               existing.mode,
			report:             true,
		})

		if existing.frontMatter.Ref == nil {
			continue
		}
		// Add a linked wrapper and its external source to the same staged
		// transaction so a commit failure rolls back both paths.
		reference := *existing.frontMatter.Ref
		referencePath, err := resolveReferencePath(store.repositoryRoot, reference)
		if err != nil {
			return nil, err
		}
		if _, duplicate := seenPaths[referencePath]; duplicate {
			return nil, fmt.Errorf("referenced file %q was selected more than once", reference.File)
		}
		seenPaths[referencePath] = struct{}{}

		referenceContent, err := syncreference.Render(reference.Format, resource.Variation)
		if err != nil {
			return nil, err
		}
		originalContent, err := os.ReadFile(referencePath)
		if err != nil {
			return nil, fmt.Errorf("read referenced file %q: %w", reference.File, err)
		}
		info, err := os.Stat(referencePath)
		if err != nil {
			return nil, fmt.Errorf("stat referenced file %q: %w", reference.File, err)
		}
		replacements = append(replacements, stagedVariation{
			relativePath:       reference.File,
			destinationPath:    referencePath,
			originalContent:    originalContent,
			replacementContent: referenceContent,
			mode:               info.Mode().Perm(),
		})
	}
	return replacements, nil
}

// stageReplacements writes every replacement to its destination directory
// before the first original file is changed.
func stageReplacements(replacements []stagedVariation) error {
	for index := range replacements {
		stagedPath, err := stageReplacement(replacements[index])
		if err != nil {
			removeStagedVariations(replacements)
			return err
		}
		replacements[index].stagedPath = stagedPath
	}
	return nil
}

// verifyReplacementSources detects editor changes made after preflight and
// before commit so sync never overwrites unreviewed content.
func verifyReplacementSources(replacements []stagedVariation) error {
	for _, replacement := range replacements {
		current, err := os.ReadFile(replacement.destinationPath)
		if err != nil {
			return fmt.Errorf("recheck variation %s: %w", replacement.relativePath, err)
		}
		if !bytes.Equal(current, replacement.originalContent) {
			return fmt.Errorf("variation %s changed while syncing", replacement.relativePath)
		}
	}
	return nil
}

// commitReplacements renames staged files into place and restores already
// replaced files if a later rename fails.
func commitReplacements(replacements []stagedVariation) error {
	var replaced []stagedVariation
	for _, replacement := range replacements {
		// Each rename is atomic, but the batch is not. Keep the committed prefix
		// so it can be restored if a later destination fails.
		if err := os.Rename(replacement.stagedPath, replacement.destinationPath); err != nil {
			return errors.Join(fmt.Errorf("replace variation %s: %w", replacement.relativePath, err), rollbackVariations(replaced))
		}
		replaced = append(replaced, replacement)
	}
	return nil
}

// replacementPaths reports wrapper paths while hiding referenced-file details.
func replacementPaths(replacements []stagedVariation) []string {
	var paths []string
	for _, replacement := range replacements {
		if replacement.report {
			paths = append(paths, replacement.relativePath)
		}
	}
	return paths
}

// stageReplacement durably writes one temporary file beside its destination,
// preserving the destination's permission bits.
func stageReplacement(replacement stagedVariation) (string, error) {
	temp, err := os.CreateTemp(filepath.Dir(replacement.destinationPath), "."+filepath.Base(replacement.destinationPath)+".tmp-")
	if err != nil {
		return "", fmt.Errorf("stage variation %s: %w", replacement.relativePath, err)
	}
	tempPath := temp.Name()
	closeWithError := func(err error) (string, error) {
		_ = temp.Close()
		_ = os.Remove(tempPath)
		return "", err
	}

	if err := temp.Chmod(replacement.mode); err != nil {
		return closeWithError(fmt.Errorf("set variation permissions %s: %w", replacement.relativePath, err))
	}
	if _, err := temp.Write(replacement.replacementContent); err != nil {
		return closeWithError(fmt.Errorf("write staged variation %s: %w", replacement.relativePath, err))
	}
	if err := temp.Sync(); err != nil {
		return closeWithError(fmt.Errorf("sync staged variation %s: %w", replacement.relativePath, err))
	}
	if err := temp.Close(); err != nil {
		_ = os.Remove(tempPath)
		return "", fmt.Errorf("close staged variation %s: %w", replacement.relativePath, err)
	}
	return tempPath, nil
}

// removeStagedVariations cleans up temporary files left after success or failure.
func removeStagedVariations(replacements []stagedVariation) {
	for _, replacement := range replacements {
		if replacement.stagedPath != "" {
			_ = os.Remove(replacement.stagedPath)
		}
	}
}

// rollbackVariations restores original bytes in reverse commit order.
func rollbackVariations(replacements []stagedVariation) error {
	var rollbackErr error
	// Reverse order mirrors the commit sequence and minimizes time spent in a
	// partially restored state.
	for index := len(replacements) - 1; index >= 0; index-- {
		replacement := replacements[index]
		tempPath, err := stageReplacement(stagedVariation{
			relativePath: replacement.relativePath, destinationPath: replacement.destinationPath,
			replacementContent: replacement.originalContent, mode: replacement.mode,
		})
		if err == nil {
			err = os.Rename(tempPath, replacement.destinationPath)
			if err != nil {
				_ = os.Remove(tempPath)
			}
		}
		if err != nil {
			rollbackErr = errors.Join(rollbackErr, fmt.Errorf("roll back variation %s: %w", replacement.relativePath, err))
		}
	}
	return rollbackErr
}
