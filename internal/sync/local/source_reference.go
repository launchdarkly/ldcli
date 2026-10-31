package local

import (
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"slices"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
	syncreference "github.com/launchdarkly/ldcli/internal/sync/reference"
)

// SourceDefinition is one compiled descriptor-backed resource.
type SourceDefinition struct {
	Variation  *syncdomain.SyncedResource
	Attachment *syncdomain.SyncedAttachment
}

// ID returns the resource identity of a source definition.
func (definition SourceDefinition) ID() syncdomain.ResourceID {
	if definition.Variation != nil {
		return definition.Variation.ID()
	}
	if definition.Attachment != nil {
		return definition.Attachment.ID()
	}
	return syncdomain.ResourceID{}
}

// CompileSource compiles a proposed reference without writing its descriptor.
func CompileSource(
	repositoryRoot string,
	id syncdomain.ResourceID,
	reference Reference,
) (SourceDefinition, error) {
	bundle, err := ReadSourceBundle(repositoryRoot, reference)
	if err != nil {
		return SourceDefinition{}, err
	}
	switch id.Kind {
	case syncdomain.KindVariation:
		variation, err := compileVariationSource(id, reference, bundle)
		return SourceDefinition{Variation: &variation}, err
	case syncdomain.KindTool, syncdomain.KindSkill:
		attachment, err := compileAttachmentSource(id, reference, bundle)
		return SourceDefinition{Attachment: &attachment}, err
	default:
		return SourceDefinition{}, fmt.Errorf("unsupported resource kind %q", id.Kind)
	}
}

// ReadSourceBundle reads a primary source and its static dependencies.
func ReadSourceBundle(repositoryRoot string, reference Reference) (syncreference.SourceBundle, error) {
	content, err := ReadReference(repositoryRoot, reference)
	if err != nil {
		return syncreference.SourceBundle{}, err
	}
	return sourceBundle(reference, content, func(dependency Reference) ([]byte, error) {
		return ReadReference(repositoryRoot, dependency)
	})
}

func sourceBundle(
	reference Reference,
	content []byte,
	read func(Reference) ([]byte, error),
) (syncreference.SourceBundle, error) {
	bundle := syncreference.SourceBundle{
		Primary:      syncreference.Document{Path: reference.File, Data: content},
		Dependencies: map[string]syncreference.Document{},
	}
	paths, err := syncreference.DependencyPaths(reference.Format, bundle.Primary)
	if err != nil {
		return syncreference.SourceBundle{}, err
	}
	if len(paths) > 32 {
		return syncreference.SourceBundle{}, fmt.Errorf("source has more than 32 dependencies")
	}
	total := len(content)
	for _, dependencyPath := range paths {
		dependency := reference
		dependency.File = dependencyPath
		data, err := read(dependency)
		problem, isProblem := syncdomain.ProblemFrom(err)
		if errors.Is(err, fs.ErrNotExist) ||
			isProblem && problem.Code == syncdomain.ErrorLinkedSourceMissing {
			continue
		}
		if err != nil {
			return syncreference.SourceBundle{}, err
		}
		total += len(data)
		if total > 8<<20 {
			return syncreference.SourceBundle{}, fmt.Errorf("source dependencies exceed 8 MiB")
		}
		bundle.Dependencies[dependencyPath] = syncreference.Document{
			Path: dependencyPath, Data: data,
		}
	}
	return bundle, nil
}

func compileReferencedVariation(
	fsys fs.FS,
	descriptorPath string,
	id syncdomain.ResourceID,
	readSource func(Reference) ([]byte, error),
) (syncdomain.SyncedResource, error) {
	reference, err := ReadDescriptor(fsys, descriptorPath, id)
	if err != nil {
		return syncdomain.SyncedResource{}, err
	}
	content, err := readSource(reference)
	if err != nil {
		return syncdomain.SyncedResource{}, err
	}
	bundle, err := sourceBundle(reference, content, readSource)
	if err != nil {
		return syncdomain.SyncedResource{}, err
	}
	return compileVariationSource(id, reference, bundle)
}

func compileVariationSource(
	id syncdomain.ResourceID,
	reference Reference,
	bundle syncreference.SourceBundle,
) (syncdomain.SyncedResource, error) {
	if generatedSource(reference.File, bundle.Primary.Data) {
		return syncdomain.SyncedResource{}, publicationProblem(
			reference.File, "source is generated, vendored, or minified", "Link the authored source file.", nil,
		)
	}
	projection, indexedSelections, err := syncreference.ParseSourceWithSelections(
		bundle,
		id,
		syncdomain.VariationModeAgent,
		reference,
	)
	if err != nil {
		return syncdomain.SyncedResource{}, err
	}
	variationProjection, ok := projection.(syncdomain.VariationProjection)
	if !ok {
		return syncdomain.SyncedResource{}, fmt.Errorf("source returned %T for a variation", projection)
	}
	_, variationKey, _ := id.VariationKeys()
	variation := syncdomain.ApplyVariationProjection(syncdomain.Variation{
		Mode: syncdomain.VariationModeAgent,
		Key:  variationKey,
		Name: syncdomain.DisplayName(variationKey),
	}, variationProjection)
	return syncdomain.SyncedResource{
		Kind: id.Kind, ProjectKey: id.ProjectKey, LookupKey: id.LookupKey,
		Source: &reference, Dependencies: sortedDependencyPaths(bundle),
		IndexedSelections: indexedSelections, Projection: projection, Variation: variation,
	}, nil
}

func compileReferencedAttachment(
	fsys fs.FS,
	descriptorPath string,
	id syncdomain.ResourceID,
	readSource func(Reference) ([]byte, error),
) (syncdomain.SyncedAttachment, error) {
	reference, err := ReadDescriptor(fsys, descriptorPath, id)
	if err != nil {
		return syncdomain.SyncedAttachment{}, err
	}
	content, err := readSource(reference)
	if err != nil {
		return syncdomain.SyncedAttachment{}, err
	}
	bundle, err := sourceBundle(reference, content, readSource)
	if err != nil {
		return syncdomain.SyncedAttachment{}, err
	}
	return compileAttachmentSource(id, reference, bundle)
}

func compileAttachmentSource(
	id syncdomain.ResourceID,
	reference Reference,
	bundle syncreference.SourceBundle,
) (syncdomain.SyncedAttachment, error) {
	if generatedSource(reference.File, bundle.Primary.Data) {
		return syncdomain.SyncedAttachment{}, publicationProblem(
			reference.File, "source is generated, vendored, or minified", "Link the authored source file.", nil,
		)
	}
	projection, indexedSelections, err := syncreference.ParseSourceWithSelections(
		bundle,
		id,
		"",
		reference,
	)
	if err != nil {
		return syncdomain.SyncedAttachment{}, err
	}

	attachment := syncdomain.Attachment{Kind: syncdomain.AttachmentKind(id.Kind)}
	switch id.Kind {
	case syncdomain.KindTool:
		attachment.Tool = &syncdomain.Tool{Key: id.LookupKey}
	case syncdomain.KindSkill:
		attachment.Skill = &syncdomain.Skill{Key: id.LookupKey}
	default:
		return syncdomain.SyncedAttachment{}, fmt.Errorf("unsupported attachment kind %q", id.Kind)
	}
	attachment, err = syncdomain.ApplyAttachmentProjection(attachment, projection)
	if err != nil {
		return syncdomain.SyncedAttachment{}, err
	}
	return syncdomain.SyncedAttachment{
		ProjectKey: id.ProjectKey, Attachment: attachment, Source: &reference,
		Dependencies: sortedDependencyPaths(bundle), IndexedSelections: indexedSelections, Projection: projection,
	}, nil
}

func sortedDependencyPaths(bundle syncreference.SourceBundle) []string {
	paths := slices.Collect(maps.Keys(bundle.Dependencies))
	slices.Sort(paths)
	return paths
}
