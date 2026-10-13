package sync

import (
	"errors"
	"fmt"
	"slices"
	"strings"
)

// VariationMode identifies how a prompt variation stores its content. An agent
// variation has instructions. A completion variation has messages.
type VariationMode string

const (
	VariationModeAgent      VariationMode = "agent"
	VariationModeCompletion VariationMode = "completion"
)

// Valid reports whether sync supports the mode.
func (mode VariationMode) Valid() bool {
	return mode == VariationModeAgent || mode == VariationModeCompletion
}

// Message is one role and content pair in a completion prompt.
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// The roles that a completion message can use.
const (
	RoleSystem    = "system"
	RoleUser      = "user"
	RoleAssistant = "assistant"
)

// MessageRoles lists the roles that a completion message can use.
var MessageRoles = []string{RoleSystem, RoleUser, RoleAssistant}

// ValidMessageRole reports whether a completion message can use the role.
func ValidMessageRole(role string) bool {
	return slices.Contains(MessageRoles, role)
}

// NormalizePromptText gives equivalent prompt text one representation across
// API responses, local files, and operating systems.
func NormalizePromptText(content string) string {
	content = strings.ReplaceAll(content, "\r\n", "\n")
	content = strings.ReplaceAll(content, "\r", "\n")
	return strings.TrimSpace(content)
}

// Variation is the prompt variation model that every sync component uses.
//
// The JSON encoding of this struct is part of the fingerprint. Do not rename a
// field or change a JSON tag, because each change invalidates every manifest.
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
	// Attachments holds the content of every tool and skill in Tools and Skills.
	Attachments []Attachment `json:"-" yaml:"-"`
}

// Validate makes sure that the variation has the fields that LaunchDarkly
// requires and that its attachment references are usable.
func (variation Variation) Validate() error {
	switch {
	case variation.Mode == "":
		return errors.New("mode is required")
	case !variation.Mode.Valid():
		return fmt.Errorf("unsupported mode %q", variation.Mode)
	case variation.Key == "":
		return errors.New("key is required")
	case variation.Name == "":
		return errors.New("name is required")
	case variation.Mode != VariationModeAgent && len(variation.Skills) != 0:
		return errors.New("skills can only be attached to agent-mode configs")
	}

	for _, kind := range AttachmentKinds {
		seen := make(map[string]struct{})
		for _, ref := range variation.Refs(kind) {
			if strings.TrimSpace(ref.Key) == "" {
				return fmt.Errorf("%s key is required", kind)
			}
			if _, duplicate := seen[ref.Key]; duplicate {
				return fmt.Errorf("%s key %q is duplicated", kind, ref.Key)
			}
			seen[ref.Key] = struct{}{}
		}
	}
	return nil
}

// Refs returns the references of one attachment kind.
func (variation Variation) Refs(kind AttachmentKind) []AttachmentRef {
	switch kind {
	case AttachmentTool:
		return variation.Tools
	case AttachmentSkill:
		return variation.Skills
	default:
		return nil
	}
}

// Attachment returns the content of one referenced attachment.
func (variation Variation) Attachment(kind AttachmentKind, key string) (Attachment, bool) {
	index := variation.attachmentIndex(kind, key)
	if index < 0 {
		return Attachment{}, false
	}
	return variation.Attachments[index], true
}

// SetAttachment inserts or replaces the content of one attachment.
func (variation *Variation) SetAttachment(attachment Attachment) {
	if index := variation.attachmentIndex(attachment.Kind, attachment.Key()); index >= 0 {
		variation.Attachments[index] = attachment
		return
	}
	variation.Attachments = append(variation.Attachments, attachment)
}

// Attach adds a reference to the attachment and stores its content. If the
// variation already references the key, Attach replaces only the content.
func (variation *Variation) Attach(attachment Attachment) {
	refs := variation.refsPointer(attachment.Kind)
	if refs != nil && !slices.ContainsFunc(*refs, func(ref AttachmentRef) bool { return ref.Key == attachment.Key() }) {
		*refs = append(*refs, AttachmentRef{Key: attachment.Key()})
	}
	variation.SetAttachment(attachment)
}

// HydrateAttachments replaces Attachments with the content that read returns
// for each reference, and then sorts the references and the content.
func (variation *Variation) HydrateAttachments(read func(kind AttachmentKind, key string) (Attachment, error)) error {
	attachments := make([]Attachment, 0, len(variation.Tools)+len(variation.Skills))
	for _, kind := range AttachmentKinds {
		for _, ref := range variation.Refs(kind) {
			attachment, err := read(kind, ref.Key)
			if err != nil {
				return err
			}
			attachments = append(attachments, attachment)
		}
	}
	variation.Attachments = attachments
	variation.SortAttachments()
	return nil
}

// SortAttachments puts references and content in key order, so that the local
// order of a list never changes a fingerprint or a rendered file.
func (variation *Variation) SortAttachments() {
	byKey := func(left, right AttachmentRef) int { return strings.Compare(left.Key, right.Key) }
	slices.SortFunc(variation.Tools, byKey)
	slices.SortFunc(variation.Skills, byKey)
	slices.SortFunc(variation.Attachments, func(left, right Attachment) int {
		if result := strings.Compare(string(left.Kind), string(right.Kind)); result != 0 {
			return result
		}
		return strings.Compare(left.Key(), right.Key())
	})
}

// PinAttachments returns a copy of the variation in which each reference uses
// the version that pin returns for its content. The original is not changed.
func (variation Variation) PinAttachments(pin func(Attachment) (int, error)) (Variation, error) {
	variation.Tools = slices.Clone(variation.Tools)
	variation.Skills = slices.Clone(variation.Skills)
	for _, kind := range AttachmentKinds {
		refs := *variation.refsPointer(kind)
		for index := range refs {
			attachment, ok := variation.Attachment(kind, refs[index].Key)
			if !ok {
				return Variation{}, fmt.Errorf("%s %q content is missing", kind, refs[index].Key)
			}
			version, err := pin(attachment)
			if err != nil {
				return Variation{}, err
			}
			refs[index].Version = version
		}
	}
	return variation, nil
}

// PinnedToLatest returns a copy of the variation in which each reference uses
// the version of its hydrated content.
func (variation Variation) PinnedToLatest() (Variation, error) {
	return variation.PinAttachments(func(attachment Attachment) (int, error) {
		return attachment.Version, nil
	})
}

func (variation *Variation) refsPointer(kind AttachmentKind) *[]AttachmentRef {
	switch kind {
	case AttachmentTool:
		return &variation.Tools
	case AttachmentSkill:
		return &variation.Skills
	default:
		return nil
	}
}

func (variation Variation) attachmentIndex(kind AttachmentKind, key string) int {
	return slices.IndexFunc(variation.Attachments, func(attachment Attachment) bool {
		return attachment.Kind == kind && attachment.Key() == key
	})
}
