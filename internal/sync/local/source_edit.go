package local

import (
	"bytes"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
	syncreference "github.com/launchdarkly/ldcli/internal/sync/reference"
)

// SourceReplacement is one descriptor-backed resource update.
type SourceReplacement struct {
	ID         syncdomain.ResourceID
	Mode       syncdomain.VariationMode
	Reference  Reference
	Projection syncdomain.Projection
	// ExpectedFingerprint is the mapped source state from the reviewed plan.
	ExpectedFingerprint string
	// ExpectedProjection supplies reviewed values for unreadable fields.
	ExpectedProjection syncdomain.Projection
	// ExpectedIndexedSelections identifies the indexed items from the reviewed plan.
	ExpectedIndexedSelections []syncdomain.IndexedSelection
}

func publicationProblem(file, message, suggestion string, cause error) error {
	return syncdomain.WrapProblem(
		syncdomain.NewProblem(
			syncdomain.ErrorPublicationUnsupported,
			message,
			suggestion,
		).WithFile(file, 0, 0),
		cause,
	)
}

// ReplaceSources applies checked edits to each selected repository source.
func (store Store) ReplaceSources(replacements []SourceReplacement) ([]string, error) {
	staged, err := store.prepareSourceReplacements(replacements)
	if err != nil {
		return nil, err
	}
	if err := replaceFiles(staged); err != nil {
		return nil, err
	}
	paths := make([]string, len(staged))
	for index := range staged {
		paths[index] = staged[index].displayPath
	}
	return paths, nil
}

// CheckSources verifies grouped source edits without writing files.
func (store Store) CheckSources(replacements []SourceReplacement) error {
	_, err := store.prepareSourceReplacements(replacements)
	return err
}

func (store Store) prepareSourceReplacements(replacements []SourceReplacement) ([]stagedFile, error) {
	groups := make(map[string][]SourceReplacement)
	for _, replacement := range replacements {
		groups[replacement.Reference.File] = append(groups[replacement.Reference.File], replacement)
	}
	bindings, err := store.sourceBindings()
	if err != nil {
		return nil, err
	}

	var staged []stagedFile
	files := slices.Collect(maps.Keys(groups))
	slices.Sort(files)
	for _, file := range files {
		next, err := store.prepareSourceFiles(file, groups[file], bindings[file])
		if err != nil {
			return nil, err
		}
		staged = append(staged, next...)
	}
	return staged, nil
}

type sourceBinding struct {
	id        syncdomain.ResourceID
	mode      syncdomain.VariationMode
	reference Reference
}

func (store Store) sourceBindings() (map[string][]sourceBinding, error) {
	files, err := SourceFiles(store.repositoryRoot)
	if err != nil {
		return nil, err
	}
	root, err := filepath.EvalSymlinks(store.repositoryRoot)
	if err != nil {
		return nil, fmt.Errorf("resolve repository root: %w", err)
	}
	fsys := workspaceFS{root: root}
	result := make(map[string][]sourceBinding)
	for _, file := range files {
		id, ok := ParseReferencePath(file)
		if !ok {
			continue
		}
		reference, err := ReadDescriptor(fsys, file, id)
		if err != nil {
			return nil, ParseError{Path: file, Err: err}
		}
		mode := syncdomain.VariationMode("")
		if id.Kind == syncdomain.KindVariation {
			mode = syncdomain.VariationModeAgent
		}
		result[reference.File] = append(result[reference.File], sourceBinding{id: id, mode: mode, reference: reference})
	}
	return result, nil
}

func (store Store) prepareSourceFiles(
	file string,
	replacements []SourceReplacement,
	bindings []sourceBinding,
) ([]stagedFile, error) {
	bundle, err := ReadSourceBundle(store.repositoryRoot, replacements[0].Reference)
	if err != nil {
		return nil, err
	}

	var edits []syncreference.TextEdit
	targets := make(map[syncdomain.ResourceID]struct{}, len(replacements))
	for _, replacement := range replacements {
		targets[replacement.ID] = struct{}{}
		current, indexedSelections, err := syncreference.ParseSourceWithSelections(
			bundle,
			replacement.ID,
			replacement.Mode,
			replacement.Reference,
		)
		if err != nil {
			return nil, publicationProblem(file, "source cannot be updated safely", "Change the source to a supported static form.", err)
		}
		if replacement.ExpectedIndexedSelections != nil &&
			!slices.Equal(replacement.ExpectedIndexedSelections, indexedSelections) {
			return nil, syncdomain.NewProblem(
				syncdomain.ErrorIndexedSelectionChanged,
				"an indexed source selection changed after review",
				"Run sync again. Use a keyed selector when the item has a stable key.",
			).WithResource(replacement.ID).WithFile(file, 0, 0)
		}
		if replacement.ExpectedProjection != nil {
			current, err = syncdomain.FillReadOnlyFields(current, replacement.ExpectedProjection)
			if err != nil {
				return nil, err
			}
		}
		fingerprint, err := syncdomain.FingerprintProjection(replacement.ID, current)
		if err != nil {
			return nil, err
		}
		if replacement.ExpectedFingerprint != "" && fingerprint != replacement.ExpectedFingerprint {
			return nil, syncdomain.NewProblem(
				syncdomain.ErrorStateChanged,
				"mapped source fields changed after review",
				"Run sync again.",
			).WithResource(replacement.ID).WithFile(file, 0, 0)
		}
		result, err := syncreference.UpdateSource(
			bundle,
			replacement.ID,
			replacement.Mode,
			replacement.Reference,
			replacement.Projection,
		)
		if err != nil {
			return nil, err
		}
		for field, update := range result.Fields {
			if update.Capability != syncreference.CapabilityDirect {
				if update.Capability == syncreference.CapabilityReadOnly {
					return nil, syncdomain.NewProblem(
						syncdomain.ErrorFieldReadOnly,
						fmt.Sprintf("field %q is read-only: %s", field, update.Reason),
						"Change the field to a directly writable form.",
					).WithResource(replacement.ID).WithFile(file, 0, 0)
				}
				return nil, publicationProblem(
					file,
					fmt.Sprintf("field %q of %s is %s: %s", field, replacement.ID.Label(), update.Capability, update.Reason),
					"Change the field to a directly writable form.",
					nil,
				)
			}
			edits = append(edits, update.Edits...)
		}
	}
	editsByFile := map[string][]syncreference.TextEdit{}
	for _, edit := range edits {
		editsByFile[edit.File] = append(editsByFile[edit.File], edit)
	}
	updatedBundle := bundle
	updatedBundle.Dependencies = maps.Clone(bundle.Dependencies)
	updatedDocuments := map[string]syncreference.Document{}
	for editFile, fileEdits := range editsByFile {
		document, ok := sourceBundleDocument(bundle, editFile)
		if !ok {
			return nil, fmt.Errorf("edit targets unknown dependency %q", editFile)
		}
		updated, err := applySourceEdits(editFile, document.Data, fileEdits)
		if err != nil {
			return nil, err
		}
		document.Data = updated
		updatedDocuments[editFile] = document
		if editFile == bundle.Primary.Path {
			updatedBundle.Primary = document
		} else {
			updatedBundle.Dependencies[editFile] = document
		}
	}

	for _, replacement := range replacements {
		actual, err := syncreference.ParseSource(
			updatedBundle,
			replacement.ID,
			replacement.Mode,
			replacement.Reference,
		)
		if err != nil {
			return nil, fmt.Errorf("verify updated %s: %w", replacement.ID.Label(), err)
		}
		if !syncdomain.ProjectionIncludes(actual, replacement.Projection) {
			return nil, fmt.Errorf("updated projection does not match %s", replacement.ID.Label())
		}
	}
	for _, binding := range bindings {
		if _, changed := targets[binding.id]; changed {
			continue
		}
		before, err := syncreference.ParseSource(
			bundle,
			binding.id, binding.mode, binding.reference,
		)
		if err != nil {
			return nil, err
		}
		after, err := syncreference.ParseSource(
			updatedBundle,
			binding.id, binding.mode, binding.reference,
		)
		if err != nil || !sameProjection(binding.id, before, after) {
			return nil, fmt.Errorf("non-target projection %s changed", binding.id.Label())
		}
	}
	var staged []stagedFile
	for changedFile, updated := range updatedDocuments {
		original, _ := sourceBundleDocument(bundle, changedFile)
		next, err := store.stageSourceDocument(changedFile, original.Data, updated.Data)
		if err != nil {
			return nil, err
		}
		if !bytes.Equal(next.original, next.replacement) {
			staged = append(staged, next)
		}
	}
	return staged, nil
}

func sourceBundleDocument(bundle syncreference.SourceBundle, file string) (syncreference.Document, bool) {
	if file == bundle.Primary.Path {
		return bundle.Primary, true
	}
	document, ok := bundle.Dependencies[file]
	return document, ok
}

func (store Store) stageSourceDocument(file string, original, updated []byte) (stagedFile, error) {
	root, err := filepath.EvalSymlinks(store.repositoryRoot)
	if err != nil {
		return stagedFile{}, fmt.Errorf("resolve repository root: %w", err)
	}
	reference := Reference{File: file, Format: "dependency"}
	unresolved := filepath.Join(root, filepath.FromSlash(file))
	if err := rejectSymlinkedPath(root, unresolved); err != nil {
		return stagedFile{}, publicationProblem(file, "source path is not safe to replace", "Use a regular repository file.", err)
	}
	path, err := resolveReferencePath(store.repositoryRoot, reference)
	if err != nil {
		return stagedFile{}, err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return stagedFile{}, fmt.Errorf("inspect source file %q: %w", file, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return stagedFile{}, publicationProblem(file, "source is a symbolic link", "Use a regular repository file.", nil)
	}
	if stat, ok := info.Sys().(*syscall.Stat_t); ok && stat.Nlink > 1 {
		return stagedFile{}, publicationProblem(file, "source has multiple hard links", "Use a file with one link.", nil)
	}
	if generatedSource(file, original) {
		return stagedFile{}, publicationProblem(file, "source is generated, vendored, or minified", "Link the authored source file.", nil)
	}
	return stagedFile{
		displayPath: file, path: path, original: original, originalExists: true,
		replacement: updated, mode: info.Mode().Perm(),
	}, nil
}

func applySourceEdits(file string, original []byte, edits []syncreference.TextEdit) ([]byte, error) {
	for _, edit := range edits {
		if edit.File != file || edit.Start < 0 || edit.End < edit.Start || edit.End > len(original) {
			return nil, fmt.Errorf("invalid edit for %q", file)
		}
		if !bytes.Equal(original[edit.Start:edit.End], edit.Expected) {
			return nil, syncdomain.NewProblem(
				syncdomain.ErrorStateChanged,
				"source bytes changed after review",
				"Run sync again.",
			).WithFile(file, 0, 0)
		}
	}
	slices.SortFunc(edits, func(left, right syncreference.TextEdit) int {
		if left.Start != right.Start {
			return right.Start - left.Start
		}
		return right.End - left.End
	})
	for index := 1; index < len(edits); index++ {
		if edits[index-1].Start < edits[index].End {
			return nil, syncdomain.NewProblem(
				syncdomain.ErrorOverlappingEdits,
				"source updates overlap",
				"Use mappings that own separate source values.",
			).WithFile(file, 0, 0)
		}
	}
	updated := bytes.Clone(original)
	for _, edit := range edits {
		next := make([]byte, 0, len(updated)-edit.End+edit.Start+len(edit.Replacement))
		next = append(next, updated[:edit.Start]...)
		next = append(next, edit.Replacement...)
		next = append(next, updated[edit.End:]...)
		updated = next
	}
	return updated, nil
}

func sameProjection(id syncdomain.ResourceID, left, right syncdomain.Projection) bool {
	leftFingerprint, leftErr := syncdomain.FingerprintProjection(id, left)
	rightFingerprint, rightErr := syncdomain.FingerprintProjection(id, right)
	return leftErr == nil && rightErr == nil && leftFingerprint == rightFingerprint
}

func generatedSource(file string, data []byte) bool {
	path := filepath.ToSlash(file)
	if strings.Contains("/"+path+"/", "/vendor/") || strings.HasSuffix(path, ".min.js") {
		return true
	}
	prefix := strings.ToLower(string(data))
	if len(prefix) > 512 {
		prefix = prefix[:512]
	}
	return strings.Contains(prefix, "code generated") && strings.Contains(prefix, "do not edit")
}
