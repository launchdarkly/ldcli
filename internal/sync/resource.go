package sync

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
)

// RootDir is the repository-relative directory containing sync state.
const RootDir = ".launchdarkly"

// Kind identifies a synchronized resource type.
type Kind string

const (
	KindVariation Kind = "variation"
	KindTool      Kind = "tool"
	KindSkill     Kind = "skill"
)

// ResourceID uniquely identifies a synchronized resource.
type ResourceID struct {
	Kind       Kind
	ProjectKey string
	LookupKey  string
}

// ParseVariationSelector parses the stable project-key/config-key/variation-key identity
// accepted by non-interactive sync commands.
func ParseVariationSelector(selector string) (ResourceID, error) {
	parts := strings.Split(selector, "/")
	if len(parts) != 3 || slices.ContainsFunc(parts, func(part string) bool {
		return part == "" ||
			part == "." ||
			part == ".." ||
			strings.ContainsAny(part, "\\\x00")
	}) {
		return ResourceID{}, fmt.Errorf(
			"invalid variation %q; expected project-key/config-key/variation-key",
			selector,
		)
	}
	return ResourceID{
		Kind:       KindVariation,
		ProjectKey: parts[0],
		LookupKey:  parts[1] + "/" + parts[2],
	}, nil
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
	Kind        Kind
	ProjectKey  string
	LookupKey   string
	Payload     json.RawMessage
	Attachments []Attachment
	Upsert      bool
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
	Mode               VariationMode   `json:"mode" yaml:"mode"`
	Key                string          `json:"key" yaml:"key"`
	Name               string          `json:"name" yaml:"name"`
	Instructions       string          `json:"instructions,omitempty" yaml:"-"`
	ModelConfigKey     string          `json:"modelConfigKey,omitempty" yaml:"modelConfigKey,omitempty"`
	ModelConfigVersion int             `json:"modelConfigVersion,omitempty" yaml:"modelConfigVersion,omitempty"`
	Model              map[string]any  `json:"model,omitempty" yaml:"model,omitempty"`
	OutputFormat       map[string]any  `json:"outputFormat,omitempty" yaml:"outputFormat,omitempty"`
	Messages           []Message       `json:"messages,omitempty" yaml:"-"`
	Tools              []AttachmentRef `json:"tools,omitempty" yaml:"tools,omitempty"`
	Skills             []AttachmentRef `json:"skills,omitempty" yaml:"skills,omitempty"`
	Attachments        []Attachment    `json:"-" yaml:"-"`
}

// NormalizeAttachments validates, sorts, and de-duplicates attachment keys so
// local ordering never produces fingerprint drift.
func (variation *Variation) NormalizeAttachments() error {
	if err := normalizeAttachmentRefs(AttachmentTool, variation.Tools); err != nil {
		return err
	}
	if err := normalizeAttachmentRefs(AttachmentSkill, variation.Skills); err != nil {
		return err
	}
	if err := validateAttachmentMode(*variation); err != nil {
		return err
	}

	slices.SortFunc(variation.Tools, func(a, b AttachmentRef) int { return strings.Compare(a.Key, b.Key) })
	slices.SortFunc(variation.Skills, func(a, b AttachmentRef) int { return strings.Compare(a.Key, b.Key) })
	slices.SortFunc(variation.Attachments, func(a, b Attachment) int {
		if result := strings.Compare(string(a.Kind), string(b.Kind)); result != 0 {
			return result
		}
		return strings.Compare(a.Key(), b.Key())
	})
	return nil
}

// Attachment returns canonical content and the latest observed version for one reference.
func (variation Variation) Attachment(kind AttachmentKind, key string) (Attachment, bool) {
	for _, attachment := range variation.Attachments {
		if attachment.Kind == kind && attachment.Key() == key {
			return attachment, true
		}
	}
	return Attachment{}, false
}

// SetAttachment inserts or replaces one canonical dependency.
func (variation *Variation) SetAttachment(attachment Attachment) {
	for index := range variation.Attachments {
		if variation.Attachments[index].Kind == attachment.Kind && variation.Attachments[index].Key() == attachment.Key() {
			variation.Attachments[index] = attachment
			return
		}
	}
	variation.Attachments = append(variation.Attachments, attachment)
}

func normalizeAttachmentRefs(kind AttachmentKind, refs []AttachmentRef) error {
	seen := make(map[string]struct{}, len(refs))
	for _, ref := range refs {
		if strings.TrimSpace(ref.Key) == "" {
			return fmt.Errorf("%s key is required", kind)
		}
		if _, duplicate := seen[ref.Key]; duplicate {
			return fmt.Errorf("%s key %q is duplicated", kind, ref.Key)
		}
		seen[ref.Key] = struct{}{}
	}
	return nil
}

func validateAttachmentMode(variation Variation) error {
	if variation.Mode == VariationModeCompletion && len(variation.Skills) != 0 {
		return fmt.Errorf("skills can only be attached to agent-mode configs")
	}
	return nil
}
