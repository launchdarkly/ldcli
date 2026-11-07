package local

import (
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"slices"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
	syncadapter "github.com/launchdarkly/ldcli/internal/sync/adapter"
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
func CompileSource(repositoryRoot string, id syncdomain.ResourceID, reference Reference) (SourceDefinition, error) {
	bundle, err := ReadSourceBundle(repositoryRoot, reference)
	if err != nil {
		return SourceDefinition{}, err
	}
	return compileSource(id, reference, bundle)
}

// ReadSourceBundle reads a primary source and its static dependencies.
func ReadSourceBundle(repositoryRoot string, reference Reference) (syncadapter.SourceBundle, error) {
	return newSourceSnapshot(repositoryRoot).bundle(reference)
}

// sourceBundle builds the bundle of one source from its content and the
// dependencies that its format reports. read returns the bytes of one
// repository file. A dependency that does not exist is left out, and does not
// count toward the limit: a format reports several candidate paths for one
// import, and only the file that exists is a dependency.
func sourceBundle(
	reference Reference,
	content []byte,
	read func(file string) ([]byte, error),
) (syncadapter.SourceBundle, error) {
	bundle := syncadapter.SourceBundle{
		Primary:      syncadapter.Document{Path: reference.File, Data: content},
		Dependencies: map[string]syncadapter.Document{},
	}
	paths, err := syncadapter.DependencyPaths(reference.Format, bundle.Primary)
	if err != nil {
		return syncadapter.SourceBundle{}, err
	}
	total := len(content)
	if total > 8<<20 {
		return syncadapter.SourceBundle{}, fmt.Errorf("source dependencies exceed 8 MiB")
	}
	for _, dependencyPath := range paths {
		data, err := read(dependencyPath)
		problem, isProblem := syncdomain.ProblemFrom(err)
		if errors.Is(err, fs.ErrNotExist) ||
			isProblem && problem.Code == syncdomain.ErrorLinkedSourceMissing {
			continue
		}
		if err != nil {
			return syncadapter.SourceBundle{}, err
		}
		total += len(data)
		if total > 8<<20 {
			return syncadapter.SourceBundle{}, fmt.Errorf("source dependencies exceed 8 MiB")
		}
		bundle.Dependencies[dependencyPath] = syncadapter.Document{
			Path: dependencyPath, Data: data,
		}
		if len(bundle.Dependencies) > 32 {
			return syncadapter.SourceBundle{}, fmt.Errorf("source has more than 32 dependencies")
		}
	}
	return bundle, nil
}

// readSourceFileFromFS reads one repository source file from a file system.
// Tests use it with an in-memory file system.
func readSourceFileFromFS(fsys fs.FS, file string) ([]byte, error) {
	if err := validateSourcePath(file); err != nil {
		return nil, err
	}
	data, err := fs.ReadFile(fsys, file)
	if err != nil {
		return nil, fmt.Errorf("read referenced file %q: %w", file, err)
	}
	return data, nil
}

// compileDescriptor compiles the resource that one .ref.yaml file defines.
// readAttachment, when set, hydrates the tools and skills of a variation.
func compileDescriptor(
	fsys fs.FS,
	descriptorPath string,
	id syncdomain.ResourceID,
	readSource func(file string) ([]byte, error),
	readAttachment func(syncdomain.AttachmentKind, string) (syncdomain.Attachment, error),
) (SourceDefinition, error) {
	reference, err := ReadDescriptor(fsys, descriptorPath, id)
	if err != nil {
		return SourceDefinition{}, err
	}
	content, err := readSource(reference.File)
	if err != nil {
		return SourceDefinition{}, err
	}
	bundle, err := sourceBundle(reference, content, readSource)
	if err != nil {
		return SourceDefinition{}, err
	}
	definition, err := compileSource(id, reference, bundle)
	if err != nil {
		return SourceDefinition{}, err
	}
	if definition.Variation != nil && readAttachment != nil && (reference.Tools != nil || reference.Skills != nil) {
		if err := definition.Variation.Variation.HydrateAttachments(readAttachment); err != nil {
			return SourceDefinition{}, err
		}
	}
	return definition, nil
}

// compileSource reads the mapped projection of a source and builds the local
// resource of the kind from it. The descriptor path gives the identity, so
// the key and the display name come from the id.
func compileSource(id syncdomain.ResourceID, reference Reference, bundle syncadapter.SourceBundle) (SourceDefinition, error) {
	if generatedSource(reference.File, bundle.Primary.Data) {
		return SourceDefinition{}, publicationProblem(
			reference.File, "source is generated, vendored, or minified", "Link the authored source file.", nil,
		)
	}
	projection, indexedSelections, err := syncadapter.ParseSourceWithSelections(bundle, id, reference)
	if err != nil {
		return SourceDefinition{}, err
	}
	dependencies := sortedDependencyPaths(bundle)
	switch id.Kind {
	case syncdomain.KindVariation:
		variationProjection, ok := projection.(syncdomain.VariationProjection)
		if !ok {
			return SourceDefinition{}, fmt.Errorf("source returned %T for a variation", projection)
		}
		_, variationKey, _ := id.VariationKeys()
		variation := syncdomain.ApplyVariationProjection(syncdomain.Variation{
			Key: variationKey, Name: syncdomain.DisplayName(variationKey),
		}, variationProjection)
		// The descriptor validates its relationships as an agent, because the
		// mode comes from the source. A messages source is a completion
		// variation, which cannot have skills.
		if err := syncdomain.ValidateAttachmentRefs(variation.Mode, variation.Tools, variation.Skills); err != nil {
			return SourceDefinition{}, syncdomain.NewProblem(
				syncdomain.ErrorInvalidReference,
				fmt.Sprintf("descriptor relationships do not fit the %s source: %s", variation.Mode, err),
				"Remove the skills from the descriptor, or map a prompt instead of messages.",
			).WithFile(reference.File, 0, 0)
		}
		return SourceDefinition{Variation: &syncdomain.SyncedResource{
			Kind: id.Kind, ProjectKey: id.ProjectKey, LookupKey: id.LookupKey,
			Source: &reference, Dependencies: dependencies,
			IndexedSelections: indexedSelections, Projection: projection, Variation: variation,
		}}, nil
	case syncdomain.KindTool, syncdomain.KindSkill:
		attachment := syncdomain.Attachment{Kind: syncdomain.AttachmentKind(id.Kind)}
		if id.Kind == syncdomain.KindTool {
			attachment.Tool = &syncdomain.Tool{Key: id.LookupKey}
		} else {
			attachment.Skill = &syncdomain.Skill{Key: id.LookupKey}
		}
		if attachment, err = syncdomain.ApplyAttachmentProjection(attachment, projection); err != nil {
			return SourceDefinition{}, err
		}
		attachment.Projection = projection
		return SourceDefinition{Attachment: &syncdomain.SyncedAttachment{
			ProjectKey: id.ProjectKey, Attachment: attachment, Source: &reference,
			Dependencies: dependencies, IndexedSelections: indexedSelections, Projection: projection,
		}}, nil
	default:
		return SourceDefinition{}, fmt.Errorf("unsupported resource kind %q", id.Kind)
	}
}

func sortedDependencyPaths(bundle syncadapter.SourceBundle) []string {
	paths := slices.Collect(maps.Keys(bundle.Dependencies))
	slices.Sort(paths)
	return paths
}
