package adapter

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
)

// ParseSource reads one mapped resource from current source bytes. A
// variation projection has the mode that the source implies. The planner
// converts it to the mode of the config.
func ParseSource(
	bundle SourceBundle,
	id syncdomain.ResourceID,
	reference syncdomain.Reference,
) (syncdomain.Projection, error) {
	projection, _, err := ParseSourceWithSelections(bundle, id, reference)
	return projection, err
}

// ParseSourceWithSelections reads one mapped resource and the identity of
// each indexed item on its paths.
func ParseSourceWithSelections(
	bundle SourceBundle,
	id syncdomain.ResourceID,
	reference syncdomain.Reference,
) (syncdomain.Projection, []syncdomain.IndexedSelection, error) {
	reference, err := NormalizeReference(id, reference)
	if err != nil {
		return nil, nil, err
	}
	root, err := Parse(bundle, reference.Format)
	if err != nil {
		return nil, nil, annotateProblem(err, id, bundle.Primary.Path)
	}
	selected, selectedIndexes, err := root.resolve(reference.Select, "select")
	if err != nil {
		return nil, nil, annotateProblem(fmt.Errorf("select %q: %w", reference.Select, err), id, bundle.Primary.Path)
	}
	selections, err := indexedSelections(selectedIndexes, selected)
	if err != nil {
		return nil, nil, annotateProblem(err, id, bundle.Primary.Path)
	}
	fields := make([]string, 0, len(reference.Map))
	for field := range reference.Map {
		fields = append(fields, field)
	}
	slices.Sort(fields)
	for _, field := range fields {
		_, indexes, err := selected.resolve(reference.Map[field], "map."+field)
		if err != nil {
			return nil, nil, annotateProblem(fmt.Errorf("map field %q: %w", field, err), id, bundle.Primary.Path)
		}
		mappedSelections, err := indexedSelections(indexes, selected)
		if err != nil {
			return nil, nil, annotateProblem(err, id, bundle.Primary.Path)
		}
		selections = append(selections, mappedSelections...)
	}
	slices.SortFunc(selections, func(left, right syncdomain.IndexedSelection) int {
		return strings.Compare(left.Path, right.Path)
	})
	projection, err := buildProjection(id.Kind, selected, reference.Map)
	if variation, ok := projection.(syncdomain.VariationProjection); ok {
		if reference.Tools != nil {
			keys := attachmentKeys(*reference.Tools)
			variation.Tools = &keys
			variation.FieldStates[syncdomain.FieldTools] = syncdomain.FieldState{
				Capability: syncdomain.CapabilityDirect, Known: true,
			}
		}
		if reference.Skills != nil {
			keys := attachmentKeys(*reference.Skills)
			variation.Skills = &keys
			variation.FieldStates[syncdomain.FieldSkills] = syncdomain.FieldState{
				Capability: syncdomain.CapabilityDirect, Known: true,
			}
		}
		projection = variation
	}
	return projection, selections, annotateProblem(err, id, bundle.Primary.Path)
}

// buildProjection reads each mapped field below the selected node into the
// typed projection of the kind.
func buildProjection(
	kind syncdomain.Kind,
	selected *Node,
	fieldMap map[string]string,
) (syncdomain.Projection, error) {
	switch kind {
	case syncdomain.KindVariation:
		projection := syncdomain.VariationProjection{
			Mode: syncdomain.VariationModeAgent, FieldStates: make(map[syncdomain.Field]syncdomain.FieldState),
		}
		for name, path := range fieldMap {
			node, err := mappedNode(selected, name, path)
			if err != nil {
				return nil, err
			}
			field := syncdomain.Field(name)
			projection.FieldStates[field] = syncdomain.FieldState{
				Capability: node.Capability, Known: node.Value != nil, Reason: node.Reason, Code: node.Code, File: node.File,
			}
			parameterKey, parameterField := syncdomain.ModelParameterKey(field)
			if parameterField {
				projection.FieldStates[field] = syncdomain.FieldState{
					Capability: node.Capability, Known: true, Reason: node.Reason, Code: node.Code, File: node.File,
				}
				value, ok := jsonValue(node.Value)
				if !ok {
					return nil, TypeMismatch(name, "a JSON-compatible value")
				}
				if projection.Parameters == nil {
					parameters := map[string]any{}
					projection.Parameters = &parameters
				}
				(*projection.Parameters)[parameterKey] = value
				continue
			}
			if field == syncdomain.FieldModelParameters && node.Value == nil {
				return nil, TypeMismatch(name, "an object")
			}
			if node.Value == nil {
				continue
			}
			switch field {
			case syncdomain.FieldPrompt, syncdomain.FieldMessages:
				switch value := node.Value.(type) {
				case string:
					projection.Instructions = &value
				case []syncdomain.Message:
					for _, message := range value {
						if !syncdomain.ValidMessageRole(message.Role) {
							return nil, syncdomain.NewProblem(
								syncdomain.ErrorMapTypeMismatch,
								fmt.Sprintf("message role %q is not supported", message.Role),
								"Use system, user, or assistant.",
							)
						}
					}
					projection.Mode = syncdomain.VariationModeCompletion
					projection.Messages = &value
				default:
					return nil, TypeMismatch(name, "a string or a message list")
				}
			case syncdomain.FieldName:
				value, ok := node.Value.(string)
				if !ok {
					return nil, TypeMismatch(name, "a string")
				}
				projection.Name = &value
			case syncdomain.FieldModel:
				value, ok := node.Value.(string)
				if !ok {
					return nil, TypeMismatch(name, "a string")
				}
				projection.Model = &value
			case syncdomain.FieldModelParameters:
				value, ok := objectValue(node.Value)
				if !ok {
					return nil, TypeMismatch(name, "an object")
				}
				value = cloneObject(value)
				delete(value, "tools")
				projection.Parameters = &value
			}
		}
		_, promptMapped := projection.FieldStates[syncdomain.FieldPrompt]
		_, messagesMapped := projection.FieldStates[syncdomain.FieldMessages]
		if !promptMapped && !messagesMapped {
			return nil, fmt.Errorf("map field %q is required for a variation", syncdomain.FieldPrompt)
		}
		return projection, nil
	case syncdomain.KindTool:
		projection := syncdomain.ToolProjection{FieldStates: make(map[syncdomain.Field]syncdomain.FieldState)}
		for name, path := range fieldMap {
			node, err := mappedNode(selected, name, path)
			if err != nil {
				return nil, err
			}
			field := syncdomain.Field(name)
			projection.FieldStates[field] = syncdomain.FieldState{
				Capability: node.Capability, Known: node.Value != nil, Reason: node.Reason, Code: node.Code, File: node.File,
			}
			if node.Value == nil {
				continue
			}
			switch field {
			case syncdomain.FieldDescription:
				value, ok := node.Value.(string)
				if !ok {
					return nil, TypeMismatch(name, "a string")
				}
				projection.Description = &value
			case syncdomain.FieldSchema:
				value, ok := objectValue(node.Value)
				if !ok {
					return nil, TypeMismatch(name, "an object")
				}
				projection.Schema = &value
			case syncdomain.FieldCustomParameters:
				value, ok := objectValue(node.Value)
				if !ok {
					return nil, TypeMismatch(name, "an object")
				}
				projection.CustomParameters = &value
			case syncdomain.FieldTags:
				value, ok := stringList(node.Value)
				if !ok {
					return nil, TypeMismatch(name, "a string collection")
				}
				projection.Tags = &value
			}
		}
		return projection, nil
	case syncdomain.KindSkill:
		projection := syncdomain.SkillProjection{FieldStates: make(map[syncdomain.Field]syncdomain.FieldState)}
		for name, path := range fieldMap {
			node, err := mappedNode(selected, name, path)
			if err != nil {
				return nil, err
			}
			field := syncdomain.Field(name)
			projection.FieldStates[field] = syncdomain.FieldState{
				Capability: node.Capability, Known: node.Value != nil, Reason: node.Reason, Code: node.Code, File: node.File,
			}
			if node.Value == nil {
				continue
			}
			value, ok := node.Value.(string)
			if !ok {
				return nil, TypeMismatch(name, "a string")
			}
			if field == syncdomain.FieldDescription {
				projection.Description = &value
			} else {
				projection.Markdown = &value
			}
		}
		return projection, nil
	default:
		return nil, fmt.Errorf("unsupported resource kind %q", kind)
	}
}

func mappedNode(root *Node, name, path string) (*Node, error) {
	node, err := root.Resolve(path)
	if err != nil {
		return nil, fmt.Errorf("map field %q: %w", name, err)
	}
	return node, nil
}

func attachmentKeys(refs []syncdomain.AttachmentRef) []string {
	keys := make([]string, len(refs))
	for index := range refs {
		keys[index] = refs[index].Key
	}
	slices.Sort(keys)
	return keys
}

func jsonValue(value any) (any, bool) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, false
	}
	var decoded any
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		return nil, false
	}
	return decoded, true
}

func objectValue(value any) (map[string]any, bool) {
	object, ok := value.(map[string]any)
	if !ok {
		return nil, false
	}
	return object, true
}

func cloneObject(value map[string]any) map[string]any {
	cloned, ok := jsonValue(value)
	if !ok {
		return map[string]any{}
	}
	object, ok := cloned.(map[string]any)
	if !ok {
		return map[string]any{}
	}
	return object
}

func stringList(value any) ([]string, bool) {
	switch values := value.(type) {
	case []string:
		return slices.Clone(values), true
	case []any:
		result := make([]string, len(values))
		for index, value := range values {
			text, ok := value.(string)
			if !ok {
				return nil, false
			}
			result[index] = text
		}
		return result, true
	default:
		return nil, false
	}
}
