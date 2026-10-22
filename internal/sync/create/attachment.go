package create

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/charmbracelet/huh"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
	syncinteractive "github.com/launchdarkly/ldcli/internal/sync/interactive"
)

// definitionKeys are top-level keys of a tool file or of an MCP tool
// definition. A --json-schema value with one of them is a pasted definition,
// not an input schema.
var definitionKeys = []string{"formatVersion", "inputSchema", "schema"}

// Check applies the content rules of a create of a tool or a skill. It reads
// nothing, so an add runs it before any request.
func (input Input) Check(kind syncdomain.AttachmentKind) error {
	if err := input.checkContentFlag(kind); err != nil {
		return err
	}
	if kind == syncdomain.AttachmentTool {
		_, err := parseSchema(*input.JSONSchema)
		return err
	}
	switch {
	case strings.TrimSpace(*input.Content) == "":
		return errors.New("--content cannot be blank")
	case input.Name != nil && strings.TrimSpace(*input.Name) == "":
		return errors.New("--name cannot be blank")
	}
	return nil
}

// checkContentFlag makes sure that the flag that starts a create is set when
// another create flag is.
func (input Input) checkContentFlag(kind syncdomain.AttachmentKind) error {
	flag, content := "--content", input.Content
	if kind == syncdomain.AttachmentTool {
		flag, content = "--json-schema", input.JSONSchema
	}
	if content != nil {
		return nil
	}

	var passed []string
	if input.Name != nil {
		passed = append(passed, "--name")
	}
	if input.Description != nil {
		passed = append(passed, "--description")
	}
	switch len(passed) {
	case 0:
		return fmt.Errorf("add creates a %s only when %s is set", kind, flag)
	case 1:
		return fmt.Errorf("%s needs %s. add creates a %s only when %s is set.", passed[0], flag, kind, flag)
	default:
		return fmt.Errorf("%s need %s. add creates a %s only when %s is set.", joinFlags(passed), flag, kind, flag)
	}
}

// parseSchema decodes the value of --json-schema, which must be one JSON
// object.
func parseSchema(value string) (map[string]any, error) {
	decoder := json.NewDecoder(strings.NewReader(value))
	var decoded any
	if err := decoder.Decode(&decoded); err != nil {
		return nil, fmt.Errorf("--json-schema must be one JSON object: %w", err)
	}
	schema, isObject := decoded.(map[string]any)
	if !isObject || decoder.Decode(new(any)) != io.EOF {
		return nil, errors.New("--json-schema must be one JSON object")
	}
	if slices.ContainsFunc(definitionKeys, func(key string) bool { _, ok := schema[key]; return ok }) {
		return nil, errors.New(`--json-schema is the input schema only. Pass the value of "inputSchema", ` +
			"and pass the description with --description.")
	}
	return schema, nil
}

// Ask returns the input with the name and the description of a new skill
// that the flags do not give. A tool has no questions. Without questions, the
// input does not change, so the name comes from the key and the description
// is empty. An answer that keeps the name of the key does not set a name. The
// bool result is true when the user cancels.
func (input Input) Ask(id syncdomain.ResourceID, questions Questions) (Input, bool, error) {
	if id.Kind != syncdomain.KindSkill || !questions.Allowed {
		return input, false, nil
	}

	keyName := syncdomain.DisplayName(id.LookupKey)
	name, description := keyName, ""
	var fields []huh.Field
	if input.Name == nil {
		fields = append(fields, huh.NewInput().Title("Skill name").Value(&name).Validate(RequiredValue("skill name")))
	}
	if input.Description == nil {
		fields = append(fields, huh.NewInput().Title("Skill description").Value(&description))
	}
	if len(fields) == 0 {
		return input, false, nil
	}
	if canceled, err := syncinteractive.RunForm(questions.Input, questions.Output, fields...); err != nil || canceled {
		return input, canceled, err
	}

	if input.Name == nil && name != keyName {
		input.Name = &name
	}
	if input.Description == nil {
		input.Description = &description
	}
	return input, false, nil
}

// Attachment returns the tool or the skill that the input creates. It has
// upsert, so that its file can create it again. A skill without a name takes
// the name of its key, and its Markdown ends with a newline, so that content
// from "$(cat file)" has the fingerprint of the file.
func (input Input) Attachment(id syncdomain.ResourceID) (syncdomain.Attachment, error) {
	kind := syncdomain.AttachmentKind(id.Kind)
	if err := input.Check(kind); err != nil {
		return syncdomain.Attachment{}, err
	}

	attachment := syncdomain.Attachment{Kind: kind, Upsert: true}
	var name, description string
	if input.Description != nil {
		description = *input.Description
	}
	if kind == syncdomain.AttachmentTool {
		schema, err := parseSchema(*input.JSONSchema)
		if err != nil {
			return syncdomain.Attachment{}, err
		}
		attachment.Tool = &syncdomain.Tool{Key: id.LookupKey, Schema: schema}
		if description != "" {
			attachment.Tool.Description = &description
		}
	} else {
		skill := syncdomain.Skill{Key: id.LookupKey, Description: description, Markdown: withFinalNewline(*input.Content)}
		if input.Name != nil {
			skill.Name = *input.Name
		}
		skill.Name = skill.CreateName()
		name, attachment.Skill = skill.Name, &skill
	}
	if err := syncdomain.ValidateCreate(id.Kind, id.LookupKey, name, description); err != nil {
		return syncdomain.Attachment{}, err
	}
	return attachment, nil
}

// Matches reports whether LaunchDarkly already has the value of each flag
// that the user passed: the schema or the Markdown, and the description when
// it is set. created is the attachment of the input. The fields without a
// flag, such as tool tags and the name of a skill, come from LaunchDarkly.
func (input Input) Matches(created, existing syncdomain.Attachment) bool {
	candidate := existing
	switch {
	case created.Tool != nil && existing.Tool != nil:
		tool := *existing.Tool
		tool.Schema = created.Tool.Schema
		if input.Description != nil {
			tool.Description = created.Tool.Description
		}
		candidate.Tool = &tool
	case created.Skill != nil && existing.Skill != nil:
		skill := *existing.Skill
		skill.Markdown = created.Skill.Markdown
		if input.Description != nil {
			skill.Description = created.Skill.Description
		}
		candidate.Skill = &skill
	default:
		return false
	}
	return syncdomain.SameAttachmentContent(candidate, existing)
}

// withFinalNewline returns text that ends with a newline.
func withFinalNewline(text string) string {
	if strings.HasSuffix(text, "\n") {
		return text
	}
	return text + "\n"
}
