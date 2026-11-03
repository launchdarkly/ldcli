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
	syncadapter "github.com/launchdarkly/ldcli/internal/sync/adapter"
)

// SourceReplacement is one descriptor-backed resource update.
type SourceReplacement struct {
	ID         syncdomain.ResourceID
	Reference  Reference
	Projection syncdomain.Projection
	// ExpectedFingerprint is the mapped source state from the reviewed plan.
	ExpectedFingerprint string
	// ExpectedProjection supplies reviewed values for unreadable fields.
	ExpectedProjection syncdomain.Projection
	// ExpectedIndexedSelections identifies the indexed items from the reviewed plan.
	ExpectedIndexedSelections []syncdomain.IndexedSelection
	// RelationshipAttachments supplies content for a relationship that a pull
	// adds when the project has no local definition.
	RelationshipAttachments []syncdomain.Attachment
}

func publicationProblem(file, message, suggestion string, cause error) error {
	return syncdomain.WrapProblem(
		syncdomain.NewProblem(syncdomain.ErrorSourceNotWritable, message, suggestion).WithFile(file, 0, 0),
		cause,
	)
}

// ReplaceSources applies checked edits to each selected repository source.
// Every replacement in one call is one unit: one byte snapshot per file, one
// edit pass per file, and one atomic replacement of every changed file.
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

// prepareSourceReplacements plans, applies, and verifies every edit in
// memory, and then stages each changed file.
func (store Store) prepareSourceReplacements(replacements []SourceReplacement) ([]stagedFile, error) {
	before := newSourceSnapshot(store.repositoryRoot)
	edits := make(map[string][]syncadapter.TextEdit)
	for _, replacement := range replacements {
		bundle, err := before.bundle(replacement.Reference)
		if err != nil {
			return nil, err
		}
		planned, err := planSourceEdits(bundle, replacement)
		if err != nil {
			return nil, err
		}
		for _, edit := range planned {
			edits[edit.File] = append(edits[edit.File], edit)
		}
	}

	updated := make(map[string]syncadapter.Document, len(edits))
	for file, fileEdits := range edits {
		document, err := before.document(file)
		if err != nil {
			return nil, err
		}
		data, err := applySourceEdits(file, document.Data, fileEdits)
		if err != nil {
			return nil, err
		}
		updated[file] = syncadapter.Document{Path: file, Data: data}
	}
	after := before.withUpdates(updated)
	if err := verifyTargetProjections(after, replacements); err != nil {
		return nil, err
	}
	if err := store.verifyOtherBindings(before, after, updated, replacements); err != nil {
		return nil, err
	}

	var staged []stagedFile
	for _, file := range slices.Sorted(maps.Keys(updated)) {
		original, _ := before.document(file)
		next, err := store.stageSourceDocument(file, original.Data, updated[file].Data)
		if err != nil {
			return nil, err
		}
		if !bytes.Equal(next.original, next.replacement) {
			staged = append(staged, next)
		}
	}
	for _, replacement := range replacements {
		next, changed, err := store.prepareDescriptorRelationshipReplacement(replacement)
		if err != nil {
			return nil, err
		}
		if changed {
			staged = append(staged, next)
		}
	}
	missingAttachments, err := store.prepareMissingRelationshipAttachments(replacements)
	if err != nil {
		return nil, err
	}
	return append(staged, missingAttachments...), nil
}

// planSourceEdits proves that the source still has the reviewed state, and
// returns the direct edits that give it the desired projection.
func planSourceEdits(bundle syncadapter.SourceBundle, replacement SourceReplacement) ([]syncadapter.TextEdit, error) {
	file := replacement.Reference.File
	current, indexedSelections, err := syncadapter.ParseSourceWithSelections(bundle, replacement.ID, replacement.Reference)
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
		if current, err = alignProjection(current, replacement.ExpectedProjection); err != nil {
			return nil, err
		}
		if current, err = syncdomain.FillReadOnlyFields(current, replacement.ExpectedProjection); err != nil {
			return nil, err
		}
	}
	fingerprint, err := syncdomain.FingerprintProjection(replacement.ID, current)
	if err != nil {
		return nil, err
	}
	if replacement.ExpectedFingerprint != "" && fingerprint != replacement.ExpectedFingerprint {
		return nil, syncdomain.NewProblem(
			syncdomain.ErrorStateChanged, "mapped source fields changed after review", "Run sync again.",
		).WithResource(replacement.ID).WithFile(file, 0, 0)
	}
	result, err := syncadapter.UpdateSource(bundle, replacement.ID, replacement.Reference, replacement.Projection)
	if err != nil {
		return nil, err
	}
	var edits []syncadapter.TextEdit
	for _, field := range slices.Sorted(maps.Keys(result.Fields)) {
		update := result.Fields[field]
		if update.Capability != syncadapter.CapabilityDirect {
			return nil, syncdomain.NewProblem(
				syncdomain.ErrorFieldReadOnly,
				fmt.Sprintf("field %q is read-only: %s", field, update.Reason),
				"Change the field to a directly writable form.",
			).WithResource(replacement.ID).WithFile(file, 0, 0)
		}
		edits = append(edits, update.Edits...)
	}
	return edits, nil
}

// verifyTargetProjections reparses every updated source and proves that it
// has the desired values.
func verifyTargetProjections(after *sourceSnapshot, replacements []SourceReplacement) error {
	for _, replacement := range replacements {
		reference, err := referenceWithProjectedRelationships(replacement.Reference, replacement.Projection)
		if err != nil {
			return err
		}
		bundle, err := after.bundle(reference)
		if err != nil {
			return err
		}
		actual, err := syncadapter.ParseSource(bundle, replacement.ID, reference)
		if err != nil {
			return fmt.Errorf("verify updated %s: %w", replacement.ID.Label(), err)
		}
		if actual, err = alignProjection(actual, replacement.Projection); err != nil {
			return err
		}
		if !syncdomain.ProjectionIncludes(actual, replacement.Projection) {
			return fmt.Errorf("updated projection does not match %s", replacement.ID.Label())
		}
	}
	return nil
}

// verifyOtherBindings proves that no other descriptor that reads an edited
// file sees a different projection after the edits.
func (store Store) verifyOtherBindings(
	before, after *sourceSnapshot,
	updated map[string]syncadapter.Document,
	replacements []SourceReplacement,
) error {
	targets := make(map[syncdomain.ResourceID]struct{}, len(replacements))
	for _, replacement := range replacements {
		targets[replacement.ID] = struct{}{}
	}
	bindings, err := store.sourceBindings()
	if err != nil {
		return err
	}
	for _, binding := range bindings {
		if _, target := targets[binding.id]; target {
			continue
		}
		bundle, err := before.bundle(binding.reference)
		if err != nil || !bundleTouches(bundle, updated) {
			// A source that sync cannot read has no projection to keep.
			continue
		}
		previous, err := syncadapter.ParseSource(bundle, binding.id, binding.reference)
		if err != nil {
			continue
		}
		bundle, err = after.bundle(binding.reference)
		if err != nil {
			return fmt.Errorf("non-target projection %s changed", binding.id.Label())
		}
		next, err := syncadapter.ParseSource(bundle, binding.id, binding.reference)
		if err != nil || !sameProjection(binding.id, previous, next) {
			return fmt.Errorf("non-target projection %s changed", binding.id.Label())
		}
	}
	return nil
}

func bundleTouches(bundle syncadapter.SourceBundle, updated map[string]syncadapter.Document) bool {
	if _, ok := updated[bundle.Primary.Path]; ok {
		return true
	}
	for file := range bundle.Dependencies {
		if _, ok := updated[file]; ok {
			return true
		}
	}
	return false
}

// sourceSnapshot is one read of each repository file that a publication
// touches. Every replacement reads the same bytes of a file, so two
// replacements cannot stage two versions of one file.
type sourceSnapshot struct {
	root      string
	documents map[string]syncadapter.Document
}

func newSourceSnapshot(root string) *sourceSnapshot {
	return &sourceSnapshot{root: root, documents: make(map[string]syncadapter.Document)}
}

func (snapshot *sourceSnapshot) document(file string) (syncadapter.Document, error) {
	if document, ok := snapshot.documents[file]; ok {
		return document, nil
	}
	data, err := ReadSourceFile(snapshot.root, file)
	if err != nil {
		return syncadapter.Document{}, err
	}
	document := syncadapter.Document{Path: file, Data: data}
	snapshot.documents[file] = document
	return document, nil
}

func (snapshot *sourceSnapshot) read(file string) ([]byte, error) {
	document, err := snapshot.document(file)
	return document.Data, err
}

// bundle reads a primary source and its static dependencies from the snapshot.
func (snapshot *sourceSnapshot) bundle(reference Reference) (syncadapter.SourceBundle, error) {
	content, err := snapshot.read(reference.File)
	if err != nil {
		return syncadapter.SourceBundle{}, err
	}
	return sourceBundle(reference, content, snapshot.read)
}

// withUpdates returns a snapshot that serves the updated bytes of each edited
// file and the original bytes of every other file.
func (snapshot *sourceSnapshot) withUpdates(updated map[string]syncadapter.Document) *sourceSnapshot {
	documents := maps.Clone(snapshot.documents)
	maps.Copy(documents, updated)
	return &sourceSnapshot{root: snapshot.root, documents: documents}
}

// sourceBinding is one descriptor and the resource that it defines.
type sourceBinding struct {
	id        syncdomain.ResourceID
	reference Reference
}

// alignProjection converts a parsed variation projection to the mode of the
// projection that it is compared with.
func alignProjection(parsed, like syncdomain.Projection) (syncdomain.Projection, error) {
	variation, ok := parsed.(syncdomain.VariationProjection)
	expected, likeOK := like.(syncdomain.VariationProjection)
	if !ok || !likeOK {
		return parsed, nil
	}
	return variation.ForMode(expected.Mode)
}

// sourceBindings reads every descriptor in the repository.
func (store Store) sourceBindings() ([]sourceBinding, error) {
	files, err := SourceFiles(store.repositoryRoot)
	if err != nil {
		return nil, err
	}
	root, err := filepath.EvalSymlinks(store.repositoryRoot)
	if err != nil {
		return nil, fmt.Errorf("resolve repository root: %w", err)
	}
	fsys := workspaceFS{root: root}
	var bindings []sourceBinding
	for _, file := range files {
		id, ok := ParseReferencePath(file)
		if !ok {
			continue
		}
		reference, err := ReadDescriptor(fsys, file, id)
		if err != nil {
			return nil, ParseError{Path: file, Err: err}
		}
		bindings = append(bindings, sourceBinding{id: id, reference: reference})
	}
	return bindings, nil
}

// stageSourceDocument stages one checked replacement of a repository file.
func (store Store) stageSourceDocument(file string, original, updated []byte) (stagedFile, error) {
	root, err := filepath.EvalSymlinks(store.repositoryRoot)
	if err != nil {
		return stagedFile{}, fmt.Errorf("resolve repository root: %w", err)
	}
	unresolved := filepath.Join(root, filepath.FromSlash(file))
	if err := rejectSymlinkedPath(root, unresolved); err != nil {
		return stagedFile{}, publicationProblem(file, "source path is not safe to replace", "Use a regular repository file.", err)
	}
	path, err := resolveSourcePath(store.repositoryRoot, file)
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

// applySourceEdits applies the edits of one file from the highest offset to
// the lowest. It rejects an edit whose expected bytes changed, and edits that
// overlap.
func applySourceEdits(file string, original []byte, edits []syncadapter.TextEdit) ([]byte, error) {
	for _, edit := range edits {
		if edit.File != file || edit.Start < 0 || edit.End < edit.Start || edit.End > len(original) {
			return nil, fmt.Errorf("invalid edit for %q", file)
		}
		if !bytes.Equal(original[edit.Start:edit.End], edit.Expected) {
			return nil, syncdomain.NewProblem(
				syncdomain.ErrorStateChanged, "source bytes changed after review", "Run sync again.",
			).WithFile(file, 0, 0)
		}
	}
	slices.SortFunc(edits, func(left, right syncadapter.TextEdit) int {
		if left.Start != right.Start {
			return right.Start - left.Start
		}
		return right.End - left.End
	})
	for index := 1; index < len(edits); index++ {
		if edits[index-1].Start < edits[index].End {
			return nil, syncdomain.NewProblem(
				syncdomain.ErrorOverlappingEdits, "source updates overlap", "Use mappings that own separate source values.",
			).WithFile(file, 0, 0)
		}
	}
	updated := bytes.Clone(original)
	for _, edit := range edits {
		updated = slices.Concat(updated[:edit.Start], edit.Replacement, updated[edit.End:])
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
