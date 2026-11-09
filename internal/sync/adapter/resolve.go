package adapter

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
)

// Resolve returns the node at a select or map path below root.
func (root *Node) Resolve(text string) (*Node, error) {
	node, _, err := root.resolve(text, "")
	return node, err
}

// indexedResolution is one index step on a resolved path: where it is, how
// long the collection was, and which item it chose.
type indexedResolution struct {
	path             string
	collectionLength int
	item             *Node
}

// resolve returns the node at a path and the index steps that it took. scope
// names the path in the identity of each index step.
func (root *Node) resolve(text, scope string) (*Node, []indexedResolution, error) {
	path, err := parsePath(text)
	if err != nil {
		return nil, nil, err
	}
	if path.root {
		return root, nil, nil
	}
	current := root
	var indexes []indexedResolution
	for _, step := range path.steps {
		if step.index != nil {
			if !current.IsCollection() {
				return nil, nil, syncdomain.NewProblem(
					syncdomain.ErrorPathTypeMismatch,
					fmt.Sprintf("index %d needs a collection", *step.index),
					"Select a collection before this index.",
				)
			}
			if *step.index >= len(current.Items) {
				return nil, nil, syncdomain.NewProblem(
					syncdomain.ErrorIndexOutOfRange,
					fmt.Sprintf("index %d is out of range for collection of length %d", *step.index, len(current.Items)),
					"Use an index within the collection.",
				)
			}
			collectionLength := len(current.Items)
			current = current.Items[*step.index]
			indexes = append(indexes, indexedResolution{
				path:             fmt.Sprintf("%s#%d", scope, len(indexes)),
				collectionLength: collectionLength,
				item:             current,
			})
			continue
		}
		if current.Fields == nil {
			return nil, nil, syncdomain.NewProblem(
				syncdomain.ErrorPathTypeMismatch,
				fmt.Sprintf("%q does not contain fields", step.key),
				"Select an object before this field.",
			)
		}
		next, ok := current.Fields[step.key]
		if !ok {
			return nil, nil, syncdomain.NewProblem(
				syncdomain.ErrorPathNotFound,
				fmt.Sprintf("field %q was not found", step.key),
				"Update the select or map path.",
			)
		}
		current = next
		if len(step.filters) == 0 {
			continue
		}
		var matches []*Node
		for _, item := range current.Items {
			matched := true
			for _, filter := range step.filters {
				identity, ok := item.Fields[filter.key]
				if !ok {
					matched = false
					break
				}
				text, ok := identity.Value.(string)
				if !ok {
					return nil, nil, syncdomain.NewProblem(
						syncdomain.ErrorFilterTypeMismatch,
						fmt.Sprintf("selector field %q is not a string", filter.key),
						"Select a direct string identity field.",
					)
				}
				if text != filter.value {
					matched = false
					break
				}
			}
			if matched {
				matches = append(matches, item)
			}
		}
		selector := formatFilters(step.filters)
		switch len(matches) {
		case 0:
			return nil, nil, syncdomain.NewProblem(
				syncdomain.ErrorPathNotFound,
				fmt.Sprintf("selector %s matched no values", selector),
				"Update the selector or restore the source value.",
			)
		case 1:
			current = matches[0]
		default:
			return nil, nil, syncdomain.NewProblem(
				syncdomain.ErrorPathAmbiguous,
				fmt.Sprintf("selector %s matched %d values", selector, len(matches)),
				"Use a selector that matches one resource.",
			)
		}
	}
	return current, indexes, nil
}

// indexedSelections records the identity of each indexed item, so that a
// later sync can tell when the item at that position changed.
func indexedSelections(indexes []indexedResolution, selectedRoot *Node) ([]syncdomain.IndexedSelection, error) {
	selections := make([]syncdomain.IndexedSelection, 0, len(indexes))
	var ancestors []string
	for _, index := range indexes {
		identity := directSelectionIdentity(index.item)
		if identity == nil {
			// Without scalar identity, bind the nearest selected resource root and prior indexed ancestors.
			identity = struct {
				Root      any      `json:"root"`
				Ancestors []string `json:"ancestors,omitempty"`
				Item      any      `json:"item"`
			}{
				Root:      stableNodeValue(selectedRoot, map[*Node]bool{}),
				Ancestors: slices.Clone(ancestors),
				Item:      stableNodeValue(index.item, map[*Node]bool{}),
			}
		}
		fingerprint, err := selectionFingerprint(identity)
		if err != nil {
			return nil, err
		}
		selections = append(selections, syncdomain.IndexedSelection{
			Path: index.path, CollectionLength: index.collectionLength, Fingerprint: fingerprint,
		})
		ancestors = append(ancestors, fingerprint)
	}
	return selections, nil
}

func directSelectionIdentity(node *Node) any {
	if node == nil || node.Fields == nil {
		return nil
	}
	identity := map[string]any{}
	for _, key := range []string{"id", "name", "role", "type"} {
		field, ok := node.Fields[key]
		if !ok || !scalarValue(field.Value) {
			continue
		}
		identity[key] = field.Value
	}
	if len(identity) == 0 {
		return nil
	}
	return identity
}

func scalarValue(value any) bool {
	switch value.(type) {
	case string, bool, int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, float32, float64:
		return true
	default:
		return false
	}
}

func stableNodeValue(node *Node, visited map[*Node]bool) any {
	if node == nil {
		return nil
	}
	if node.Value != nil {
		return node.Value
	}
	if visited[node] {
		return "<cycle>"
	}
	visited[node] = true
	defer delete(visited, node)
	if node.Fields != nil {
		value := make(map[string]any, len(node.Fields))
		for key, field := range node.Fields {
			value[key] = stableNodeValue(field, visited)
		}
		return value
	}
	if node.IsCollection() {
		value := make([]any, len(node.Items))
		for index, item := range node.Items {
			value[index] = stableNodeValue(item, visited)
		}
		return value
	}
	return nil
}

func selectionFingerprint(value any) (string, error) {
	canonical, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("encode indexed selection fingerprint: %w", err)
	}
	sum := sha256.Sum256(canonical)
	return fmt.Sprintf("sha256:%x", sum), nil
}

func formatFilters(filters []pathFilter) string {
	parts := make([]string, len(filters))
	for index, filter := range filters {
		parts[index] = fmt.Sprintf("%s=%q", filter.key, filter.value)
	}
	return strings.Join(parts, ",")
}
