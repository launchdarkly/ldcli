// Package link validates and writes repository source descriptors.
package link

import (
	"errors"
	"os"
	"path/filepath"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
	synclocal "github.com/launchdarkly/ldcli/internal/sync/local"
	syncreference "github.com/launchdarkly/ldcli/internal/sync/reference"
)

// Options are the explicit inputs for one repository source link.
type Options struct {
	Store            synclocal.Store
	RepositoryRoot   string
	WorkingDirectory string
	ID               syncdomain.ResourceID
	File             string
	Format           string
	Select           string
	Map              map[string]string
}

// Prepared is one validated descriptor and its compiled local resource.
type Prepared struct {
	ID         syncdomain.ResourceID
	Reference  syncdomain.Reference
	Definition synclocal.SourceDefinition
	Path       string
	Content    []byte
	Previous   []byte
}

// Prepare validates a repository source without writing its descriptor.
func Prepare(options Options) (Prepared, error) {
	if err := options.Store.ValidateDescriptorTarget(options.ID); err != nil {
		return Prepared{}, err
	}
	reference, err := synclocal.NewSourceReference(
		options.RepositoryRoot,
		options.WorkingDirectory,
		options.File,
		options.Format,
		options.Select,
		options.Map,
	)
	if err != nil {
		return Prepared{}, err
	}
	reference, err = syncreference.NormalizeReference(options.ID, reference)
	if err != nil {
		return Prepared{}, err
	}
	definition, err := synclocal.CompileSource(options.RepositoryRoot, options.ID, reference)
	if err != nil {
		return Prepared{}, err
	}
	path, err := synclocal.ReferencePath(options.ID)
	if err != nil {
		return Prepared{}, err
	}
	content, err := synclocal.RenderDescriptor(options.ID, reference)
	if err != nil {
		return Prepared{}, err
	}
	previous, readErr := os.ReadFile(filepath.Join(options.RepositoryRoot, filepath.FromSlash(path)))
	if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
		return Prepared{}, readErr
	}
	return Prepared{
		ID: options.ID, Reference: reference, Definition: definition, Path: path, Content: content,
		Previous: previous,
	}, nil
}

// Write atomically publishes the prepared descriptor.
func (prepared Prepared) Write(store synclocal.Store) (string, error) {
	return store.WriteDescriptor(prepared.ID, prepared.Reference)
}
