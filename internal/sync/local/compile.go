package local

import (
	"cmp"
	"errors"
	"io/fs"
	"os"
	"path"
	"slices"
	"strings"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
)

// ErrNoDirectory reports that no local sync directory exists.
var ErrNoDirectory = errors.New(".launchdarkly directory not found")

// ParseError identifies the local file that could not be compiled.
type ParseError struct {
	Path string
	Err  error
}

// Error includes the repository-relative file that could not be compiled.
func (e ParseError) Error() string {
	return e.Path + ": " + e.Err.Error()
}

// Unwrap exposes the underlying syntax or validation error.
func (e ParseError) Unwrap() error {
	return e.Err
}

// Compile reads local resources from an arbitrary filesystem.
func Compile(fsys fs.FS) ([]syncdomain.SyncedResource, error) {
	return compile(fsys, func(reference Reference) ([]byte, error) {
		return readReferenceFromFS(fsys, reference)
	})
}

// CompileWorkspace compiles local resources and safely resolves references
// within the Git repository.
func CompileWorkspace(repositoryRoot string) ([]syncdomain.SyncedResource, error) {
	return compile(os.DirFS(repositoryRoot), func(reference Reference) ([]byte, error) {
		return readWorkspaceReference(repositoryRoot, reference)
	})
}

// compile walks every managed project and delegates reference loading to the
// caller so tests and real workspaces share the same parser.
func compile(fsys fs.FS, readReference func(Reference) ([]byte, error)) ([]syncdomain.SyncedResource, error) {
	entries, err := fs.ReadDir(fsys, syncdomain.RootDir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, ErrNoDirectory
	}
	if err != nil {
		return nil, err
	}

	var resources []syncdomain.SyncedResource

	// Directories immediately below .launchdarkly are project scopes. Files at
	// the root, including the manifest, are handled by their owning packages.
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		variations, err := compileProjectVariations(fsys, entry.Name(), readReference)
		if err != nil {
			return nil, err
		}

		resources = append(resources, variations...)
	}

	slices.SortFunc(resources, compareResources)

	return resources, nil
}

// compileProjectVariations turns every supported wrapper in one project into
// the common resource representation consumed by reconciliation.
func compileProjectVariations(
	fsys fs.FS,
	projectKey string,
	readReference func(Reference) ([]byte, error),
) ([]syncdomain.SyncedResource, error) {
	dir := path.Join(syncdomain.RootDir, projectKey, configsDir)

	var resources []syncdomain.SyncedResource

	err := fs.WalkDir(fsys, dir, func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			// A project may legitimately contain no resources of this kind.
			if name == dir && errors.Is(err, fs.ErrNotExist) {
				return nil
			}
			return err
		}
		if entry.IsDir() {
			return nil
		}

		relPath := strings.TrimPrefix(name, dir+"/")
		// Ignore files owned by other resource kinds. Each compiler recognizes
		// only its own directory shape and suffix.
		if relPath == name || !isVariationFile(relPath) {
			return nil
		}

		data, err := fs.ReadFile(fsys, name)
		if err != nil {
			return err
		}

		resource, err := parseVariation(localFile{
			ProjectKey:    projectKey,
			RelPath:       relPath,
			Data:          data,
			ReadReference: readReference,
		})
		if err != nil {
			// Preserve the managed path so users can locate malformed content
			// while callers can still inspect the parser error through Unwrap.
			return ParseError{Path: name, Err: err}
		}

		resources = append(resources, resource)

		return nil
	})
	if err != nil {
		return nil, err
	}

	return resources, nil
}

// compareResources provides deterministic project and lookup-key ordering.
func compareResources(a, b syncdomain.SyncedResource) int {
	return cmp.Or(
		cmp.Compare(a.ProjectKey, b.ProjectKey),
		cmp.Compare(a.LookupKey, b.LookupKey),
	)
}
