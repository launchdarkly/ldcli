package local

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
)

// ErrNoDirectory reports that the repository has no managed directory.
var ErrNoDirectory = errors.New(".launchdarkly directory not found")

// ParseError identifies the managed file that is not valid.
type ParseError struct {
	Path string
	Err  error
}

func (e ParseError) Error() string { return e.Path + ": " + e.Err.Error() }
func (e ParseError) Unwrap() error { return e.Err }

// Compile reads every variation in a file system that contains a managed
// directory. Tests use it with an in-memory file system.
func Compile(fsys fs.FS) ([]syncdomain.SyncedResource, error) {
	return compile(fsys, func(reference Reference) ([]byte, error) {
		return readReferenceFromFS(fsys, reference)
	})
}

// CompileWorkspace reads every variation in a repository. It rejects a
// symbolic link in a managed path, and a linked file outside the repository.
func CompileWorkspace(repositoryRoot string) ([]syncdomain.SyncedResource, error) {
	root, err := filepath.EvalSymlinks(repositoryRoot)
	if err != nil {
		return nil, fmt.Errorf("resolve repository root: %w", err)
	}
	return compile(workspaceFS{root: root}, func(reference Reference) ([]byte, error) {
		return ReadReference(root, reference)
	})
}

// Compile reads every variation in the repository of the store.
func (store Store) Compile() ([]syncdomain.SyncedResource, error) {
	return CompileWorkspace(store.repositoryRoot)
}

// workspaceFS opens repository files and rejects a path that goes through a
// symbolic link. Sync owns the managed files, so a link is not valid there.
type workspaceFS struct {
	root string
}

func (fsys workspaceFS) Open(name string) (fs.File, error) {
	if !fs.ValidPath(name) {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrInvalid}
	}
	target := filepath.Join(fsys.root, filepath.FromSlash(name))
	resolved, err := filepath.EvalSymlinks(target)
	if err != nil {
		return nil, err
	}
	if filepath.Clean(resolved) != filepath.Clean(target) {
		return nil, &fs.PathError{Op: "open", Path: name, Err: errors.New("symbolic links are not supported")}
	}
	return os.Open(target)
}

// compile reads the variation files of each project, in identity order.
func compile(fsys fs.FS, readReference func(Reference) ([]byte, error)) ([]syncdomain.SyncedResource, error) {
	entries, err := fs.ReadDir(fsys, syncdomain.RootDir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, ErrNoDirectory
	}
	if err != nil {
		return nil, err
	}

	var variations []syncdomain.SyncedResource
	for _, entry := range entries {
		// Each directory is a project. Files in the managed directory itself
		// are not resources.
		if !entry.IsDir() {
			continue
		}
		projectVariations, err := compileProject(fsys, entry.Name(), readReference)
		if err != nil {
			return nil, err
		}
		variations = append(variations, projectVariations...)
	}

	slices.SortFunc(variations, func(left, right syncdomain.SyncedResource) int {
		return syncdomain.CompareResourceIDs(left.ID(), right.ID())
	})
	return variations, nil
}

// compileProject reads every variation file in one project.
func compileProject(
	fsys fs.FS,
	projectKey string,
	readReference func(Reference) ([]byte, error),
) ([]syncdomain.SyncedResource, error) {
	configsRoot := path.Join(syncdomain.RootDir, projectKey, configsDir)
	readAttachment := func(kind syncdomain.AttachmentKind, key string) (syncdomain.Attachment, error) {
		return readAttachment(fsys, projectKey, kind, key)
	}

	var variations []syncdomain.SyncedResource
	err := fs.WalkDir(fsys, configsRoot, func(name string, entry fs.DirEntry, err error) error {
		switch {
		case name == configsRoot && errors.Is(err, fs.ErrNotExist):
			// A project can have no variations.
			return nil
		case err != nil:
			return err
		case entry.IsDir():
			return nil
		}
		if id, ok := ParseManagedPath(name); !ok || id.Kind != syncdomain.KindVariation {
			return nil
		}

		data, err := fs.ReadFile(fsys, name)
		if err != nil {
			return err
		}
		variation, err := parseVariation(localFile{
			ProjectKey:     projectKey,
			RelPath:        strings.TrimPrefix(name, configsRoot+"/"),
			Data:           data,
			ReadReference:  readReference,
			ReadAttachment: readAttachment,
		})
		if err != nil {
			return ParseError{Path: name, Err: err}
		}
		variations = append(variations, variation)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return variations, nil
}
