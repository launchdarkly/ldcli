package local

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
	syncadapter "github.com/launchdarkly/ldcli/internal/sync/adapter"
	"gopkg.in/yaml.v3"
)

// Reference identifies one repository source and its mapped fields.
type Reference = syncdomain.Reference

func definitionConflict(id syncdomain.ResourceID, message string) error {
	return syncdomain.NewProblem(
		syncdomain.ErrorDefinitionConflict,
		message,
		"Keep one managed file or one .ref.yaml descriptor for this resource.",
	).WithResource(id)
}

// NewSourceReference validates an external source and its mapping.
func NewSourceReference(
	repositoryRoot, workingDirectory, file, format, selectPath string,
	fieldMap map[string]string,
) (Reference, error) {
	if file == "" {
		return Reference{}, errors.New("linked file path is required")
	}
	if !filepath.IsAbs(file) {
		file = filepath.Join(workingDirectory, file)
	}
	// Compare resolved paths. A path that looks like it is in the repository
	// can go out of it through a symbolic link.
	target, err := filepath.EvalSymlinks(file)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Reference{}, syncdomain.NewProblem(
				syncdomain.ErrorLinkedSourceMissing,
				fmt.Sprintf("source file %q does not exist", file),
				"Restore the file or pass another --file.",
			).WithFile(file, 0, 0)
		}
		return Reference{}, fmt.Errorf("resolve linked file %q: %w", file, err)
	}
	root, err := filepath.EvalSymlinks(repositoryRoot)
	if err != nil {
		return Reference{}, fmt.Errorf("resolve repository root: %w", err)
	}
	if !isWithin(root, target) {
		return Reference{}, errors.New("linked file must be inside the Git repository")
	}

	relative, _ := filepath.Rel(root, target)
	reference := Reference{
		File: filepath.ToSlash(relative), Format: format, Select: selectPath, Map: fieldMap,
	}
	if _, err := resolveSourcePath(root, reference.File); err != nil {
		return Reference{}, err
	}
	return reference, nil
}

const referenceFormatVersion = 1

type referenceDescriptor struct {
	FormatVersion int `yaml:"formatVersion"`
	Reference     `yaml:",inline"`
}

// ReadDescriptor reads and validates one .ref.yaml file.
func ReadDescriptor(fsys fs.FS, descriptorPath string, id syncdomain.ResourceID) (Reference, error) {
	data, err := fs.ReadFile(fsys, descriptorPath)
	if err != nil {
		return Reference{}, err
	}
	var descriptor referenceDescriptor
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(&descriptor); err != nil {
		return Reference{}, fmt.Errorf("decode descriptor: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			err = errors.New("more than one YAML document")
		}
		return Reference{}, fmt.Errorf("decode descriptor: %w", err)
	}
	if descriptor.FormatVersion != referenceFormatVersion {
		return Reference{}, fmt.Errorf("unsupported formatVersion %d", descriptor.FormatVersion)
	}
	if err := validateDescriptorRelationships(id, descriptor.Reference); err != nil {
		return Reference{}, err
	}
	if err := validateReference(descriptor.Reference); err != nil {
		return Reference{}, err
	}
	reference, err := syncadapter.NormalizeReference(id, descriptor.Reference)
	if err != nil {
		return Reference{}, err
	}
	sortReferenceRelationships(&reference)
	return reference, nil
}

// RenderDescriptor encodes one editable .ref.yaml descriptor.
func RenderDescriptor(id syncdomain.ResourceID, reference Reference) ([]byte, error) {
	if err := validateDescriptorRelationships(id, reference); err != nil {
		return nil, err
	}
	reference, err := syncadapter.NormalizeReference(id, reference)
	if err != nil {
		return nil, err
	}
	sortReferenceRelationships(&reference)
	var content bytes.Buffer
	encoder := yaml.NewEncoder(&content)
	encoder.SetIndent(2)
	if err := encoder.Encode(referenceDescriptor{
		FormatVersion: referenceFormatVersion,
		Reference:     reference,
	}); err != nil {
		return nil, fmt.Errorf("encode descriptor: %w", err)
	}
	if err := encoder.Close(); err != nil {
		return nil, fmt.Errorf("encode descriptor: %w", err)
	}
	return content.Bytes(), nil
}

func validateDescriptorRelationships(id syncdomain.ResourceID, reference Reference) error {
	if id.Kind != syncdomain.KindVariation && reference.Tools != nil {
		return fmt.Errorf("top-level %q is not valid for %s descriptors", syncdomain.FieldTools, id.Kind)
	}
	if id.Kind != syncdomain.KindVariation && reference.Skills != nil {
		return fmt.Errorf("top-level %q is not valid for %s descriptors", syncdomain.FieldSkills, id.Kind)
	}
	var tools, skills []syncdomain.AttachmentRef
	if reference.Tools != nil {
		tools = *reference.Tools
	}
	if reference.Skills != nil {
		skills = *reference.Skills
	}
	return syncdomain.ValidateAttachmentRefs(syncdomain.VariationModeAgent, tools, skills)
}

func sortReferenceRelationships(reference *Reference) {
	sortRefs := func(refs *[]syncdomain.AttachmentRef) *[]syncdomain.AttachmentRef {
		if refs == nil {
			return nil
		}
		values := slices.Clone(*refs)
		slices.SortFunc(values, func(left, right syncdomain.AttachmentRef) int {
			return strings.Compare(left.Key, right.Key)
		})
		return &values
	}
	reference.Tools = sortRefs(reference.Tools)
	reference.Skills = sortRefs(reference.Skills)
}

// ValidateDescriptorTarget rejects a managed definition of the same resource.
func (store Store) ValidateDescriptorTarget(id syncdomain.ResourceID) error {
	managed, err := resourcePath(id)
	if err != nil {
		return err
	}
	if exists, err := store.fileExists(managed, id.Label()); err != nil {
		return err
	} else if exists {
		return definitionConflict(id, id.Label()+" already has a managed file")
	}
	return nil
}

// WriteDescriptor creates or replaces one resource descriptor atomically.
func (store Store) WriteDescriptor(id syncdomain.ResourceID, reference Reference) (string, error) {
	if err := store.ValidateDescriptorTarget(id); err != nil {
		return "", err
	}

	repositoryPath, err := ReferencePath(id)
	if err != nil {
		return "", err
	}
	relativePath := strings.TrimPrefix(repositoryPath, syncdomain.RootDir+"/")
	target := store.absolute(relativePath)
	content, err := RenderDescriptor(id, reference)
	if err != nil {
		return "", err
	}
	current, err := os.ReadFile(target)
	switch {
	case errors.Is(err, os.ErrNotExist):
		if err := createFile(store.root, target, content); err != nil {
			return "", err
		}
	case err != nil:
		return "", fmt.Errorf("read descriptor %q: %w", repositoryPath, err)
	case bytes.Equal(current, content):
		return repositoryPath, nil
	default:
		info, err := os.Stat(target)
		if err != nil {
			return "", fmt.Errorf("inspect descriptor %q: %w", repositoryPath, err)
		}
		if err := ReplaceFileAtomically(target, repositoryPath, current, content, info.Mode().Perm()); err != nil {
			return "", err
		}
	}
	return repositoryPath, nil
}

// validateReference makes sure that the descriptor names a format and a
// clean repository-relative file outside the managed directory.
func validateReference(reference Reference) error {
	if reference.Format == "" {
		return errors.New("ref.format is required")
	}
	return validateSourcePath(reference.File)
}

// validateSourcePath makes sure that file is a clean path in the repository
// and outside the managed directory.
func validateSourcePath(file string) error {
	switch {
	case file == "":
		return errors.New("ref.file is required")
	case !fs.ValidPath(file):
		return fmt.Errorf("ref.file %q must be a repository-relative path", file)
	case file == syncdomain.RootDir || strings.HasPrefix(file, syncdomain.RootDir+"/"):
		return fmt.Errorf("ref.file %q must be outside %s", file, syncdomain.RootDir)
	case path.Clean(file) != file:
		return fmt.Errorf("ref.file %q must be a clean repository-relative path", file)
	default:
		return nil
	}
}

// resolveSourcePath returns the absolute path of a repository source file. It
// resolves symbolic links on each call, because a link can change after the
// resource was linked.
func resolveSourcePath(repositoryRoot, file string) (string, error) {
	if err := validateSourcePath(file); err != nil {
		return "", err
	}
	root, err := filepath.EvalSymlinks(repositoryRoot)
	if err != nil {
		return "", fmt.Errorf("resolve repository root: %w", err)
	}
	target, err := filepath.EvalSymlinks(filepath.Join(root, filepath.FromSlash(file)))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", syncdomain.NewProblem(
				syncdomain.ErrorLinkedSourceMissing,
				fmt.Sprintf("source file %q does not exist", file),
				"Restore the file or run detach.",
			).WithFile(file, 0, 0)
		}
		return "", fmt.Errorf("resolve referenced file %q: %w", file, err)
	}
	if !isWithin(root, target) {
		return "", fmt.Errorf("referenced file %q resolves outside the Git repository", file)
	}
	info, err := os.Stat(target)
	if err != nil {
		return "", fmt.Errorf("inspect referenced file %q: %w", file, err)
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("referenced file %q is not a regular file", file)
	}
	return target, nil
}

// ReadSourceFile reads one repository source file. It resolves symbolic links
// and makes sure that the file is a regular file in the repository.
func ReadSourceFile(repositoryRoot, file string) ([]byte, error) {
	target, err := resolveSourcePath(repositoryRoot, file)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(target)
	if err != nil {
		return nil, fmt.Errorf("read referenced file %q: %w", file, err)
	}
	return data, nil
}
