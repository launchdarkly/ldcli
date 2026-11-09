package local

import (
	"bytes"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
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

// ReplaceSources applies checked edits to each selected repository source.
// Every replacement in one call is one unit: one byte snapshot per file, one
// edit pass per file, and one atomic replacement of every changed file.
func (store Store) ReplaceSources(replacements []SourceReplacement) ([]string, error) {
	// The descriptors that the mapping check read are verified again at the
	// commit, so a descriptor that another process retargets after the
	// check does not reach a write of the file that it named before.
	guards := make(map[string]stagedFile)
	for _, replacement := range replacements {
		guard, err := store.checkDescriptorMapping(replacement)
		if err != nil {
			return nil, err
		}
		guards[guard.path] = guard
	}
	staged, err := store.prepareSourceReplacements(replacements)
	if err != nil {
		return nil, err
	}
	paths := make([]string, len(staged))
	for index := range staged {
		paths[index] = staged[index].displayPath
		delete(guards, staged[index].path)
	}
	for _, path := range slices.Sorted(maps.Keys(guards)) {
		staged = append(staged, guards[path])
	}
	if err := replaceFiles(staged); err != nil {
		return nil, err
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
	// Two descriptors can name one file two ways, for example through a
	// link, so every map here is keyed by the resolved path of the file.
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
			key, err := before.key(edit.File)
			if err != nil {
				return nil, err
			}
			edits[key] = append(edits[key], edit)
		}
	}

	updated := make(map[string]syncadapter.Document, len(edits))
	for key, fileEdits := range edits {
		document := before.documents[key]
		data, err := applySourceEdits(document.Path, document.Data, fileEdits)
		if err != nil {
			return nil, err
		}
		updated[key] = syncadapter.Document{Path: document.Path, Data: data}
	}
	after := before.withUpdates(updated)
	if err := verifyTargetProjections(after, replacements); err != nil {
		return nil, err
	}
	if err := store.verifyOtherBindings(before, after, updated, replacements); err != nil {
		return nil, err
	}

	var staged []stagedFile
	for _, key := range slices.Sorted(maps.Keys(updated)) {
		original := before.documents[key]
		next, err := store.stageSourceDocument(before.names[key], original.Data, updated[key].Data)
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

// checkDescriptorMapping proves that the descriptor still names the reviewed
// file, format, selection, and map, because the edits follow the reviewed
// reference. The tools and skills lists have their own check. Only a write
// runs it: CheckSources runs before a link writes its descriptor. It returns
// the descriptor as a verify-only file, so that the commit makes sure that
// the checked content is still there.
func (store Store) checkDescriptorMapping(replacement SourceReplacement) (stagedFile, error) {
	descriptorPath, err := ReferencePath(replacement.ID)
	if err != nil {
		return stagedFile{}, err
	}
	changed := syncdomain.NewProblem(
		syncdomain.ErrorStateChanged, "the source descriptor changed after review", "Run sync again.",
	).WithResource(replacement.ID).WithFile(descriptorPath, 0, 0)
	target := filepath.Join(store.repositoryRoot, filepath.FromSlash(descriptorPath))
	data, err := os.ReadFile(target)
	if err != nil {
		return stagedFile{}, syncdomain.WrapProblem(changed, err)
	}
	current, err := decodeDescriptor(data, replacement.ID)
	if err != nil {
		return stagedFile{}, syncdomain.WrapProblem(changed, err)
	}
	mappingChanged, err := descriptorMappingChanged(replacement, current)
	if err != nil {
		return stagedFile{}, err
	}
	if mappingChanged {
		return stagedFile{}, changed
	}
	return stagedFile{
		displayPath: descriptorPath, path: target, original: data, originalExists: true, verifyOnly: true,
	}, nil
}

// descriptorMappingChanged reports whether a descriptor read from disk names
// another file, format, selection, or map than the reviewed reference.
func descriptorMappingChanged(replacement SourceReplacement, current Reference) (bool, error) {
	// A reviewed reference can carry the defaults of its format implicitly,
	// and the descriptor on disk always reads back normalized.
	reviewed, err := syncadapter.NormalizeReference(replacement.ID, replacement.Reference)
	if err != nil {
		return false, err
	}
	reviewedDigest, err := syncdomain.ReferenceDigest(reviewed)
	if err != nil {
		return false, err
	}
	digest, err := syncdomain.ReferenceDigest(current)
	if err != nil {
		return false, err
	}
	return digest != reviewedDigest, nil
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
		// A descriptor keeps its tools and skills sorted, and LaunchDarkly
		// lists them in its own order, so the lists compare as sets.
		if !syncdomain.ProjectionIncludes(withSortedRelationships(actual), withSortedRelationships(replacement.Projection)) {
			return fmt.Errorf("updated projection does not match %s", replacement.ID.Label())
		}
	}
	return nil
}

// withSortedRelationships returns a variation projection whose tools and
// skills lists are sorted. Another projection is returned as it is.
func withSortedRelationships(projection syncdomain.Projection) syncdomain.Projection {
	variation, ok := projection.(syncdomain.VariationProjection)
	if !ok {
		return projection
	}
	if variation.Tools != nil {
		tools := slices.Sorted(slices.Values(*variation.Tools))
		variation.Tools = &tools
	}
	if variation.Skills != nil {
		skills := slices.Sorted(slices.Values(*variation.Skills))
		variation.Skills = &skills
	}
	return variation
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
		if err != nil || !before.bundleTouches(bundle, updated) {
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

// bundleTouches reports whether a bundle reads an updated file. updated is
// keyed by resolved path.
func (snapshot *sourceSnapshot) bundleTouches(bundle syncadapter.SourceBundle, updated map[string]syncadapter.Document) bool {
	files := append([]string{bundle.Primary.Path}, slices.Collect(maps.Keys(bundle.Dependencies))...)
	for _, file := range files {
		if key, err := snapshot.key(file); err == nil {
			if _, ok := updated[key]; ok {
				return true
			}
		}
	}
	return false
}

// sourceSnapshot is one read of each repository file that a publication
// touches. Every replacement reads the same bytes of a file, so two
// replacements cannot stage two versions of one file. The documents are
// keyed by resolved path, so two names of one file share one read.
type sourceSnapshot struct {
	root      string
	documents map[string]syncadapter.Document
	// names holds each name that a descriptor gave a file, by resolved
	// path, so the commit can check every name of a shared file.
	names map[string][]string
	// keys holds the resolved path of each name at the time of its first
	// read, so that a link retargeted after the snapshot cannot move the
	// edits of one file onto another before the commit checks the names.
	keys map[string]string
}

func newSourceSnapshot(root string) *sourceSnapshot {
	return &sourceSnapshot{
		root:      root,
		documents: make(map[string]syncadapter.Document),
		names:     make(map[string][]string),
		keys:      make(map[string]string),
	}
}

// key returns the resolved path of a repository file, which identifies the
// file however a descriptor names it. A name resolves once per snapshot.
func (snapshot *sourceSnapshot) key(file string) (string, error) {
	if key, ok := snapshot.keys[file]; ok {
		return key, nil
	}
	key, err := resolveSourcePath(snapshot.root, file)
	if err != nil {
		return "", err
	}
	snapshot.keys[file] = key
	return key, nil
}

func (snapshot *sourceSnapshot) document(file string) (syncadapter.Document, error) {
	key, err := snapshot.key(file)
	if err != nil {
		return syncadapter.Document{}, err
	}
	if document, ok := snapshot.documents[key]; ok {
		if !slices.Contains(snapshot.names[key], file) {
			snapshot.names[key] = append(snapshot.names[key], file)
		}
		return syncadapter.Document{Path: file, Data: document.Data}, nil
	}
	data, err := os.ReadFile(key)
	if err != nil {
		return syncadapter.Document{}, fmt.Errorf("read referenced file %q: %w", file, err)
	}
	generated, err := generatedTarget(snapshot.root, file, key, data)
	if err != nil {
		return syncadapter.Document{}, err
	}
	document := syncadapter.Document{Path: file, Data: data, ReadOnly: generated}
	snapshot.documents[key] = document
	snapshot.names[key] = []string{file}
	return document, nil
}

// read returns the bytes of a file that sync writes, so a generated file is
// a problem here and read-only as a dependency.
func (snapshot *sourceSnapshot) read(file string) ([]byte, error) {
	document, err := snapshot.document(file)
	if err != nil {
		return nil, err
	}
	if document.ReadOnly {
		return nil, generatedProblem(file)
	}
	return document.Data, nil
}

// bundle reads a primary source and its static dependencies from the snapshot.
func (snapshot *sourceSnapshot) bundle(reference Reference) (syncadapter.SourceBundle, error) {
	content, err := snapshot.read(reference.File)
	if err != nil {
		return syncadapter.SourceBundle{}, err
	}
	return sourceBundle(reference, content, snapshot.document)
}

// withUpdates returns a snapshot that serves the updated bytes of each edited
// file and the original bytes of every other file.
func (snapshot *sourceSnapshot) withUpdates(updated map[string]syncadapter.Document) *sourceSnapshot {
	documents := maps.Clone(snapshot.documents)
	maps.Copy(documents, updated)
	return &sourceSnapshot{root: snapshot.root, documents: documents, names: snapshot.names, keys: snapshot.keys}
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
			// A descriptor that sync cannot read has no projection to keep,
			// as a source that it cannot read has none. A compile reports
			// the descriptor when it matters.
			continue
		}
		bindings = append(bindings, sourceBinding{id: id, reference: reference})
	}
	return bindings, nil
}

// stageSourceDocument stages one checked replacement of a repository file,
// which the descriptors name by each of names. It writes the regular file
// that the source path resolves to, which is the file that compile read, so a
// link inside the repository works on both paths.
func (store Store) stageSourceDocument(names []string, original, updated []byte) (stagedFile, error) {
	file := names[0]
	path, err := resolveSourcePath(store.repositoryRoot, file)
	if err != nil {
		return stagedFile{}, err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return stagedFile{}, fmt.Errorf("inspect source file %q: %w", file, err)
	}
	if stat, ok := info.Sys().(*syscall.Stat_t); ok && stat.Nlink > 1 {
		return stagedFile{}, publicationProblem(file, "source has multiple hard links", "Use a file with one link.", nil)
	}
	// A link can point into a vendored or minified tree, so the resolved
	// path counts as well as the name in the descriptor.
	root, err := filepath.EvalSymlinks(store.repositoryRoot)
	if err != nil {
		return stagedFile{}, fmt.Errorf("resolve repository root: %w", err)
	}
	resolved, err := filepath.Rel(root, path)
	if err != nil {
		return stagedFile{}, fmt.Errorf("resolve source file %q: %w", file, err)
	}
	if generatedSource(file, original) || generatedSource(filepath.ToSlash(resolved), original) {
		return stagedFile{}, publicationProblem(file, "source is generated, vendored, or minified", "Link the authored source file.", nil)
	}
	return stagedFile{
		displayPath: file, path: path, original: original, originalExists: true,
		replacement: updated, mode: info.Mode().Perm(), sources: names, root: store.repositoryRoot,
	}, nil
}

// applySourceEdits applies the edits of one file from the highest offset to
// the lowest. It rejects an edit whose expected bytes changed, and edits that
// overlap.
func applySourceEdits(file string, original []byte, edits []syncadapter.TextEdit) ([]byte, error) {
	for _, edit := range edits {
		if edit.Start < 0 || edit.End < edit.Start || edit.End > len(original) {
			return nil, fmt.Errorf("invalid edit for %q", file)
		}
		if !bytes.Equal(original[edit.Start:edit.End], edit.Expected) {
			return nil, syncdomain.NewProblem(
				syncdomain.ErrorStateChanged, "source bytes changed after review", "Run sync again.",
			).WithFile(file, 0, 0)
		}
	}
	slices.SortStableFunc(edits, func(left, right syncadapter.TextEdit) int {
		if left.Start != right.Start {
			return right.Start - left.Start
		}
		return right.End - left.End
	})
	// Two edits that start at one offset, such as two inserts, have no
	// order, so they overlap as a shared range does.
	for index := 1; index < len(edits); index++ {
		if edits[index-1].Start < edits[index].End || edits[index-1].Start == edits[index].Start {
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
