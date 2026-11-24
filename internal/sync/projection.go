package sync

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"
)

// ProjectionFingerprintSchema is the schema of descriptor-backed fingerprints.
const ProjectionFingerprintSchema = "launchdarkly.config.projection/1"

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

// Capability describes whether sync can write one mapped field back to its
// source. A read-only field blocks the whole resource.
type Capability string

const (
	CapabilityDirect   Capability = "direct"
	CapabilityReadOnly Capability = "read-only"
)

// FieldState records the write capability of one mapped field and whether
// the source gave it a value. A read-only field explains itself with Reason,
// and with Code and File when another file holds the value.
type FieldState struct {
	Capability Capability
	Known      bool
	Reason     string
	Code       ErrorCode
	File       string
}

// Projection is the mapped content of one repository source. Each kind has
// one typed projection. The methods let the planner compare and merge
// projections without knowing the kind.
type Projection interface {
	Kind() Kind
	// States returns the state of every mapped field.
	States() map[Field]FieldState
	// Value returns one known mapped value.
	Value(Field) (any, bool)
	// set returns a copy with one field set to value in state.
	set(Field, any, FieldState) (Projection, error)
	// empty returns a projection of the same kind with no fields.
	empty() Projection
}

// VariationProjection is the mapped content of one variation. Mode is the
// mode that the source implies: completion with Messages, else agent.
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

// Kind returns KindVariation.
func (VariationProjection) Kind() Kind { return KindVariation }

// States returns the state of every mapped field.
func (projection VariationProjection) States() map[Field]FieldState { return projection.FieldStates }

// Value returns one known mapped value. FieldPrompt and FieldMessages both
// return the prompt: instructions or messages.
func (projection VariationProjection) Value(field Field) (any, bool) {
	switch field {
	case FieldPrompt, FieldMessages:
		if projection.Instructions != nil {
			return *projection.Instructions, true
		}
		if projection.Messages != nil {
			return *projection.Messages, true
		}
	case FieldName:
		return deref(projection.Name)
	case FieldModel:
		return deref(projection.Model)
	case FieldModelParameters:
		if projection.Parameters != nil {
			return cloneMap(*projection.Parameters), true
		}
	case FieldTools:
		return deref(projection.Tools)
	case FieldSkills:
		return deref(projection.Skills)
	default:
		if key, ok := ModelParameterKey(field); ok && projection.Parameters != nil {
			parameter, present := (*projection.Parameters)[key]
			return cloneJSONValue(parameter), present
		}
	}
	return nil, false
}

func (projection VariationProjection) set(field Field, raw any, state FieldState) (Projection, error) {
	projection.FieldStates = withState(projection.FieldStates, field, state)
	switch field {
	case FieldPrompt, FieldMessages:
		switch prompt := NormalizePrompt(raw).(type) {
		case string:
			projection.Instructions, projection.Messages = &prompt, nil
		case []Message:
			projection.Instructions, projection.Messages = nil, &prompt
		default:
			return nil, fmt.Errorf("field %q needs a string or messages", field)
		}
	case FieldName:
		return projection, setString(field, raw, &projection.Name)
	case FieldModel:
		return projection, setString(field, raw, &projection.Model)
	case FieldModelParameters:
		return projection, setMap(field, raw, &projection.Parameters)
	case FieldTools:
		return projection, setStrings(field, raw, &projection.Tools)
	case FieldSkills:
		return projection, setStrings(field, raw, &projection.Skills)
	default:
		key, ok := ModelParameterKey(field)
		if !ok {
			return nil, fmt.Errorf("field %q is not valid for a variation", field)
		}
		parameters := map[string]any{}
		if projection.Parameters != nil {
			parameters = cloneMap(*projection.Parameters)
		}
		parameters[key] = cloneJSONValue(raw)
		projection.Parameters = &parameters
	}
	return projection, nil
}

func (projection VariationProjection) empty() Projection {
	return VariationProjection{Mode: projection.Mode, FieldStates: make(map[Field]FieldState)}
}

// ForMode returns the projection in the mode of a config. One system message
// and agent instructions are the same prompt in the two modes, and an empty
// prompt is empty in both. Any other message list cannot become instructions.
func (projection VariationProjection) ForMode(mode VariationMode) (VariationProjection, error) {
	if !mode.Valid() || projection.Mode == mode {
		return projection, nil
	}
	projection.Mode = mode
	switch {
	case mode == VariationModeCompletion && projection.Instructions != nil:
		messages := []Message{}
		if *projection.Instructions != "" {
			messages = append(messages, Message{Role: RoleSystem, Content: *projection.Instructions})
		}
		projection.Instructions, projection.Messages = nil, &messages
	case mode == VariationModeAgent && projection.Messages != nil:
		messages := *projection.Messages
		content := ""
		switch {
		case len(messages) == 0:
		case len(messages) == 1 && messages[0].Role == RoleSystem:
			content = messages[0].Content
		default:
			return VariationProjection{}, fmt.Errorf(
				"the source has %d messages, but an agent config has only instructions", len(messages),
			)
		}
		projection.Instructions, projection.Messages = &content, nil
	}
	return projection, nil
}

// ToolProjection is the mapped content of one tool.
type ToolProjection struct {
	Description      *string              `json:"description,omitempty"`
	Schema           *map[string]any      `json:"schema,omitempty"`
	CustomParameters *map[string]any      `json:"customParameters,omitempty"`
	Tags             *[]string            `json:"tags,omitempty"`
	FieldStates      map[Field]FieldState `json:"-"`
}

// Kind returns KindTool.
func (ToolProjection) Kind() Kind { return KindTool }

// States returns the state of every mapped field.
func (projection ToolProjection) States() map[Field]FieldState { return projection.FieldStates }

// Value returns one known mapped value.
func (projection ToolProjection) Value(field Field) (any, bool) {
	switch field {
	case FieldDescription:
		return deref(projection.Description)
	case FieldSchema:
		return derefMap(projection.Schema)
	case FieldCustomParameters:
		return derefMap(projection.CustomParameters)
	case FieldTags:
		return deref(projection.Tags)
	}
	return nil, false
}

func (projection ToolProjection) set(field Field, raw any, state FieldState) (Projection, error) {
	projection.FieldStates = withState(projection.FieldStates, field, state)
	switch field {
	case FieldDescription:
		return projection, setString(field, raw, &projection.Description)
	case FieldSchema:
		return projection, setMap(field, raw, &projection.Schema)
	case FieldCustomParameters:
		return projection, setMap(field, raw, &projection.CustomParameters)
	case FieldTags:
		return projection, setStrings(field, raw, &projection.Tags)
	default:
		return nil, fmt.Errorf("field %q is not valid for a tool", field)
	}
}

func (ToolProjection) empty() Projection {
	return ToolProjection{FieldStates: make(map[Field]FieldState)}
}

// SkillProjection is the mapped content of one skill.
type SkillProjection struct {
	Description *string              `json:"description,omitempty"`
	Markdown    *string              `json:"markdown,omitempty"`
	FieldStates map[Field]FieldState `json:"-"`
}

// Kind returns KindSkill.
func (SkillProjection) Kind() Kind { return KindSkill }

// States returns the state of every mapped field.
func (projection SkillProjection) States() map[Field]FieldState { return projection.FieldStates }

// Value returns one known mapped value.
func (projection SkillProjection) Value(field Field) (any, bool) {
	switch field {
	case FieldDescription:
		return deref(projection.Description)
	case FieldMarkdown:
		return deref(projection.Markdown)
	}
	return nil, false
}

func (projection SkillProjection) set(field Field, raw any, state FieldState) (Projection, error) {
	projection.FieldStates = withState(projection.FieldStates, field, state)
	switch field {
	case FieldDescription:
		return projection, setString(field, raw, &projection.Description)
	case FieldMarkdown:
		return projection, setString(field, raw, &projection.Markdown)
	default:
		return nil, fmt.Errorf("field %q is not valid for a skill", field)
	}
}

func (SkillProjection) empty() Projection {
	return SkillProjection{FieldStates: make(map[Field]FieldState)}
}

// ProjectionFields returns the fields that a reference maps, in order.
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

// ModelParameterKey returns the exact key of a model.parameters["key"] field.
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

// ProjectVariation selects the mapped fields of a reference from a variation.
func ProjectVariation(variation Variation, reference Reference) (VariationProjection, error) {
	fields, err := ProjectionFields(reference)
	if err != nil {
		return VariationProjection{}, err
	}
	projection := VariationProjection{Mode: variation.Mode, FieldStates: make(map[Field]FieldState)}
	for _, field := range fields {
		projection.FieldStates[field] = directState
		switch field {
		case FieldPrompt, FieldMessages:
			if variation.Mode == VariationModeCompletion {
				messages := NormalizeMessages(variation.Messages)
				projection.Messages = &messages
			} else {
				value := NormalizePromptText(variation.Instructions)
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
	if reference.Tools != nil {
		values := referenceKeys(variation.Tools)
		projection.Tools = &values
		projection.FieldStates[FieldTools] = directState
	}
	if reference.Skills != nil {
		values := referenceKeys(variation.Skills)
		projection.Skills = &values
		projection.FieldStates[FieldSkills] = directState
	}
	markKnown(projection)
	return projection, nil
}

// markKnown sets Known on each direct field state from whether the projection
// has a value for the field. A server can lack a mapped value, such as a
// model name or a tool description.
func markKnown(projection Projection) {
	for field, state := range projection.States() {
		_, state.Known = projection.Value(field)
		projection.States()[field] = state
	}
}

// ApplyVariationProjection merges the mapped fields of a projection into a
// variation. Fields that the projection does not map stay unchanged.
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
			for _, field := range sortedFields(projection.FieldStates) {
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

// ProjectAttachment selects the mapped fields of a reference from a tool or
// a skill.
func ProjectAttachment(attachment Attachment, reference Reference) (Projection, error) {
	fields, err := ProjectionFields(reference)
	if err != nil {
		return nil, err
	}
	switch {
	case attachment.Tool != nil:
		projection := ToolProjection{FieldStates: make(map[Field]FieldState)}
		for _, field := range fields {
			projection.FieldStates[field] = directState
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
				value := sortedUnique(attachment.Tool.Tags)
				projection.Tags = &value
			default:
				return nil, fmt.Errorf("field %q is not valid for a tool", field)
			}
		}
		markKnown(projection)
		return projection, nil
	case attachment.Skill != nil:
		projection := SkillProjection{FieldStates: make(map[Field]FieldState)}
		for _, field := range fields {
			projection.FieldStates[field] = directState
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
		markKnown(projection)
		return projection, nil
	default:
		return nil, fmt.Errorf("%s %q has no content", attachment.Kind, attachment.Key())
	}
}

// ApplyAttachmentProjection merges the mapped fields of a projection into a
// tool or a skill.
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
			tool.Tags = sortedUnique(*value.Tags)
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

// FingerprintProjection returns a stable fingerprint of the mapped fields.
func FingerprintProjection(id ResourceID, projection Projection) (string, error) {
	return fingerprint("projection", struct {
		Schema       string     `json:"schema"`
		ResourceKind Kind       `json:"resourceKind"`
		ProjectKey   string     `json:"projectKey"`
		LookupKey    string     `json:"lookupKey"`
		Projection   Projection `json:"projection"`
	}{
		Schema:       ProjectionFingerprintSchema,
		ResourceKind: id.Kind,
		ProjectKey:   id.ProjectKey,
		LookupKey:    id.LookupKey,
		Projection:   projection,
	})
}

// ReferenceDigest returns a stable digest of the mapping of a descriptor. The
// relationship lists are content, so they are not part of it.
func ReferenceDigest(reference Reference) (string, error) {
	return fingerprint("reference descriptor", struct {
		Schema string            `json:"schema"`
		File   string            `json:"file"`
		Format string            `json:"format"`
		Select string            `json:"select,omitempty"`
		Map    map[string]string `json:"map,omitempty"`
	}{
		Schema: "launchdarkly.config.reference/1",
		File:   reference.File, Format: reference.Format, Select: reference.Select, Map: reference.Map,
	})
}

// FillReadOnlyFields copies the server value of each read-only field that
// the source cannot read, so that the two sides compare on equal terms.
func FillReadOnlyFields(local, server Projection) (Projection, error) {
	result := local
	for _, field := range sortedFields(local.States()) {
		state := local.States()[field]
		if state.Capability != CapabilityReadOnly || state.Known {
			continue
		}
		value, ok := server.Value(field)
		if !ok {
			continue
		}
		var err error
		if result, err = result.set(field, value, state); err != nil {
			return nil, err
		}
	}
	return result, nil
}

// ChangedProjection returns the server values of the mapped fields whose
// values differ from the local projection.
func ChangedProjection(local, server Projection) (Projection, error) {
	result := server.empty()
	for _, field := range sortedFields(local.States()) {
		left, leftOK := local.Value(field)
		right, rightOK := server.Value(field)
		if leftOK == rightOK && sameJSON(left, right) {
			continue
		}
		if !rightOK {
			continue
		}
		var err error
		if result, err = result.set(field, right, directState); err != nil {
			return nil, err
		}
	}
	return result, nil
}

// ProjectionIncludes reports whether actual has every known value of expected.
func ProjectionIncludes(actual, expected Projection) bool {
	fields := sortedFields(expected.States())
	if len(fields) == 0 {
		// A projection without field states, such as a desired value built
		// by a caller, is compared on every field that it has a value for.
		for _, field := range comparableFields {
			if _, ok := expected.Value(field); ok {
				fields = append(fields, field)
			}
		}
	}
	for _, field := range fields {
		want, wantOK := expected.Value(field)
		have, haveOK := actual.Value(field)
		if wantOK != haveOK || !sameJSON(want, have) {
			return false
		}
	}
	return true
}

// comparableFields are the fields of every kind that a projection can hold
// a value for, without the per-parameter model fields.
var comparableFields = []Field{
	FieldPrompt, FieldName, FieldModel, FieldModelParameters, FieldTools, FieldSkills,
	FieldDescription, FieldSchema, FieldCustomParameters, FieldTags, FieldMarkdown,
}

var directState = FieldState{Capability: CapabilityDirect, Known: true}

func sortedFields(states map[Field]FieldState) []Field {
	return slices.Sorted(maps.Keys(states))
}

func withState(states map[Field]FieldState, field Field, state FieldState) map[Field]FieldState {
	states = maps.Clone(states)
	if states == nil {
		states = make(map[Field]FieldState)
	}
	states[field] = state
	return states
}

func setString(field Field, raw any, target **string) error {
	value, ok := raw.(string)
	if !ok {
		return fmt.Errorf("field %q needs a string", field)
	}
	*target = &value
	return nil
}

func setStrings(field Field, raw any, target **[]string) error {
	value, ok := raw.([]string)
	if !ok {
		return fmt.Errorf("field %q needs a string list", field)
	}
	value = slices.Clone(value)
	*target = &value
	return nil
}

func setMap(field Field, raw any, target **map[string]any) error {
	value, ok := raw.(map[string]any)
	if !ok {
		return fmt.Errorf("field %q needs an object", field)
	}
	value = cloneMap(value)
	*target = &value
	return nil
}

func deref[T any](value *T) (any, bool) {
	if value == nil {
		return nil, false
	}
	return *value, true
}

func derefMap(value *map[string]any) (any, bool) {
	if value == nil {
		return nil, false
	}
	return cloneMap(*value), true
}

func sameJSON(left, right any) bool {
	leftJSON, _ := json.Marshal(left)
	rightJSON, _ := json.Marshal(right)
	return string(leftJSON) == string(rightJSON)
}

func sortedUnique(values []string) []string {
	return slices.Compact(slices.Sorted(slices.Values(values)))
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
