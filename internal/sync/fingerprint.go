package sync

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
)

// The schema names are part of every fingerprint. Change one only to force a
// new baseline for every workspace.
const (
	variationFingerprintSchema  = "launchdarkly.config.variation/v1"
	attachmentFingerprintSchema = "launchdarkly.config.attachment/v1"
)

// FingerprintVariation returns a stable hash of the variation content that
// sync owns. Two variations with the same behavior have the same fingerprint.
func FingerprintVariation(projectKey, lookupKey string, variation Variation) (string, error) {
	if err := variation.Validate(); err != nil {
		return "", err
	}

	canonical := variation.canonical()
	return fingerprint("variation", struct {
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
		Variation:    canonical,
		Attachments:  canonical.Attachments,
	})
}

// FingerprintAttachment returns a stable hash of the content of one tool or
// skill in a project. The runtime version does not change the hash.
func FingerprintAttachment(projectKey string, attachment Attachment) (string, error) {
	if !attachment.Kind.Valid() {
		return "", fmt.Errorf("unsupported attachment kind %q", attachment.Kind)
	}
	if attachment.Key() == "" {
		return "", fmt.Errorf("%s key is required", attachment.Kind)
	}

	return fingerprint(string(attachment.Kind), struct {
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
	})
}

// canonical returns a copy that keeps only the content that changes behavior.
// It does not change the slices or maps of the original variation.
func (variation Variation) canonical() Variation {
	variation.Tools = unversionedRefs(variation.Tools)
	variation.Skills = unversionedRefs(variation.Skills)
	variation.Attachments = slices.Clone(variation.Attachments)
	for index := range variation.Attachments {
		variation.Attachments[index] = CanonicalAttachment(variation.Attachments[index])
	}
	variation.SortAttachments()
	variation.Model = canonicalModel(variation.Model)

	switch variation.Mode {
	case VariationModeAgent:
		variation.Instructions = NormalizePromptText(variation.Instructions)
		variation.Messages = nil
	case VariationModeCompletion:
		variation.Instructions = ""
		messages := make([]Message, 0, len(variation.Messages))
		for _, message := range variation.Messages {
			messages = append(messages, Message{Role: message.Role, Content: NormalizePromptText(message.Content)})
		}
		variation.Messages = nil
		if len(messages) != 0 {
			variation.Messages = messages
		}
	}
	return variation
}

// unversionedRefs copies references without their runtime version.
func unversionedRefs(refs []AttachmentRef) []AttachmentRef {
	if refs == nil {
		return nil
	}
	unversioned := make([]AttachmentRef, len(refs))
	for index, ref := range refs {
		unversioned[index] = AttachmentRef{Key: ref.Key}
	}
	return unversioned
}

// canonicalModel removes the empty "parameters" and "custom" objects that the
// variation API adds. Other empty objects can change behavior, so they remain.
func canonicalModel(model map[string]any) map[string]any {
	model = maps.Clone(model)
	for _, key := range []string{"parameters", "custom"} {
		if value, ok := model[key].(map[string]any); ok && len(value) == 0 {
			delete(model, key)
		}
	}
	if len(model) == 0 {
		return nil
	}
	return model
}

func fingerprint(resource string, value any) (string, error) {
	canonical, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("encode %s fingerprint: %w", resource, err)
	}
	sum := sha256.Sum256(canonical)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}
