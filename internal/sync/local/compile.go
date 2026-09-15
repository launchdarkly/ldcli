package local

import (
	"cmp"
	"errors"
	"io/fs"
	"path"
	"slices"
	"strings"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
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

func Compile(fsys fs.FS) ([]syncdomain.SyncedResource, error) {
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

		variations, err := compileProjectVariations(fsys, entry.Name())
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
) ([]syncdomain.SyncedResource, error) {
	dir := path.Join(syncdomain.RootDir, projectKey, configsDir)

	var resources []syncdomain.SyncedResource

	err := fs.WalkDir(fsys, dir, func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
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
			ProjectKey: projectKey,
			RelPath:    relPath,
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

func compareResources(a, b syncdomain.SyncedResource) int {
	return cmp.Or(
		cmp.Compare(a.ProjectKey, b.ProjectKey),
		cmp.Compare(a.LookupKey, b.LookupKey),
	)
}
