package reference

import (
	"fmt"
	"sync"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
)

// Adapter inspects one source format and supplies its conventional mapping.
// It receives bytes only and never reads files, runs code, or uses the network.
type Adapter interface {
	Format() string
	Inspect(SourceBundle) (Inspection, error)
	Defaults(syncdomain.Kind) (selectPath string, fieldMap map[string]string)
}

// DependencyReporter lists repository files that can contribute mapped values.
type DependencyReporter interface {
	Dependencies(Document) ([]string, error)
}

// Inspection is the logical document that an adapter exposes to select paths.
type Inspection struct {
	root *sourceNode
}

// Node is one logical value exposed by an adapter.
type Node struct {
	Value      any
	Fields     map[string]*Node
	Items      []*Node
	Start      int
	End        int
	File       string
	Capability syncdomain.Capability
	Reason     string
	Code       syncdomain.ErrorCode
	Encode     func(any) ([]byte, error)
}

// NewInspection creates an inspection from an adapter's logical root.
func NewInspection(root *Node) Inspection {
	return Inspection{root: importNode(root)}
}

func importNode(node *Node) *sourceNode {
	if node == nil {
		return nil
	}
	result := &sourceNode{
		value: node.Value, start: node.Start, end: node.End, file: node.File,
		capability: node.Capability, reason: node.Reason, code: node.Code, encode: node.Encode,
	}
	if len(node.Fields) != 0 {
		result.fields = make(map[string]*sourceNode, len(node.Fields))
		for key, child := range node.Fields {
			result.fields[key] = importNode(child)
		}
	}
	for _, child := range node.Items {
		result.items = append(result.items, importNode(child))
	}
	return result
}

type adapterRegistry struct {
	mu       sync.RWMutex
	adapters map[string]Adapter
}

var formats = adapterRegistry{adapters: make(map[string]Adapter)}

// Register adds one independent source format.
func Register(adapter Adapter) error {
	if adapter == nil || adapter.Format() == "" {
		return fmt.Errorf("adapter format is required")
	}
	formats.mu.Lock()
	defer formats.mu.Unlock()
	if _, exists := formats.adapters[adapter.Format()]; exists {
		return fmt.Errorf("source format %q is already registered", adapter.Format())
	}
	formats.adapters[adapter.Format()] = adapter
	return nil
}

func registeredAdapter(format string) (Adapter, error) {
	formats.mu.RLock()
	adapter := formats.adapters[format]
	formats.mu.RUnlock()
	if adapter == nil {
		return nil, syncdomain.NewProblem(
			syncdomain.ErrorUnsupportedFormat,
			fmt.Sprintf("source format %q is not supported", format),
			"Use a registered format name.",
		)
	}
	return adapter, nil
}

// DependencyPaths returns repository dependencies reported by one format.
func DependencyPaths(format string, primary Document) ([]string, error) {
	adapter, err := registeredAdapter(format)
	if err != nil {
		return nil, err
	}
	reporter, ok := adapter.(DependencyReporter)
	if !ok {
		return nil, nil
	}
	return reporter.Dependencies(primary)
}

func mustRegister(adapter Adapter) {
	if err := Register(adapter); err != nil {
		panic(err)
	}
}
