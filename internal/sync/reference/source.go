package reference

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strings"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
)

// Capability is the shared mapped-field capability.
type Capability = syncdomain.Capability

const (
	CapabilityDirect   = syncdomain.CapabilityDirect
	CapabilityPatch    = syncdomain.CapabilityPatch
	CapabilityReadOnly = syncdomain.CapabilityReadOnly
	CapabilityReject   = syncdomain.CapabilityReject
)

// Document is one repository file supplied to an adapter.
type Document struct {
	Path string
	Data []byte
}

// SourceBundle contains a primary source and optional repository dependencies.
type SourceBundle struct {
	Primary      Document
	Dependencies map[string]Document
}

// TextEdit replaces one exact byte range.
type TextEdit struct {
	File        string
	Start       int
	End         int
	Expected    []byte
	Replacement []byte
}

// FieldUpdate is the update capability and edits for one mapped field.
type FieldUpdate struct {
	Capability Capability
	Edits      []TextEdit
	Reason     string
}

// UpdateResult contains one result for each changed field.
type UpdateResult struct {
	Fields map[syncdomain.Field]FieldUpdate
}

// NormalizeReference validates a descriptor and fills conventional defaults.
func NormalizeReference(id syncdomain.ResourceID, reference syncdomain.Reference) (syncdomain.Reference, error) {
	if reference.File == "" {
		return syncdomain.Reference{}, invalidReference(id, "file is required", "Pass --file.")
	}
	adapter, err := registeredAdapter(reference.Format)
	if err != nil {
		return syncdomain.Reference{}, err
	}
	defaultSelect, defaultMap := adapter.Defaults(id.Kind)
	if reference.Select == "" {
		if defaultSelect == "" {
			return syncdomain.Reference{}, invalidReference(
				id, fmt.Sprintf("select is required for format %q", reference.Format), "Pass --select.",
			)
		}
		reference.Select = defaultSelect
	}
	if err := ParsePath(reference.Select); err != nil {
		return syncdomain.Reference{}, annotateProblem(
			fmt.Errorf("select %q: %w", reference.Select, err), id, reference.File,
		)
	}
	if len(reference.Map) == 0 {
		if len(defaultMap) == 0 {
			return syncdomain.Reference{}, invalidReference(
				id, "map is required", "Pass at least one --map field=path value.",
			)
		}
		reference.Map = defaultMap
	}
	for target := range reference.Map {
		if err := ParsePath(target); err != nil {
			return syncdomain.Reference{}, annotateProblem(
				fmt.Errorf("map target %q: %w", target, err), id, reference.File,
			)
		}
	}
	fields, err := syncdomain.ProjectionFields(reference)
	if err != nil {
		return syncdomain.Reference{}, invalidReference(id, err.Error(), "Use a field valid for this resource kind.")
	}
	wholeParameters := false
	parameterKeys := map[string]syncdomain.Field{}
	for _, field := range fields {
		if !fieldAllowed(id.Kind, field) {
			return syncdomain.Reference{}, invalidReference(
				id,
				fmt.Sprintf("field %q is not valid for %s", field, id.Kind),
				"Use a mapped field supported by this resource kind.",
			)
		}
		if field == syncdomain.FieldModelParameters {
			wholeParameters = true
		}
		if key, parameter := syncdomain.ModelParameterKey(field); parameter {
			if key == "tools" {
				return syncdomain.Reference{}, invalidReference(
					id,
					fmt.Sprintf("map target %q duplicates the tools field", field),
					"Map tools with the tools target.",
				)
			}
			if previous, duplicate := parameterKeys[key]; duplicate {
				return syncdomain.Reference{}, invalidReference(
					id,
					fmt.Sprintf("map targets %q and %q select the same model parameter", previous, field),
					"Map each model parameter key once.",
				)
			}
			parameterKeys[key] = field
		}
		if err := ParsePath(reference.Map[string(field)]); err != nil {
			return syncdomain.Reference{}, annotateProblem(
				fmt.Errorf("map field %q: %w", field, err), id, reference.File,
			)
		}
	}
	if wholeParameters && len(parameterKeys) != 0 {
		return syncdomain.Reference{}, invalidReference(
			id,
			"map target \"model.parameters\" overlaps an individual model parameter target",
			"Map the full parameters object or individual parameter keys.",
		)
	}
	return reference, nil
}

func invalidReference(id syncdomain.ResourceID, message, suggestion string) error {
	return syncdomain.NewProblem(
		syncdomain.ErrorInvalidReference, message, suggestion,
	).WithResource(id)
}

// ParseSource reads one mapped resource from current source bytes.
func ParseSource(
	bundle SourceBundle,
	id syncdomain.ResourceID,
	mode syncdomain.VariationMode,
	reference syncdomain.Reference,
) (syncdomain.Projection, error) {
	projection, _, err := ParseSourceWithSelections(bundle, id, mode, reference)
	return projection, err
}

// ParseSourceWithSelections reads one mapped resource and its indexed selections.
func ParseSourceWithSelections(
	bundle SourceBundle,
	id syncdomain.ResourceID,
	mode syncdomain.VariationMode,
	reference syncdomain.Reference,
) (syncdomain.Projection, []syncdomain.IndexedSelection, error) {
	reference, err := NormalizeReference(id, reference)
	if err != nil {
		return nil, nil, err
	}
	root, err := parseSourceBundle(bundle, reference.Format)
	if err != nil {
		return nil, nil, annotateProblem(err, id, bundle.Primary.Path)
	}
	selected, selectedIndexes, err := resolveSourcePathTracked(root, reference.Select, "select")
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
		_, indexes, err := resolveSourcePathTracked(selected, reference.Map[field], "map."+field)
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
	projection, err := buildProjection(id.Kind, mode, selected, reference.Map)
	return projection, selections, annotateProblem(err, id, bundle.Primary.Path)
}

// UpdateSource plans exact edits for changed mapped fields.
func UpdateSource(
	bundle SourceBundle,
	id syncdomain.ResourceID,
	mode syncdomain.VariationMode,
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
	root, err := parseSourceBundle(bundle, reference.Format)
	if err != nil {
		return UpdateResult{}, annotateProblem(err, id, bundle.Primary.Path)
	}
	selected, err := resolveSourcePath(root, reference.Select)
	if err != nil {
		return UpdateResult{}, annotateProblem(fmt.Errorf("select %q: %w", reference.Select, err), id, bundle.Primary.Path)
	}

	result := UpdateResult{Fields: make(map[syncdomain.Field]FieldUpdate)}
	for name, sourcePath := range reference.Map {
		field := syncdomain.Field(name)
		wanted, present, err := projectionField(desired, field)
		if err != nil {
			return UpdateResult{}, err
		}
		if !present {
			continue
		}
		current, err := resolveSourcePath(selected, sourcePath)
		if err != nil {
			return UpdateResult{}, fmt.Errorf("%s: map field %q: %w", bundle.Primary.Path, field, err)
		}
		if field == syncdomain.FieldPrompt || field == syncdomain.FieldMessages {
			if messages, ok := wanted.([]syncdomain.Message); ok {
				if currentMessages, sourceIsMessages := current.value.([]syncdomain.Message); sourceIsMessages {
					update := updateMessageContents(bundle, current, currentMessages, messages)
					if len(update.Edits) != 0 || update.Capability != CapabilityDirect {
						result.Fields[field] = update
					}
					continue
				}
				if _, sourceIsText := current.value.(string); sourceIsText {
					if len(messages) != 1 || messages[0].Role != syncdomain.RoleSystem {
						result.Fields[field] = FieldUpdate{
							Capability: CapabilityReadOnly,
							Reason:     "text sources support one system message",
						}
						continue
					}
					wanted = messages[0].Content
				}
			}
		}
		if semanticEqual(current.value, wanted) {
			continue
		}
		update := FieldUpdate{Capability: current.capability, Reason: current.reason}
		if current.capability == CapabilityDirect || current.capability == CapabilityPatch {
			replacement, err := current.encode(wanted)
			if err != nil {
				update.Capability, update.Reason = CapabilityReadOnly, err.Error()
			} else {
				document, ok := bundleDocument(bundle, current.file)
				if !ok {
					update.Capability, update.Reason = CapabilityReadOnly, "source dependency is not available"
					result.Fields[field] = update
					continue
				}
				update.Edits = []TextEdit{{
					File:        document.Path,
					Start:       current.start,
					End:         current.end,
					Expected:    bytes.Clone(document.Data[current.start:current.end]),
					Replacement: replacement,
				}}
			}
		}
		result.Fields[field] = update
	}
	return result, nil
}

func updateMessageContents(
	bundle SourceBundle,
	current *sourceNode,
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
		if have[index].Content == want[index].Content {
			continue
		}
		if index >= len(current.items) || current.items[index].fields["content"] == nil {
			return FieldUpdate{Capability: CapabilityReadOnly, Reason: "message content is not a writable literal"}
		}
		content := current.items[index].fields["content"]
		if content.capability != CapabilityDirect || content.encode == nil {
			return FieldUpdate{Capability: CapabilityReadOnly, Reason: content.reason}
		}
		replacement, err := content.encode(want[index].Content)
		if err != nil {
			return FieldUpdate{Capability: CapabilityReadOnly, Reason: err.Error()}
		}
		document, ok := bundleDocument(bundle, content.file)
		if !ok {
			return FieldUpdate{Capability: CapabilityReadOnly, Reason: "source dependency is not available"}
		}
		edits = append(edits, TextEdit{
			File: document.Path, Start: content.start, End: content.end,
			Expected: bytes.Clone(document.Data[content.start:content.end]), Replacement: replacement,
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

func annotateProblem(err error, id syncdomain.ResourceID, file string) error {
	if err == nil {
		return nil
	}
	if problem, ok := syncdomain.ProblemFrom(err); ok {
		_ = problem.WithResource(id).WithFile(file, 0, 0)
	}
	return err
}

type sourceNode struct {
	value      any
	fields     map[string]*sourceNode
	items      []*sourceNode
	collection bool
	start      int
	end        int
	file       string
	capability Capability
	reason     string
	code       syncdomain.ErrorCode
	encode     func(any) ([]byte, error)
}

func resolveSourcePath(root *sourceNode, text string) (*sourceNode, error) {
	node, _, err := resolveSourcePathTracked(root, text, "")
	return node, err
}

type indexedResolution struct {
	path             string
	collectionLength int
	item             *sourceNode
}

func resolveSourcePathTracked(root *sourceNode, text, scope string) (*sourceNode, []indexedResolution, error) {
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
			if !sourceCollection(current) {
				return nil, nil, syncdomain.NewProblem(
					syncdomain.ErrorPathTypeMismatch,
					fmt.Sprintf("index %d needs a collection", *step.index),
					"Select a collection before this index.",
				)
			}
			if *step.index >= len(current.items) {
				return nil, nil, syncdomain.NewProblem(
					syncdomain.ErrorIndexOutOfRange,
					fmt.Sprintf("index %d is out of range for collection of length %d", *step.index, len(current.items)),
					"Use an index within the collection.",
				)
			}
			collectionLength := len(current.items)
			current = current.items[*step.index]
			indexes = append(indexes, indexedResolution{
				path:             fmt.Sprintf("%s#%d", scope, len(indexes)),
				collectionLength: collectionLength,
				item:             current,
			})
			continue
		}
		if current.fields == nil {
			return nil, nil, syncdomain.NewProblem(
				syncdomain.ErrorPathTypeMismatch,
				fmt.Sprintf("%q does not contain fields", step.key),
				"Select an object before this field.",
			)
		}
		next, ok := current.fields[step.key]
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
		var matches []*sourceNode
		for _, item := range current.items {
			matched := true
			for _, filter := range step.filters {
				identity, ok := item.fields[filter.key]
				if !ok {
					matched = false
					break
				}
				text, ok := identity.value.(string)
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

func sourceCollection(node *sourceNode) bool {
	if node == nil {
		return false
	}
	if node.collection || node.items != nil {
		return true
	}
	value := reflect.ValueOf(node.value)
	return value.IsValid() && (value.Kind() == reflect.Array || value.Kind() == reflect.Slice)
}

func indexedSelections(indexes []indexedResolution, selectedRoot *sourceNode) ([]syncdomain.IndexedSelection, error) {
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
				Root:      stableNodeValue(selectedRoot, map[*sourceNode]bool{}),
				Ancestors: slices.Clone(ancestors),
				Item:      stableNodeValue(index.item, map[*sourceNode]bool{}),
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

func directSelectionIdentity(node *sourceNode) any {
	if node == nil || node.fields == nil {
		return nil
	}
	identity := map[string]any{}
	for _, key := range []string{"id", "name", "role", "type"} {
		field, ok := node.fields[key]
		if !ok || !scalarValue(field.value) {
			continue
		}
		identity[key] = field.value
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

func stableNodeValue(node *sourceNode, visited map[*sourceNode]bool) any {
	if node == nil {
		return nil
	}
	if node.value != nil {
		return node.value
	}
	if visited[node] {
		return "<cycle>"
	}
	visited[node] = true
	defer delete(visited, node)
	if node.fields != nil {
		value := make(map[string]any, len(node.fields))
		for key, field := range node.fields {
			value[key] = stableNodeValue(field, visited)
		}
		return value
	}
	if sourceCollection(node) {
		value := make([]any, len(node.items))
		for index, item := range node.items {
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

func buildProjection(
	kind syncdomain.Kind,
	mode syncdomain.VariationMode,
	selected *sourceNode,
	fieldMap map[string]string,
) (syncdomain.Projection, error) {
	switch kind {
	case syncdomain.KindVariation:
		projection := syncdomain.VariationProjection{
			Mode: mode, FieldStates: make(map[syncdomain.Field]syncdomain.FieldState),
		}
		if !projection.Mode.Valid() {
			projection.Mode = syncdomain.VariationModeAgent
		}
		for name, path := range fieldMap {
			node, err := mappedNode(selected, name, path)
			if err != nil {
				return nil, err
			}
			field := syncdomain.Field(name)
			projection.FieldStates[field] = syncdomain.FieldState{
				Capability: node.capability, Known: node.value != nil, Reason: node.reason, Code: node.code, File: node.file,
			}
			parameterKey, parameterField := syncdomain.ModelParameterKey(field)
			if parameterField {
				projection.FieldStates[field] = syncdomain.FieldState{
					Capability: node.capability, Known: true, Reason: node.reason, Code: node.code, File: node.file,
				}
				value, ok := jsonValue(node.value)
				if !ok {
					return nil, mapTypeError(name, "a JSON-compatible value")
				}
				if projection.Parameters == nil {
					parameters := map[string]any{}
					projection.Parameters = &parameters
				}
				(*projection.Parameters)[parameterKey] = value
				continue
			}
			if field == syncdomain.FieldModelParameters && node.value == nil {
				return nil, mapTypeError(name, "an object")
			}
			if node.value == nil {
				continue
			}
			switch field {
			case syncdomain.FieldPrompt, syncdomain.FieldMessages:
				if field == syncdomain.FieldMessages || projection.Mode == syncdomain.VariationModeCompletion {
					switch value := node.value.(type) {
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
					case string:
						messages := []syncdomain.Message{{Role: syncdomain.RoleSystem, Content: value}}
						projection.Messages = &messages
					default:
						return nil, mapTypeError(name, "messages")
					}
				} else {
					value, ok := node.value.(string)
					if !ok {
						return nil, mapTypeError(name, "a string")
					}
					projection.Instructions = &value
				}
			case syncdomain.FieldName:
				value, ok := node.value.(string)
				if !ok {
					return nil, mapTypeError(name, "a string")
				}
				projection.Name = &value
			case syncdomain.FieldModel:
				value, ok := node.value.(string)
				if !ok {
					return nil, mapTypeError(name, "a string")
				}
				projection.Model = &value
			case syncdomain.FieldModelParameters:
				value, ok := objectValue(node.value)
				if !ok {
					return nil, mapTypeError(name, "an object")
				}
				value = cloneObject(value)
				delete(value, "tools")
				projection.Parameters = &value
			case syncdomain.FieldTools:
				value, ok := stringList(node.value)
				if !ok {
					return nil, mapTypeError(name, "a string collection")
				}
				projection.Tools = &value
			case syncdomain.FieldSkills:
				value, ok := stringList(node.value)
				if !ok {
					return nil, mapTypeError(name, "a string collection")
				}
				projection.Skills = &value
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
				Capability: node.capability, Known: node.value != nil, Reason: node.reason, Code: node.code, File: node.file,
			}
			if node.value == nil {
				continue
			}
			switch field {
			case syncdomain.FieldDescription:
				value, ok := node.value.(string)
				if !ok {
					return nil, mapTypeError(name, "a string")
				}
				projection.Description = &value
			case syncdomain.FieldSchema:
				value, ok := objectValue(node.value)
				if !ok {
					return nil, mapTypeError(name, "an object")
				}
				projection.Schema = &value
			case syncdomain.FieldCustomParameters:
				value, ok := objectValue(node.value)
				if !ok {
					return nil, mapTypeError(name, "an object")
				}
				projection.CustomParameters = &value
			case syncdomain.FieldTags:
				value, ok := stringList(node.value)
				if !ok {
					return nil, mapTypeError(name, "a string collection")
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
				Capability: node.capability, Known: node.value != nil, Reason: node.reason, Code: node.code, File: node.file,
			}
			if node.value == nil {
				continue
			}
			value, ok := node.value.(string)
			if !ok {
				return nil, mapTypeError(name, "a string")
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

func mappedNode(root *sourceNode, name, path string) (*sourceNode, error) {
	node, err := resolveSourcePath(root, path)
	if err != nil {
		return nil, fmt.Errorf("map field %q: %w", name, err)
	}
	return node, nil
}

func projectionField(projection syncdomain.Projection, field syncdomain.Field) (any, bool, error) {
	switch value := projection.(type) {
	case syncdomain.VariationProjection:
		switch field {
		case syncdomain.FieldPrompt, syncdomain.FieldMessages:
			if value.Instructions != nil {
				return *value.Instructions, true, nil
			}
			if value.Messages != nil {
				return *value.Messages, true, nil
			}
		case syncdomain.FieldName:
			if value.Name != nil {
				return *value.Name, true, nil
			}
		case syncdomain.FieldModel:
			if value.Model != nil {
				return *value.Model, true, nil
			}
		case syncdomain.FieldModelParameters:
			if value.Parameters != nil {
				return cloneObject(*value.Parameters), true, nil
			}
		case syncdomain.FieldTools:
			if value.Tools != nil {
				return *value.Tools, true, nil
			}
		case syncdomain.FieldSkills:
			if value.Skills != nil {
				return *value.Skills, true, nil
			}
		default:
			if key, parameter := syncdomain.ModelParameterKey(field); parameter && value.Parameters != nil {
				item, present := (*value.Parameters)[key]
				return item, present, nil
			}
		}
	case syncdomain.ToolProjection:
		switch field {
		case syncdomain.FieldDescription:
			if value.Description != nil {
				return *value.Description, true, nil
			}
		case syncdomain.FieldSchema:
			if value.Schema != nil {
				return *value.Schema, true, nil
			}
		case syncdomain.FieldCustomParameters:
			if value.CustomParameters != nil {
				return *value.CustomParameters, true, nil
			}
		case syncdomain.FieldTags:
			if value.Tags != nil {
				return *value.Tags, true, nil
			}
		}
	case syncdomain.SkillProjection:
		if field == syncdomain.FieldDescription && value.Description != nil {
			return *value.Description, true, nil
		}
		if field == syncdomain.FieldMarkdown && value.Markdown != nil {
			return *value.Markdown, true, nil
		}
	default:
		return nil, false, fmt.Errorf("unsupported projection %T", projection)
	}
	return nil, false, nil
}

func parseSourceBundle(bundle SourceBundle, format string) (*sourceNode, error) {
	adapter, err := registeredAdapter(format)
	if err != nil {
		return nil, err
	}
	inspection, err := adapter.Inspect(bundle)
	if err != nil {
		if _, ok := syncdomain.ProblemFrom(err); !ok {
			err = syncdomain.WrapProblem(syncdomain.NewProblem(
				syncdomain.ErrorInvalidSource,
				err.Error(),
				"Fix the source syntax or use a supported static form.",
			).WithFile(bundle.Primary.Path, 0, 0), err)
		}
	}
	markSourceFile(inspection.root, bundle.Primary.Path)
	return inspection.root, err
}

func markSourceFile(node *sourceNode, file string) {
	markSourceFileOnce(node, file, map[*sourceNode]struct{}{})
}

func markSourceFileOnce(node *sourceNode, file string, visited map[*sourceNode]struct{}) {
	if node == nil {
		return
	}
	if _, seen := visited[node]; seen {
		return
	}
	visited[node] = struct{}{}
	if node.file == "" {
		node.file = file
	}
	for _, child := range node.fields {
		markSourceFileOnce(child, file, visited)
	}
	for _, child := range node.items {
		markSourceFileOnce(child, file, visited)
	}
}

func fieldAllowed(kind syncdomain.Kind, field syncdomain.Field) bool {
	switch kind {
	case syncdomain.KindVariation:
		if _, parameter := syncdomain.ModelParameterKey(field); parameter {
			return true
		}
		return slices.Contains([]syncdomain.Field{
			syncdomain.FieldPrompt, syncdomain.FieldMessages, syncdomain.FieldName, syncdomain.FieldModel,
			syncdomain.FieldModelParameters, syncdomain.FieldTools, syncdomain.FieldSkills,
		}, field)
	case syncdomain.KindTool:
		return slices.Contains([]syncdomain.Field{
			syncdomain.FieldDescription, syncdomain.FieldSchema,
			syncdomain.FieldCustomParameters, syncdomain.FieldTags,
		}, field)
	case syncdomain.KindSkill:
		return field == syncdomain.FieldDescription || field == syncdomain.FieldMarkdown
	default:
		return false
	}
}

func semanticEqual(left, right any) bool {
	leftJSON, leftErr := json.Marshal(left)
	rightJSON, rightErr := json.Marshal(right)
	return leftErr == nil && rightErr == nil && bytes.Equal(leftJSON, rightJSON)
}

func mapTypeError(field, expected string) error {
	return syncdomain.NewProblem(
		syncdomain.ErrorMapTypeMismatch,
		fmt.Sprintf("mapped field %q needs %s", field, expected),
		"Map the field to a compatible static value.",
	)
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

func stringNode(value string, start, end int, encode func(string) ([]byte, error)) *sourceNode {
	return &sourceNode{
		value: value, start: start, end: end, capability: CapabilityDirect,
		encode: func(value any) ([]byte, error) {
			text, ok := value.(string)
			if !ok {
				return nil, mapTypeError("source", "a string")
			}
			return encode(text)
		},
	}
}

func normalizeMarkdown(text string) string {
	return strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\r", "\n"))
}
