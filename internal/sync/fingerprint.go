package sync

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
)

const (
	variationFingerprintSchema  = "launchdarkly.config.variation/v1"
	attachmentFingerprintSchema = "launchdarkly.config.attachment/v1"
)

// FingerprintVariation returns a stable fingerprint for the variation fields
// supported by the existing config variation APIs.
func FingerprintVariation(projectKey, lookupKey string, variation Variation) (string, error) {
	normalized := variation
	// A value copy still shares slice backing arrays; clone before canonical
	// normalization so fingerprinting never mutates the caller's variation.
	normalized.Tools = append([]AttachmentRef(nil), variation.Tools...)
	normalized.Skills = append([]AttachmentRef(nil), variation.Skills...)
	normalized.Attachments = append([]Attachment(nil), variation.Attachments...)
	if err := normalized.NormalizeAttachments(); err != nil {
		return "", err
	}
	if err := validateDirectAPIVariationFields(normalized); err != nil {
		return "", err
	}
	for index := range normalized.Attachments {
		normalized.Attachments[index] = CanonicalAttachment(normalized.Attachments[index])
	}
	for index := range normalized.Tools {
		normalized.Tools[index].Version = 0
	}
	for index := range normalized.Skills {
		normalized.Skills[index].Version = 0
	}
	normalized.Model = normalizeModelForFingerprint(normalized.Model)
	if len(normalized.Messages) == 0 {
		normalized.Messages = nil
	}
	switch normalized.Mode {
	case VariationModeAgent:
		normalized.Instructions = NormalizePromptText(normalized.Instructions)
		normalized.Messages = nil
	case VariationModeCompletion:
		normalized.Instructions = ""
		// Clone messages before normalizing their text to preserve caller state.
		normalized.Messages = append([]Message(nil), normalized.Messages...)
		for index := range normalized.Messages {
			normalized.Messages[index].Content = NormalizePromptText(normalized.Messages[index].Content)
		}
	}

	value := struct {
		Schema       string       `json:"schema"`
		ResourceKind Kind         `json:"resourceKind"`
		ProjectKey   string       `json:"projectKey"`
		LookupKey    string       `json:"lookupKey"`
		Variation    Variation    `json:"variation"`
		Attachments  []Attachment `json:"attachments,omitempty"`
	}{
		Schema:       variationFingerprintSchema,
		ResourceKind: KindVariation,
		ProjectKey:   projectKey,
		LookupKey:    lookupKey,
		Variation:    normalized,
		Attachments:  normalized.Attachments,
	}

	return fingerprint(value, "variation")
}

// FingerprintAttachment returns the stable baseline for one project-scoped
// tool or skill, excluding runtime version and file-only metadata.
func FingerprintAttachment(projectKey string, attachment Attachment) (string, error) {
	if attachment.Kind != AttachmentTool && attachment.Kind != AttachmentSkill {
		return "", fmt.Errorf("unsupported attachment kind %q", attachment.Kind)
	}
	if attachment.Key() == "" {
		return "", fmt.Errorf("%s key is required", attachment.Kind)
	}

	value := struct {
		Schema       string     `json:"schema"`
		ResourceKind Kind       `json:"resourceKind"`
		ProjectKey   string     `json:"projectKey"`
		LookupKey    string     `json:"lookupKey"`
		Attachment   Attachment `json:"attachment"`
	}{
		Schema:       attachmentFingerprintSchema,
		ResourceKind: Kind(attachment.Kind),
		ProjectKey:   projectKey,
		LookupKey:    attachment.Key(),
		Attachment:   CanonicalAttachment(attachment),
	}
	return fingerprint(value, string(attachment.Kind))
}

func fingerprint(value any, resource string) (string, error) {
	canonical, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("encode %s fingerprint: %w", resource, err)
	}
	sum := sha256.Sum256(canonical)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

// CanonicalAttachment removes runtime versions, API-only metadata, and
// ordering differences before comparison or fingerprinting.
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
			// Clone tags before sorting so canonicalization preserves caller order.
			tool.Tags = append([]string(nil), tool.Tags...)
			slices.Sort(tool.Tags)
		}
		attachment.Tool = &tool
	}
	if attachment.Skill != nil {
		// Name is catalog metadata and is not editable from the local skill file.
		skill := *attachment.Skill
		skill.Name = ""
		attachment.Skill = &skill
	}
	return attachment
}

// normalizeModelForFingerprint removes only defaults that the variation API
// adds without changing model behavior. Other empty objects remain meaningful.
func normalizeModelForFingerprint(model map[string]any) map[string]any {
	if len(model) == 0 {
		return nil
	}

	normalized := maps.Clone(model)
	for _, key := range []string{"parameters", "custom"} {
		if value, ok := normalized[key].(map[string]any); ok && len(value) == 0 {
			delete(normalized, key)
		}
	}
	if len(normalized) == 0 {
		return nil
	}
	return normalized
}

// ValidateDirectAPIVariation rejects fields that the existing variation APIs
// cannot round-trip without the sync endpoints.
func ValidateDirectAPIVariation(variation Variation) error {
	if err := validateDirectAPIVariationFields(variation); err != nil {
		return err
	}
	if err := normalizeAttachmentRefs(AttachmentTool, variation.Tools); err != nil {
		return err
	}
	if err := normalizeAttachmentRefs(AttachmentSkill, variation.Skills); err != nil {
		return err
	}
	return validateAttachmentMode(variation)
}

func validateDirectAPIVariationFields(variation Variation) error {
	switch {
	case !variation.Mode.Valid():
		return fmt.Errorf("unsupported variation mode %q", variation.Mode)
	case variation.Key == "":
		return fmt.Errorf("variation key is required")
	case variation.Name == "":
		return fmt.Errorf("variation name is required")
	}
	return nil
}
