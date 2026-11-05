package adapter

import (
	"bytes"
	"encoding/json"
	"fmt"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
)

// UpdateSource plans the exact edits that give the mapped fields of a source
// the values of the desired projection. A field whose value is the same has
// no entry. A field that the source cannot write has a read-only entry.
func UpdateSource(
	bundle SourceBundle,
	id syncdomain.ResourceID,
	reference syncdomain.Reference,
	desired syncdomain.Projection,
) (UpdateResult, error) {
	reference, err := NormalizeReference(id, reference)
	if err != nil {
		return UpdateResult{}, err
	}
	if desired.Kind() != id.Kind {
		return UpdateResult{}, fmt.Errorf("projection kind %q does not match %s", desired.Kind(), id.Label())
	}
	root, err := Parse(bundle, reference.Format)
	if err != nil {
		return UpdateResult{}, annotateProblem(err, id, bundle.Primary.Path)
	}
	selected, err := root.Resolve(reference.Select)
	if err != nil {
		return UpdateResult{}, annotateProblem(fmt.Errorf("select %q: %w", reference.Select, err), id, bundle.Primary.Path)
	}

	result := UpdateResult{Fields: make(map[syncdomain.Field]FieldUpdate)}
	for name, sourcePath := range reference.Map {
		field := syncdomain.Field(name)
		wanted, present := desired.Value(field)
		if !present {
			continue
		}
		current, err := selected.Resolve(sourcePath)
		if err != nil {
			return UpdateResult{}, fmt.Errorf("%s: map field %q: %w", bundle.Primary.Path, field, err)
		}
		if field == syncdomain.FieldPrompt || field == syncdomain.FieldMessages {
			if semanticEqual(syncdomain.NormalizePrompt(current.Value), syncdomain.NormalizePrompt(wanted)) {
				continue
			}
			if update, handled := updatePrompt(bundle, current, wanted); handled {
				if len(update.Edits) != 0 || update.Capability != CapabilityDirect {
					result.Fields[field] = update
				}
				continue
			}
		}
		if semanticEqual(current.Value, wanted) {
			continue
		}
		result.Fields[field] = updateLeaf(bundle, current, wanted)
	}
	return result, nil
}

// updatePrompt reconciles the two prompt shapes. A text source holds one
// system message. A message list source changes the content of each message
// in place. handled is false when the leaf update applies.
func updatePrompt(bundle SourceBundle, current *Node, wanted any) (update FieldUpdate, handled bool) {
	have, sourceIsMessages := current.Value.([]syncdomain.Message)
	want, wantIsMessages := wanted.([]syncdomain.Message)
	switch {
	case sourceIsMessages:
		if text, ok := wanted.(string); ok {
			want = []syncdomain.Message{{Role: syncdomain.RoleSystem, Content: text}}
		}
		return updateMessageContents(bundle, current, have, want), true
	case wantIsMessages:
		if len(want) != 1 || want[0].Role != syncdomain.RoleSystem {
			return FieldUpdate{Capability: CapabilityReadOnly, Reason: "text sources support one system message"}, true
		}
		return updateLeaf(bundle, current, want[0].Content), true
	default:
		return FieldUpdate{}, false
	}
}

// updateLeaf plans the edit of one leaf value.
func updateLeaf(bundle SourceBundle, current *Node, wanted any) FieldUpdate {
	if current.Capability != CapabilityDirect || current.Encode == nil {
		return FieldUpdate{Capability: CapabilityReadOnly, Reason: current.Reason}
	}
	replacement, err := current.Encode(wanted)
	if err != nil {
		return FieldUpdate{Capability: CapabilityReadOnly, Reason: err.Error()}
	}
	document, ok := bundleDocument(bundle, current.File)
	if !ok {
		return FieldUpdate{Capability: CapabilityReadOnly, Reason: "source dependency is not available"}
	}
	return FieldUpdate{Capability: CapabilityDirect, Edits: []TextEdit{{
		File:        document.Path,
		Start:       current.Start,
		End:         current.End,
		Expected:    bytes.Clone(document.Data[current.Start:current.End]),
		Replacement: replacement,
	}}}
}

func updateMessageContents(
	bundle SourceBundle,
	current *Node,
	have, want []syncdomain.Message,
) FieldUpdate {
	if len(have) != len(want) {
		return FieldUpdate{
			Capability: CapabilityReadOnly,
			Reason:     "add or remove messages by hand",
		}
	}
	var edits []TextEdit
	for index := range have {
		if have[index].Role != want[index].Role {
			return FieldUpdate{
				Capability: CapabilityReadOnly,
				Reason:     "reorder messages or change roles by hand",
			}
		}
		if syncdomain.NormalizePromptText(have[index].Content) == syncdomain.NormalizePromptText(want[index].Content) {
			continue
		}
		if index >= len(current.Items) || current.Items[index].Fields["content"] == nil {
			return FieldUpdate{Capability: CapabilityReadOnly, Reason: "message content is not a writable literal"}
		}
		content := current.Items[index].Fields["content"]
		if content.Capability != CapabilityDirect || content.Encode == nil {
			return FieldUpdate{Capability: CapabilityReadOnly, Reason: content.Reason}
		}
		replacement, err := content.Encode(want[index].Content)
		if err != nil {
			return FieldUpdate{Capability: CapabilityReadOnly, Reason: err.Error()}
		}
		document, ok := bundleDocument(bundle, content.File)
		if !ok {
			return FieldUpdate{Capability: CapabilityReadOnly, Reason: "source dependency is not available"}
		}
		edits = append(edits, TextEdit{
			File: document.Path, Start: content.Start, End: content.End,
			Expected: bytes.Clone(document.Data[content.Start:content.End]), Replacement: replacement,
		})
	}
	return FieldUpdate{Capability: CapabilityDirect, Edits: edits}
}

func bundleDocument(bundle SourceBundle, file string) (Document, bool) {
	if file == "" || file == bundle.Primary.Path {
		return bundle.Primary, true
	}
	document, ok := bundle.Dependencies[file]
	return document, ok
}

func semanticEqual(left, right any) bool {
	leftJSON, leftErr := json.Marshal(left)
	rightJSON, rightErr := json.Marshal(right)
	return leftErr == nil && rightErr == nil && bytes.Equal(leftJSON, rightJSON)
}
