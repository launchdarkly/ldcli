package sync

import (
	"encoding/json"
	"strings"
)

// RootDir is the repository-relative directory containing sync state.
const RootDir = ".launchdarkly"

// Kind identifies a synchronized resource type.
type Kind string

const (
	KindVariation Kind = "variation"
)

// ResourceID uniquely identifies a synchronized resource.
type ResourceID struct {
	Kind       Kind
	ProjectKey string
	LookupKey  string
}

// CompareResourceIDs orders resource identities for deterministic plans and output.
func CompareResourceIDs(left, right ResourceID) int {
	if result := strings.Compare(string(left.Kind), string(right.Kind)); result != 0 {
		return result
	}
	if result := strings.Compare(left.ProjectKey, right.ProjectKey); result != 0 {
		return result
	}
	return strings.Compare(left.LookupKey, right.LookupKey)
}

// SyncedResource contains one compiled local resource.
type SyncedResource struct {
	Kind       Kind
	ProjectKey string
	LookupKey  string
	Payload    json.RawMessage
	Upsert     bool
}

// VariationMode identifies how a prompt variation stores its content.
type VariationMode string

const (
	VariationModeAgent      VariationMode = "agent"
	VariationModeCompletion VariationMode = "completion"
)

// Valid reports whether the mode is supported for synchronized config variations.
func (mode VariationMode) Valid() bool {
	switch mode {
	case VariationModeAgent, VariationModeCompletion:
		return true
	default:
		return false
	}
}

// Message is one role/content pair in a completion prompt.
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// AttachmentKind identifies a versioned resource referenced by a variation.
type AttachmentKind string

const (
	AttachmentTool  AttachmentKind = "tool"
	AttachmentSkill AttachmentKind = "skill"
)

// AttachmentRef is the stable local reference and exact API pin for one
// variation attachment. Local files persist only the key.
type AttachmentRef struct {
	Key     string `json:"key" yaml:"key"`
	Version int    `json:"version,omitempty" yaml:"-"`
}

// Tool is the canonical, version-independent content of an AI tool.
type Tool struct {
	Key              string         `json:"key" yaml:"key"`
	Description      *string        `json:"description,omitempty" yaml:"description,omitempty"`
	Schema           map[string]any `json:"schema" yaml:"schema"`
	CustomParameters map[string]any `json:"customParameters,omitempty" yaml:"customParameters,omitempty"`
	Tags             []string       `json:"tags,omitempty" yaml:"tags,omitempty"`
}

// Skill carries one Markdown file plus catalog metadata used for display.
// Local synchronization owns Key, Description, and Markdown; Name remains
// server-owned.
type Skill struct {
	Key         string `json:"key"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Markdown    string `json:"markdown"`
}

// Attachment pairs canonical content with the latest version observed from
// LaunchDarkly. Exactly one type-specific payload is present.
type Attachment struct {
	Kind    AttachmentKind
	Version int
	Upsert  bool
	Tool    *Tool
	Skill   *Skill
}

// Key returns the stable key of the type-specific attachment payload.
func (attachment Attachment) Key() string {
	switch attachment.Kind {
	case AttachmentTool:
		if attachment.Tool != nil {
			return attachment.Tool.Key
		}
	case AttachmentSkill:
		if attachment.Skill != nil {
			return attachment.Skill.Key
		}
	}
	return ""
}

// NormalizePromptText gives semantically equivalent prompt text one stable
// representation across API responses, local files, and operating systems.
func NormalizePromptText(content string) string {
	content = strings.ReplaceAll(content, "\r\n", "\n")
	content = strings.ReplaceAll(content, "\r", "\n")
	return strings.TrimSpace(content)
}

// Variation is the common prompt variation representation used by sync.
type Variation struct {
	Mode               VariationMode  `json:"mode" yaml:"mode"`
	Key                string         `json:"key" yaml:"key"`
	Name               string         `json:"name" yaml:"name"`
	Instructions       string         `json:"instructions,omitempty" yaml:"-"`
	ModelConfigKey     string         `json:"modelConfigKey,omitempty" yaml:"modelConfigKey,omitempty"`
	ModelConfigVersion int            `json:"modelConfigVersion,omitempty" yaml:"modelConfigVersion,omitempty"`
	Model              map[string]any `json:"model,omitempty" yaml:"model,omitempty"`
	OutputFormat       map[string]any `json:"outputFormat,omitempty" yaml:"outputFormat,omitempty"`
	Messages           []Message      `json:"messages,omitempty" yaml:"-"`
}
