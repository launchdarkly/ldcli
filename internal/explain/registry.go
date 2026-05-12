package explain

import (
	"fmt"
	"sort"
	"strings"
	"sync"
)

// Registry resolves a command path to an Explainer. It's deliberately a simple
// in-memory map keyed by the full slash-joined command path: agents reference
// commands by argv (e.g. ["flags", "update"]) which we normalize to
// "flags/update".
//
// The registry layering is:
//
//  1. Curated entries registered via Register() (highest fidelity).
//  2. Fallback explainers tried in order (OpenAPI-driven, then flag-tree).
//
// The registry is safe for concurrent reads after initial setup. We don't
// expect Register() to be called from concurrent goroutines.
type Registry struct {
	mu        sync.RWMutex
	curated   map[string]Explainer
	fallbacks []Explainer
}

// NewRegistry creates an empty registry. Most callers will want
// DefaultRegistry, which pre-registers the bundled curated explainers.
func NewRegistry() *Registry {
	return &Registry{curated: make(map[string]Explainer)}
}

// Register associates an Explainer with a command path. The path is the
// argv slice that would be passed to ldcli, e.g. ["flags", "update"]. Calling
// Register twice for the same path replaces the prior entry.
func (r *Registry) Register(commandPath []string, e Explainer) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.curated[key(commandPath)] = e
}

// AddFallback appends a fallback explainer. Fallbacks are consulted only if no
// curated explainer matches.
func (r *Registry) AddFallback(e Explainer) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.fallbacks = append(r.fallbacks, e)
}

// Resolve returns a CommandExplanation for the given command path. Returns
// ErrCommandNotFound when no curated or fallback explainer can produce one.
func (r *Registry) Resolve(commandPath []string) (CommandExplanation, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if e, ok := r.curated[key(commandPath)]; ok {
		return e.Explain(commandPath)
	}

	for _, fb := range r.fallbacks {
		exp, err := fb.Explain(commandPath)
		if err == nil {
			return exp, nil
		}
		// Fallbacks may legitimately not know a command; only bubble up
		// non-not-found errors. We treat ErrCommandNotFound as "try next".
		if err != ErrCommandNotFound {
			return CommandExplanation{}, err
		}
	}

	return CommandExplanation{}, fmt.Errorf("%w: %s", ErrCommandNotFound, strings.Join(commandPath, " "))
}

// HasCurated reports whether the given path has a curated (not fallback)
// explainer. This is used by tests and by the AGENTS.md "coverage" status.
func (r *Registry) HasCurated(commandPath []string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, ok := r.curated[key(commandPath)]
	return ok
}

// CuratedPaths returns the sorted list of command paths with curated
// explainers. Useful for documentation / coverage reports.
func (r *Registry) CuratedPaths() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]string, 0, len(r.curated))
	for k := range r.curated {
		out = append(out, strings.ReplaceAll(k, "/", " "))
	}
	sort.Strings(out)
	return out
}

func key(commandPath []string) string {
	return strings.Join(commandPath, "/")
}

// DefaultRegistry returns a Registry pre-populated with the curated
// explainers bundled with ldcli. Callers can still Register more entries on
// top of this.
func DefaultRegistry() *Registry {
	r := NewRegistry()
	r.Register([]string{"flags", "update"}, flagsUpdateExplainer{})
	r.Register([]string{"flags", "list"}, flagsListExplainer{})
	return r
}
