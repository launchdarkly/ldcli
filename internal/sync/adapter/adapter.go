// Package adapter is the contract between the sync engine and one source
// format. An adapter turns the bytes of a file into a tree of logical nodes.
// This package resolves select and map paths on that tree, builds typed
// projections, and plans exact edits. Adapters never read files, run code, or
// use the network, so they cannot get update planning, no-op detection, or
// capability checks wrong.
package adapter

import (
	"fmt"
	"sync"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
)

// Adapter inspects one source format.
//
// Format is the name that a descriptor and the --format flag use. Behavior
// names the version of the mapping rules of the adapter, such as python/1.
// It changes when a change to the adapter can change a mapped value, so that
// every binding of the format gets a review. Inspect returns the logical tree
// of a source bundle. Defaults returns the select path and the field map that
// a descriptor of a kind can leave out, or an empty select path when the
// format needs an explicit one.
type Adapter interface {
	Format() string
	Behavior() string
	Inspect(SourceBundle) (*Node, error)
	Defaults(syncdomain.Kind) (selectPath string, fieldMap map[string]string)
}

// DependencyReporter is an optional adapter interface. It lists the
// repository files, relative to the repository root, whose bytes can change
// the projection of a primary source. The host reads them.
type DependencyReporter interface {
	Dependencies(Document) ([]string, error)
}

// Document is one repository file supplied to an adapter. ReadOnly marks a
// file that sync reads but never writes, such as a generated module, so a
// value in it is read-only.
type Document struct {
	Path     string
	Data     []byte
	ReadOnly bool
}

// SourceBundle is a primary source and the dependencies that its format
// reported, by repository-relative path.
type SourceBundle struct {
	Primary      Document
	Dependencies map[string]Document
}

// Capability is the shared mapped-field capability.
type Capability = syncdomain.Capability

const (
	CapabilityDirect   = syncdomain.CapabilityDirect
	CapabilityReadOnly = syncdomain.CapabilityReadOnly
)

// TextEdit replaces one exact byte range of one file.
type TextEdit struct {
	File        string
	Start       int
	End         int
	Expected    []byte
	Replacement []byte
}

// FieldUpdate is the capability of one mapped field and the edits that give
// it the desired value.
type FieldUpdate struct {
	Capability Capability
	Edits      []TextEdit
	Reason     string
}

// UpdateResult has one entry for each mapped field whose value changes.
type UpdateResult struct {
	Fields map[syncdomain.Field]FieldUpdate
}

type registry struct {
	mu       sync.RWMutex
	adapters map[string]Adapter
}

var formats = registry{adapters: make(map[string]Adapter)}

// Register adds one source format. A format name registers once.
func Register(registered Adapter) error {
	if registered == nil || registered.Format() == "" {
		return fmt.Errorf("adapter format is required")
	}
	formats.mu.Lock()
	defer formats.mu.Unlock()
	if _, exists := formats.adapters[registered.Format()]; exists {
		return fmt.Errorf("source format %q is already registered", registered.Format())
	}
	formats.adapters[registered.Format()] = registered
	return nil
}

// MustRegister registers a format from an init function.
func MustRegister(registered Adapter) {
	if err := Register(registered); err != nil {
		panic(err)
	}
}

func lookup(format string) (Adapter, error) {
	formats.mu.RLock()
	registered := formats.adapters[format]
	formats.mu.RUnlock()
	if registered == nil {
		return nil, syncdomain.NewProblem(
			syncdomain.ErrorUnsupportedFormat,
			fmt.Sprintf("source format %q is not supported", format),
			"Use a registered format name.",
		)
	}
	return registered, nil
}

// Behavior returns the behavior version of a registered format.
func Behavior(format string) (string, error) {
	registered, err := lookup(format)
	if err != nil {
		return "", err
	}
	return registered.Behavior(), nil
}

// DependencyPaths returns the repository files that a format reports for a
// primary source.
func DependencyPaths(format string, primary Document) ([]string, error) {
	registered, err := lookup(format)
	if err != nil {
		return nil, err
	}
	reporter, ok := registered.(DependencyReporter)
	if !ok {
		return nil, nil
	}
	return reporter.Dependencies(primary)
}

// Parse returns the logical tree of a bundle in the named format. Every node
// without a file gets the path of the primary document.
func Parse(bundle SourceBundle, format string) (*Node, error) {
	registered, err := lookup(format)
	if err != nil {
		return nil, err
	}
	root, err := registered.Inspect(bundle)
	if err != nil {
		if _, ok := syncdomain.ProblemFrom(err); !ok {
			err = syncdomain.WrapProblem(syncdomain.NewProblem(
				syncdomain.ErrorInvalidSource,
				err.Error(),
				"Fix the source syntax or use a supported static form.",
			).WithFile(bundle.Primary.Path, 0, 0), err)
		}
	}
	root.SetFile(bundle.Primary.Path)
	return root, err
}

// ParseDocument returns the logical tree of one document without dependencies.
func ParseDocument(document Document, format string) (*Node, error) {
	return Parse(SourceBundle{Primary: document}, format)
}
