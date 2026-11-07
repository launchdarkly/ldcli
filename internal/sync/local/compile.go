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
	return compile(fsys, func(file string) ([]byte, error) {
		return readSourceFileFromFS(fsys, file)
	})
}

// CompileWorkspace reads every variation in a repository. It rejects a
// symbolic link in a managed path, and a linked file outside the repository.
func CompileWorkspace(repositoryRoot string) ([]syncdomain.SyncedResource, error) {
	root, err := filepath.EvalSymlinks(repositoryRoot)
	if err != nil {
		return nil, fmt.Errorf("resolve repository root: %w", err)
	}
	return compile(workspaceFS{root: root}, func(file string) ([]byte, error) {
		return ReadSourceFile(root, file)
	})
}

// Compile reads every variation in the repository of the store.
func (store Store) Compile() ([]syncdomain.SyncedResource, error) {
	return CompileWorkspace(store.repositoryRoot)
}

// errSymlinkNotSupported rejects a symbolic link in the managed directory.
var errSymlinkNotSupported = errors.New("symbolic links are not supported")

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
		return nil, &fs.PathError{Op: "open", Path: name, Err: errSymlinkNotSupported}
	}
	return os.Open(target)
}

// compile reads the variation files of each project, in identity order.
func compile(fsys fs.FS, readSource func(file string) ([]byte, error)) ([]syncdomain.SyncedResource, error) {
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
		projectVariations, err := compileProject(fsys, entry.Name(), readSource)
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
	readSource func(file string) ([]byte, error),
) ([]syncdomain.SyncedResource, error) {
	configsRoot := path.Join(syncdomain.RootDir, projectKey, configsDir)
	readAttachment := func(kind syncdomain.AttachmentKind, key string) (syncdomain.Attachment, error) {
		id := syncdomain.ResourceID{Kind: syncdomain.Kind(kind), ProjectKey: projectKey, LookupKey: key}
		descriptorPath, err := ReferencePath(id)
		if err != nil {
			return syncdomain.Attachment{}, err
		}
		if _, err := fs.Stat(fsys, descriptorPath); err == nil {
			managedPath, _ := ResourcePath(id)
			if _, managedErr := fs.Stat(fsys, managedPath); managedErr == nil {
				return syncdomain.Attachment{}, definitionConflict(
					id, id.Label()+" has a managed file and a descriptor",
				)
			}
			definition, err := compileDescriptor(fsys, descriptorPath, id, readSource, nil)
			if err != nil {
				return syncdomain.Attachment{}, err
			}
			return definition.Attachment.Attachment, nil
		} else if !errors.Is(err, fs.ErrNotExist) {
			return syncdomain.Attachment{}, err
		}
		managedPath, err := ResourcePath(id)
		if err != nil {
			return syncdomain.Attachment{}, err
		}
		if _, err := fs.Stat(fsys, managedPath); err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return syncdomain.Attachment{}, missingLocalAttachmentDefinition(id)
			}
			return syncdomain.Attachment{}, err
		}
		return readAttachment(fsys, projectKey, kind, key)
	}

	var variations []syncdomain.SyncedResource
	definitions := make(map[syncdomain.ResourceID]string)
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
		id, managed := ParseManagedPath(name)
		if !managed {
			id, managed = ParseReferencePath(name)
		}
		if !managed || id.Kind != syncdomain.KindVariation {
			return nil
		}
		// Info reads the entry itself, so it reports a link on a file system
		// whose directory listing has no file types.
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.Mode()&fs.ModeSymlink != 0 {
			// Sync owns the managed files, so a link is not a definition, as
			// it is not for a write. A leftover link beside a descriptor is
			// skipped, and the descriptor defines the variation. A link
			// alone is an error, not an absent variation.
			if skip, err := leftoverManagedLink(fsys, name, id); skip || err != nil {
				return err
			}
			return ParseError{Path: name, Err: errSymlinkNotSupported}
		}
		if existing, duplicate := definitions[id]; duplicate {
			return ParseError{Path: name, Err: definitionConflict(
				id, fmt.Sprintf("%s is also defined by %s", id.Label(), existing),
			)}
		}
		definitions[id] = name

		var variation syncdomain.SyncedResource
		if strings.HasSuffix(name, referenceFileSuffix) {
			var definition SourceDefinition
			if definition, err = compileDescriptor(fsys, name, id, readSource, readAttachment); err == nil {
				variation = *definition.Variation
			}
		} else {
			var data []byte
			data, err = fs.ReadFile(fsys, name)
			if err == nil {
				variation, err = parseVariation(localFile{
					ProjectKey:     projectKey,
					RelPath:        strings.TrimPrefix(name, configsRoot+"/"),
					Data:           data,
					ReadAttachment: readAttachment,
				})
			}
		}
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

// leftoverManagedLink reports whether a symbolic link at a managed path has
// a descriptor beside it, which defines the resource instead of the link.
func leftoverManagedLink(fsys fs.FS, name string, id syncdomain.ResourceID) (bool, error) {
	if strings.HasSuffix(name, referenceFileSuffix) {
		return false, nil
	}
	descriptorPath, err := ReferencePath(id)
	if err != nil {
		return false, err
	}
	_, err = fs.Stat(fsys, descriptorPath)
	switch {
	case err == nil:
		return true, nil
	case errors.Is(err, fs.ErrNotExist):
		return false, nil
	default:
		return false, err
	}
}

func missingLocalAttachmentDefinition(id syncdomain.ResourceID) error {
	return syncdomain.NewProblem(
		syncdomain.ErrorAttachmentDefinitionMissing,
		fmt.Sprintf("%s %q has no local definition", id.Kind, id.LookupKey),
		fmt.Sprintf(
			"Run ldcli sync %ss add --project %s --key %s, or ldcli sync %ss link --project %s --key %s --file <path> --format <format>.",
			id.Kind, id.ProjectKey, id.LookupKey, id.Kind, id.ProjectKey, id.LookupKey,
		),
	).WithResource(id)
}
