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

func (e ParseError) Error() string {
	return e.Path + ": " + e.Err.Error()
}

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

func compile(fsys fs.FS, readReference func(Reference) ([]byte, error)) ([]syncdomain.SyncedResource, error) {
	entries, err := fs.ReadDir(fsys, syncdomain.RootDir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, ErrNoDirectory
	}
	if err != nil {
		return nil, err
	}

	var resources []syncdomain.SyncedResource

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

func compileProjectVariations(
	fsys fs.FS,
	projectKey string,
	readReference func(Reference) ([]byte, error),
) ([]syncdomain.SyncedResource, error) {
	dir := path.Join(syncdomain.RootDir, projectKey, configsDir)

	var resources []syncdomain.SyncedResource

	err := fs.WalkDir(fsys, dir, func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			if name == dir && errors.Is(err, fs.ErrNotExist) {
				return nil
			}
			return err
		}
		if entry.IsDir() {
			return nil
		}

		relPath := strings.TrimPrefix(name, dir+"/")
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

func compareResources(a, b syncdomain.SyncedResource) int {
	return cmp.Or(
		cmp.Compare(a.ProjectKey, b.ProjectKey),
		cmp.Compare(a.LookupKey, b.LookupKey),
	)
}
