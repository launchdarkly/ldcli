package sync

import (
	"cmp"
	"errors"
	"io/fs"
	"path"
	"slices"
	"strings"
)

var ErrNoDirectory = errors.New(".launchdarkly directory not found")

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

func Compile(fsys fs.FS) ([]SyncedResource, error) {
	return compile(fsys, DefaultParsers())
}

func compile(fsys fs.FS, parsers []Parser) ([]SyncedResource, error) {
	entries, err := fs.ReadDir(fsys, RootDir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, ErrNoDirectory
	}
	if err != nil {
		return nil, err
	}

	var resources []SyncedResource

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		parsed, err := compileProject(fsys, entry.Name(), parsers)
		if err != nil {
			return nil, err
		}

		resources = append(resources, parsed...)
	}

	slices.SortFunc(resources, compareResources)

	return resources, nil
}

func compileProject(fsys fs.FS, projectKey string, parsers []Parser) ([]SyncedResource, error) {
	var resources []SyncedResource

	for _, parser := range parsers {
		parsed, err := compileKind(fsys, projectKey, parser)
		if err != nil {
			return nil, err
		}

		resources = append(resources, parsed...)
	}

	return resources, nil
}

func compileKind(fsys fs.FS, projectKey string, parser Parser) ([]SyncedResource, error) {
	dir := path.Join(RootDir, projectKey, parser.Dir())

	var resources []SyncedResource

	err := fs.WalkDir(fsys, dir, func(name string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}

		rel := strings.TrimPrefix(name, dir+"/")
		if rel == name || !parser.Accept(rel) {
			return nil
		}

		data, err := fs.ReadFile(fsys, name)
		if err != nil {
			return err
		}

		resource, err := parser.Parse(File{
			ProjectKey: projectKey,
			RelPath:    rel,
			Data:       data,
		})
		if err != nil {
			return ParseError{Path: name, Err: err}
		}

		resources = append(resources, resource)

		return nil
	})
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	return resources, nil
}

func compareResources(a, b SyncedResource) int {
	return cmp.Or(
		cmp.Compare(a.ProjectKey, b.ProjectKey),
		cmp.Compare(a.Kind, b.Kind),
		cmp.Compare(a.LookupKey, b.LookupKey),
	)
}
