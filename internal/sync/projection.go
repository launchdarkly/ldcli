package sync

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"
)

const projectionFingerprintSchema = "launchdarkly.config.projection/1"

// ProjectionFingerprintSchema is the schema of descriptor-backed fingerprints.
const ProjectionFingerprintSchema = projectionFingerprintSchema

// AdapterBehaviorVersion changes when adapter behavior changes mapped values.
const AdapterBehaviorVersion = 2

// AdapterBehaviorDigest identifies the parser, path, codec, and capability rules.
const AdapterBehaviorDigest = "sha256:e0c3bdc3ed2886e480b73a079f1e3ef3cd48c99015b4624d3bb044cecb8de2bf"

// Field identifies one LaunchDarkly value that a repository source owns.
type Field string

const (
	FieldPrompt           Field = "prompt"
	FieldMessages         Field = "messages"
	FieldName             Field = "name"
	FieldModel            Field = "model"
	FieldModelParameters  Field = "model.parameters"
	FieldTools            Field = "tools"
	FieldSkills           Field = "skills"
	FieldDescription      Field = "description"
	FieldSchema           Field = "schema"
	FieldCustomParameters Field = "customParameters"
	FieldTags             Field = "tags"
	FieldMarkdown         Field = "markdown"
)

// Capability describes how one mapped field can change.
type Capability string

const (
	CapabilityDirect   Capability = "direct"
	CapabilityPatch    Capability = "patch"
	CapabilityReadOnly Capability = "read-only"
	CapabilityReject   Capability = "reject"
)

// FieldState records the value availability and write capability of one field.
type FieldState struct {
	Capability Capability
	Known      bool
	Reason     string
	Code       ErrorCode
	File       string
}

// Projection is the mapped content of one repository source.
type Projection interface {
	Kind() Kind
	projection()
}

// VariationProjection contains only the mapped fields of one variation.
type VariationProjection struct {
	Mode         VariationMode        `json:"mode"`
	Instructions *string              `json:"instructions,omitempty"`
	Messages     *[]Message           `json:"messages,omitempty"`
	Name         *string              `json:"name,omitempty"`
	Model        *string              `json:"model,omitempty"`
	Parameters   *map[string]any      `json:"parameters,omitempty"`
	Tools        *[]string            `json:"tools,omitempty"`
	Skills       *[]string            `json:"skills,omitempty"`
	FieldStates  map[Field]FieldState `json:"-"`
}

func (VariationProjection) Kind() Kind  { return KindVariation }
func (VariationProjection) projection() {}

// ToolProjection contains only the mapped fields of one tool.
type ToolProjection struct {
	Description      *string              `json:"description,omitempty"`
	Schema           *map[string]any      `json:"schema,omitempty"`
	CustomParameters *map[string]any      `json:"customParameters,omitempty"`
	Tags             *[]string            `json:"tags,omitempty"`
	FieldStates      map[Field]FieldState `json:"-"`
}

func (ToolProjection) Kind() Kind  { return KindTool }
func (ToolProjection) projection() {}

// SkillProjection contains only the mapped fields of one skill.
type SkillProjection struct {
	Description *string              `json:"description,omitempty"`
	Markdown    *string              `json:"markdown,omitempty"`
	FieldStates map[Field]FieldState `json:"-"`
}

func (SkillProjection) Kind() Kind  { return KindSkill }
func (SkillProjection) projection() {}

// ProjectionFields returns the fields that a reference maps.
func ProjectionFields(reference Reference) ([]Field, error) {
	fields := make([]Field, 0, len(reference.Map))
	for name := range reference.Map {
		field := Field(name)
		if !field.valid() {
			return nil, fmt.Errorf("unsupported mapped field %q", name)
		}
		fields = append(fields, field)
	}
	slices.Sort(fields)
	return fields, nil
}

func (field Field) valid() bool {
	switch field {
	case FieldPrompt, FieldMessages, FieldName, FieldModel, FieldModelParameters, FieldTools, FieldSkills,
		FieldDescription, FieldSchema, FieldCustomParameters, FieldTags, FieldMarkdown:
		return true
	default:
		_, ok := ModelParameterKey(field)
		return ok
	}
}

// ModelParameterKey returns the exact key from a model parameter field.
func ModelParameterKey(field Field) (string, bool) {
	const prefix = `model.parameters[`
	text := string(field)
	if !strings.HasPrefix(text, prefix) || !strings.HasSuffix(text, "]") {
		return "", false
	}
	quoted := text[len(prefix) : len(text)-1]
	if len(quoted) < 2 || quoted[0] != '"' {
		return "", false
	}
	var key string
	if err := json.Unmarshal([]byte(quoted), &key); err != nil {
		return "", false
	}
	return key, true
}

// ProjectVariation selects the mapped fields from a variation.
func ProjectVariation(variation Variation, reference Reference) (VariationProjection, error) {
	fields, err := ProjectionFields(reference)
	if err != nil {
		return VariationProjection{}, err
	}
	projection := VariationProjection{Mode: variation.Mode, FieldStates: make(map[Field]FieldState)}
	for _, field := range fields {
		projection.FieldStates[field] = FieldState{Capability: CapabilityDirect, Known: true}
		switch field {
		case FieldPrompt, FieldMessages:
			if variation.Mode == VariationModeCompletion {
				messages := slices.Clone(variation.Messages)
				projection.Messages = &messages
			} else {
				value := variation.Instructions
				projection.Instructions = &value
			}
		case FieldName:
			value := variation.Name
			projection.Name = &value
		case FieldModel:
			if value, ok := variation.Model["modelName"].(string); ok {
				projection.Model = &value
			}
		case FieldModelParameters:
			parameters := canonicalModelParameters(variation)
			projection.Parameters = &parameters
		case FieldTools:
			values := referenceKeys(variation.Tools)
			projection.Tools = &values
		case FieldSkills:
			values := referenceKeys(variation.Skills)
			projection.Skills = &values
		default:
			key, ok := ModelParameterKey(field)
			if !ok {
				return VariationProjection{}, fmt.Errorf("field %q is not valid for a variation", field)
			}
			parameters := canonicalModelParameters(variation)
			if parameter, present := parameters[key]; present {
				if projection.Parameters == nil {
					value := map[string]any{}
					projection.Parameters = &value
				}
				(*projection.Parameters)[key] = cloneJSONValue(parameter)
			}
		}
	}
	return projection, nil
}

// ApplyVariationProjection merges mapped fields into a variation.
func ApplyVariationProjection(variation Variation, projection VariationProjection) Variation {
	if projection.Mode.Valid() {
		variation.Mode = projection.Mode
	}
	if projection.Instructions != nil {
		variation.Instructions, variation.Messages = *projection.Instructions, nil
	}
	if projection.Messages != nil {
		variation.Instructions, variation.Messages = "", slices.Clone(*projection.Messages)
	}
	if projection.Name != nil {
		variation.Name = *projection.Name
	}
	if projection.Model != nil {
		variation.Model = cloneMap(variation.Model)
		variation.Model["modelName"] = *projection.Model
	}
	if projection.Parameters != nil {
		variation.Model = cloneMap(variation.Model)
		if _, whole := projection.FieldStates[FieldModelParameters]; whole {
			variation.Model["parameters"] = cloneMap(*projection.Parameters)
		} else {
			parameters, _ := variation.Model["parameters"].(map[string]any)
			parameters = cloneMap(parameters)
			for _, field := range projectionStateFields(projection) {
				key, parameter := ModelParameterKey(field)
				if !parameter {
					continue
				}
				if value, present := (*projection.Parameters)[key]; present {
					parameters[key] = cloneJSONValue(value)
				}
			}
			variation.Model["parameters"] = parameters
		}
	}
	if projection.Tools != nil {
		variation.Tools = attachmentReferences(*projection.Tools)
	}
	if projection.Skills != nil {
		variation.Skills = attachmentReferences(*projection.Skills)
	}
	variation.Model = CanonicalModel(variation.Model, variation.Tools)
	return variation
}

// ProjectAttachment selects the mapped fields from a tool or skill.
func ProjectAttachment(attachment Attachment, reference Reference) (Projection, error) {
	fields, err := ProjectionFields(reference)
	if err != nil {
		return nil, err
	}
	switch {
	case attachment.Tool != nil:
		projection := ToolProjection{FieldStates: make(map[Field]FieldState)}
		for _, field := range fields {
			projection.FieldStates[field] = FieldState{Capability: CapabilityDirect, Known: true}
			switch field {
			case FieldDescription:
				projection.Description = attachment.Tool.Description
			case FieldSchema:
				value := cloneMap(attachment.Tool.Schema)
				projection.Schema = &value
			case FieldCustomParameters:
				value := cloneMap(attachment.Tool.CustomParameters)
				projection.CustomParameters = &value
			case FieldTags:
				value := slices.Clone(attachment.Tool.Tags)
				slices.Sort(value)
				value = slices.Compact(value)
				projection.Tags = &value
			default:
				return nil, fmt.Errorf("field %q is not valid for a tool", field)
			}
		}
		return projection, nil
	case attachment.Skill != nil:
		projection := SkillProjection{FieldStates: make(map[Field]FieldState)}
		for _, field := range fields {
			projection.FieldStates[field] = FieldState{Capability: CapabilityDirect, Known: true}
			switch field {
			case FieldDescription:
				value := attachment.Skill.Description
				projection.Description = &value
			case FieldMarkdown:
				value := attachment.Skill.Markdown
				projection.Markdown = &value
			default:
				return nil, fmt.Errorf("field %q is not valid for a skill", field)
			}
		}
		return projection, nil
	default:
		return nil, fmt.Errorf("%s %q has no content", attachment.Kind, attachment.Key())
	}
}

// ApplyAttachmentProjection merges mapped fields into an attachment.
func ApplyAttachmentProjection(attachment Attachment, projection Projection) (Attachment, error) {
	switch value := projection.(type) {
	case ToolProjection:
		if attachment.Tool == nil {
			return Attachment{}, fmt.Errorf("tool %q has no content", attachment.Key())
		}
		tool := *attachment.Tool
		if value.Description != nil {
			description := *value.Description
			tool.Description = &description
		}
		if value.Schema != nil {
			tool.Schema = cloneMap(*value.Schema)
		}
		if value.CustomParameters != nil {
			tool.CustomParameters = cloneMap(*value.CustomParameters)
		}
		if value.Tags != nil {
			tool.Tags = slices.Clone(*value.Tags)
			slices.Sort(tool.Tags)
			tool.Tags = slices.Compact(tool.Tags)
		}
		attachment.Tool = &tool
	case SkillProjection:
		if attachment.Skill == nil {
			return Attachment{}, fmt.Errorf("skill %q has no content", attachment.Key())
		}
		skill := *attachment.Skill
		if value.Description != nil {
			skill.Description = *value.Description
		}
		if value.Markdown != nil {
			skill.Markdown = *value.Markdown
		}
		attachment.Skill = &skill
	default:
		return Attachment{}, fmt.Errorf("unsupported projection %T", projection)
	}
	return attachment, nil
}

// FingerprintProjection returns a stable fingerprint for mapped fields only.
func FingerprintProjection(id ResourceID, projection Projection) (string, error) {
	return fingerprint("projection", struct {
		Schema       string     `json:"schema"`
		ResourceKind Kind       `json:"resourceKind"`
		ProjectKey   string     `json:"projectKey"`
		LookupKey    string     `json:"lookupKey"`
		Projection   Projection `json:"projection"`
	}{
		Schema:       projectionFingerprintSchema,
		ResourceKind: id.Kind,
		ProjectKey:   id.ProjectKey,
		LookupKey:    id.LookupKey,
		Projection:   projection,
	})
}

// ReferenceDigest returns a stable digest of one editable descriptor.
func ReferenceDigest(reference Reference) (string, error) {
	return fingerprint("reference descriptor", struct {
		Schema    string    `json:"schema"`
		Reference Reference `json:"reference"`
	}{
		Schema:    "launchdarkly.config.reference/1",
		Reference: reference,
	})
}

func referenceKeys(values []AttachmentRef) []string {
	keys := make([]string, len(values))
	for index := range values {
		keys[index] = values[index].Key
	}
	slices.Sort(keys)
	return keys
}

func attachmentReferences(keys []string) []AttachmentRef {
	references := make([]AttachmentRef, len(keys))
	for index := range keys {
		references[index] = AttachmentRef{Key: keys[index]}
	}
	return references
}

func cloneMap(source map[string]any) map[string]any {
	if source == nil {
		return map[string]any{}
	}
	data, _ := json.Marshal(source)
	var result map[string]any
	_ = json.Unmarshal(data, &result)
	return result
}

func cloneJSONValue(source any) any {
	data, err := json.Marshal(source)
	if err != nil {
		return source
	}
	var result any
	if err := json.Unmarshal(data, &result); err != nil {
		return source
	}
	return result
}

func canonicalModelParameters(variation Variation) map[string]any {
	model := CanonicalModel(variation.Model, variation.Tools)
	parameters, _ := model["parameters"].(map[string]any)
	return cloneMap(parameters)
}

// ProjectionFieldState returns one mapped field capability.
func ProjectionFieldState(projection Projection, field Field) (FieldState, bool) {
	var states map[Field]FieldState
	switch value := projection.(type) {
	case VariationProjection:
		states = value.FieldStates
	case ToolProjection:
		states = value.FieldStates
	case SkillProjection:
		states = value.FieldStates
	}
	state, ok := states[field]
	return state, ok
}

// ProjectionFieldStates returns a copy of every mapped field state.
func ProjectionFieldStates(projection Projection) map[Field]FieldState {
	result := map[Field]FieldState{}
	for _, field := range projectionStateFields(projection) {
		state, _ := ProjectionFieldState(projection, field)
		result[field] = state
	}
	return result
}

// ProjectionFieldValue returns one known mapped value.
func ProjectionFieldValue(projection Projection, field Field) (any, bool) {
	switch value := projection.(type) {
	case VariationProjection:
		switch field {
		case FieldPrompt, FieldMessages:
			if value.Instructions != nil {
				return *value.Instructions, true
			}
			if value.Messages != nil {
				return *value.Messages, true
			}
		case FieldName:
			if value.Name != nil {
				return *value.Name, true
			}
		case FieldModel:
			if value.Model != nil {
				return *value.Model, true
			}
		case FieldModelParameters:
			if value.Parameters != nil {
				return cloneMap(*value.Parameters), true
			}
		case FieldTools:
			if value.Tools != nil {
				return *value.Tools, true
			}
		case FieldSkills:
			if value.Skills != nil {
				return *value.Skills, true
			}
		default:
			if key, ok := ModelParameterKey(field); ok && value.Parameters != nil {
				parameter, present := (*value.Parameters)[key]
				return cloneJSONValue(parameter), present
			}
		}
	case ToolProjection:
		switch field {
		case FieldDescription:
			if value.Description != nil {
				return *value.Description, true
			}
		case FieldSchema:
			if value.Schema != nil {
				return *value.Schema, true
			}
		case FieldCustomParameters:
			if value.CustomParameters != nil {
				return *value.CustomParameters, true
			}
		case FieldTags:
			if value.Tags != nil {
				return *value.Tags, true
			}
		}
	case SkillProjection:
		if field == FieldDescription && value.Description != nil {
			return *value.Description, true
		}
		if field == FieldMarkdown && value.Markdown != nil {
			return *value.Markdown, true
		}
	}
	return nil, false
}

// FillReadOnlyFields copies server values for local fields that cannot be read.
func FillReadOnlyFields(local, server Projection) (Projection, error) {
	result := local
	for _, field := range projectionStateFields(local) {
		state, _ := ProjectionFieldState(local, field)
		if state.Capability != CapabilityReadOnly || state.Known {
			continue
		}
		if value, ok := ProjectionFieldValue(server, field); ok {
			var err error
			result, err = setProjectionField(result, field, value)
			if err != nil {
				return nil, err
			}
			result = setProjectionState(result, field, state)
		}
	}
	return result, nil
}

func setProjectionState(projection Projection, field Field, state FieldState) Projection {
	switch value := projection.(type) {
	case VariationProjection:
		value.FieldStates[field] = state
		return value
	case ToolProjection:
		value.FieldStates[field] = state
		return value
	case SkillProjection:
		value.FieldStates[field] = state
		return value
	default:
		return projection
	}
}

// ChangedProjection returns only server fields whose projected values differ.
func ChangedProjection(local, server Projection) (Projection, error) {
	result := emptyProjection(server)
	for _, field := range projectionStateFields(local) {
		left, leftOK := ProjectionFieldValue(local, field)
		right, rightOK := ProjectionFieldValue(server, field)
		leftJSON, _ := json.Marshal(left)
		rightJSON, _ := json.Marshal(right)
		if leftOK == rightOK && string(leftJSON) == string(rightJSON) {
			continue
		}
		if rightOK {
			var err error
			result, err = setProjectionField(result, field, right)
			if err != nil {
				return nil, err
			}
		}
	}
	return result, nil
}

// ProjectionIncludes reports whether actual has every known value in expected.
func ProjectionIncludes(actual, expected Projection) bool {
	for _, field := range projectionStateFields(expected) {
		want, wantOK := ProjectionFieldValue(expected, field)
		have, haveOK := ProjectionFieldValue(actual, field)
		wantJSON, _ := json.Marshal(want)
		haveJSON, _ := json.Marshal(have)
		if wantOK != haveOK || string(wantJSON) != string(haveJSON) {
			return false
		}
	}
	return true
}

func projectionStateFields(projection Projection) []Field {
	var states map[Field]FieldState
	switch value := projection.(type) {
	case VariationProjection:
		states = value.FieldStates
	case ToolProjection:
		states = value.FieldStates
	case SkillProjection:
		states = value.FieldStates
	}
	fields := slices.Collect(maps.Keys(states))
	slices.Sort(fields)
	return fields
}

func emptyProjection(projection Projection) Projection {
	switch value := projection.(type) {
	case VariationProjection:
		return VariationProjection{Mode: value.Mode, FieldStates: make(map[Field]FieldState)}
	case ToolProjection:
		return ToolProjection{FieldStates: make(map[Field]FieldState)}
	case SkillProjection:
		return SkillProjection{FieldStates: make(map[Field]FieldState)}
	default:
		return projection
	}
}

func setProjectionField(projection Projection, field Field, raw any) (Projection, error) {
	state := FieldState{Capability: CapabilityDirect, Known: true}
	switch value := projection.(type) {
	case VariationProjection:
		if value.FieldStates == nil {
			value.FieldStates = make(map[Field]FieldState)
		}
		value.FieldStates[field] = state
		switch field {
		case FieldPrompt, FieldMessages:
			switch prompt := raw.(type) {
			case string:
				value.Instructions = &prompt
			case []Message:
				value.Messages = &prompt
			}
		case FieldName:
			item := raw.(string)
			value.Name = &item
		case FieldModel:
			item := raw.(string)
			value.Model = &item
		case FieldModelParameters:
			item, ok := raw.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("field %q needs an object", field)
			}
			item = cloneMap(item)
			value.Parameters = &item
		case FieldTools:
			item := slices.Clone(raw.([]string))
			value.Tools = &item
		case FieldSkills:
			item := slices.Clone(raw.([]string))
			value.Skills = &item
		default:
			key, ok := ModelParameterKey(field)
			if !ok {
				return nil, fmt.Errorf("field %q is not valid for a variation", field)
			}
			if value.Parameters == nil {
				parameters := map[string]any{}
				value.Parameters = &parameters
			}
			(*value.Parameters)[key] = cloneJSONValue(raw)
		}
		return value, nil
	case ToolProjection:
		if value.FieldStates == nil {
			value.FieldStates = make(map[Field]FieldState)
		}
		value.FieldStates[field] = state
		switch field {
		case FieldDescription:
			item := raw.(string)
			value.Description = &item
		case FieldSchema:
			item := cloneMap(raw.(map[string]any))
			value.Schema = &item
		case FieldCustomParameters:
			item := cloneMap(raw.(map[string]any))
			value.CustomParameters = &item
		case FieldTags:
			item := slices.Clone(raw.([]string))
			value.Tags = &item
		default:
			return nil, fmt.Errorf("field %q is not valid for a tool", field)
		}
		return value, nil
	case SkillProjection:
		if value.FieldStates == nil {
			value.FieldStates = make(map[Field]FieldState)
		}
		value.FieldStates[field] = state
		item := raw.(string)
		if field == FieldDescription {
			value.Description = &item
		} else if field == FieldMarkdown {
			value.Markdown = &item
		} else {
			return nil, fmt.Errorf("field %q is not valid for a skill", field)
		}
		return value, nil
	default:
		return nil, fmt.Errorf("unsupported projection %T", projection)
	}
}
