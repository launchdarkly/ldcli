// Package link validates and writes repository source descriptors.
package link

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
	syncadapter "github.com/launchdarkly/ldcli/internal/sync/adapter"
	synclocal "github.com/launchdarkly/ldcli/internal/sync/local"
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
	// Previous is the descriptor that Prepare read, or nil when none existed.
	Previous []byte
	// written reports that Write published Content, so that Rollback has a
	// write of this process to undo.
	written bool
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
	path, err := synclocal.ReferencePath(options.ID)
	if err != nil {
		return Prepared{}, err
	}
	previous, readErr := os.ReadFile(filepath.Join(options.RepositoryRoot, filepath.FromSlash(path)))
	if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
		return Prepared{}, readErr
	}
	if previous != nil {
		// The link options set the file, the format, the selection, and the
		// map. A relink keeps the tools and skills of the current descriptor,
		// so that it does not read as a local removal of them. The one read
		// serves the snapshot and the lists, so Write checks the bytes that
		// the lists came from.
		current, err := synclocal.ParseDescriptor(previous, options.ID)
		if err != nil {
			return Prepared{}, fmt.Errorf("read the current descriptor %s: %w", path, err)
		}
		reference.Tools, reference.Skills = current.Tools, current.Skills
	}
	reference, err = syncadapter.NormalizeReference(options.ID, reference)
	if err != nil {
		return Prepared{}, err
	}
	definition, err := synclocal.CompileSource(options.RepositoryRoot, options.ID, reference)
	if err != nil {
		return Prepared{}, err
	}
	content, err := synclocal.RenderDescriptor(options.ID, reference)
	if err != nil {
		return Prepared{}, err
	}
	return Prepared{
		ID: options.ID, Reference: reference, Definition: definition, Path: path, Content: content,
		Previous: previous,
	}, nil
}

// Write atomically publishes the prepared descriptor. The descriptor must
// still be as Prepare read it, present or absent, so that a write of another
// process in the review window is not lost.
func (prepared *Prepared) Write(store synclocal.Store) (string, error) {
	path, err := store.WriteDescriptorFrom(prepared.ID, prepared.Reference, prepared.Previous)
	if err == nil {
		prepared.written = true
	}
	return path, err
}

// Rollback undoes a Write that succeeded: it restores the previous
// descriptor, or removes the new one. A descriptor that another process
// wrote stays, also when it has the same content, because a Write that was
// refused or did not run published nothing.
func (prepared *Prepared) Rollback(store synclocal.Store) error {
	if !prepared.written {
		return nil
	}
	return store.RestoreDescriptor(prepared.ID, prepared.Previous, prepared.Content)
}
