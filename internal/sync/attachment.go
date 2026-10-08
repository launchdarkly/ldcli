package sync

import (
	"encoding/json"
	"slices"
)

// AttachmentKind identifies a versioned resource that a variation references.
type AttachmentKind string

const (
	AttachmentTool  AttachmentKind = "tool"
	AttachmentSkill AttachmentKind = "skill"
)

// AttachmentKinds lists every attachment kind in canonical order.
var AttachmentKinds = []AttachmentKind{AttachmentTool, AttachmentSkill}

// Valid reports whether sync supports the attachment kind.
func (kind AttachmentKind) Valid() bool {
	return slices.Contains(AttachmentKinds, kind)
}

// AttachmentRef is a reference from a variation to one tool or skill. Local
// files store only the key. The API also pins an exact version.
type AttachmentRef struct {
	Key     string `json:"key" yaml:"key"`
	Version int    `json:"version,omitempty" yaml:"-"`
}

// Tool is the version-independent content of an AI tool.
type Tool struct {
	Key              string         `json:"key" yaml:"key"`
	Description      *string        `json:"description,omitempty" yaml:"description,omitempty"`
	Schema           map[string]any `json:"schema" yaml:"schema"`
	CustomParameters map[string]any `json:"customParameters,omitempty" yaml:"customParameters,omitempty"`
	Tags             []string       `json:"tags,omitempty" yaml:"tags,omitempty"`
}

// Skill is one Markdown file and its catalog metadata. Sync owns Key,
// Description, and Markdown. LaunchDarkly owns Name.
type Skill struct {
	Key         string `json:"key"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Markdown    string `json:"markdown"`
}

// Attachment is the content of one tool or one skill, with the latest version
// that sync read from LaunchDarkly. Exactly one of Tool and Skill is set.
//
// The JSON encoding of this struct is part of the variation fingerprint.
type Attachment struct {
	Kind    AttachmentKind
	Version int
	// Upsert lets sync create the tool in LaunchDarkly when it is absent.
	Upsert bool
	Tool   *Tool
	Skill  *Skill
}

// Key returns the stable key of the tool or the skill.
func (attachment Attachment) Key() string {
	switch {
	case attachment.Kind == AttachmentTool && attachment.Tool != nil:
		return attachment.Tool.Key
	case attachment.Kind == AttachmentSkill && attachment.Skill != nil:
		return attachment.Skill.Key
	default:
		return ""
	}
}

// ID returns the manifest identity of the attachment in one project.
func (attachment Attachment) ID(projectKey string) ResourceID {
	return ResourceID{Kind: Kind(attachment.Kind), ProjectKey: projectKey, LookupKey: attachment.Key()}
}

// CanonicalAttachment removes the runtime version, local-only flags, and
// server-owned metadata. Two attachments with the same canonical form have
// the same content.
func CanonicalAttachment(attachment Attachment) Attachment {
	attachment.Version = 0
	attachment.Upsert = false
	if attachment.Tool != nil {
		tool := *attachment.Tool
		if len(tool.CustomParameters) == 0 {
			tool.CustomParameters = nil
		}
		if len(tool.Tags) == 0 {
			tool.Tags = nil
		} else {
			tool.Tags = slices.Sorted(slices.Values(tool.Tags))
		}
		attachment.Tool = &tool
	}
	if attachment.Skill != nil {
		skill := *attachment.Skill
		skill.Name = ""
		attachment.Skill = &skill
	}
	return attachment
}

// SameAttachmentContent reports whether two attachments have the same
// canonical content.
func SameAttachmentContent(left, right Attachment) bool {
	leftJSON, _ := json.Marshal(CanonicalAttachment(left))
	rightJSON, _ := json.Marshal(CanonicalAttachment(right))
	return string(leftJSON) == string(rightJSON)
}
