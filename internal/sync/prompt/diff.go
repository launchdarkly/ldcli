package prompt

import (
	"encoding/json"
	"slices"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
)

// variationFieldDiff is the JSON of one part of a variation before and after
// the sync. A nil value means that the part does not exist.
type variationFieldDiff struct {
	Before json.RawMessage `json:"before"`
	After  json.RawMessage `json:"after"`
}

// variationDiffFields maps a part name, such as "variation" or "tools", to its diff.
type variationDiffFields map[string]variationFieldDiff

// variationDiff returns the parts of the variation that differ. The tools and
// the skills have their own parts, so that the review shows their content.
func variationDiff(before, after *syncdomain.Variation) variationDiffFields {
	if before == nil && after == nil {
		return nil
	}
	var beforeAttachments, afterAttachments []syncdomain.Attachment
	if before != nil {
		beforeAttachments = before.Attachments
	}
	if after != nil {
		afterAttachments = after.Attachments
	}
	return diffParts(variationJSON(before), variationJSON(after), beforeAttachments, afterAttachments)
}

// attachmentDiff returns the "tools" or the "skills" part of one tool or skill
// row, so that the review shows it like the attachment of a variation.
func attachmentDiff(before, after *syncdomain.Attachment) variationDiffFields {
	var beforeAttachments, afterAttachments []syncdomain.Attachment
	if before != nil {
		beforeAttachments = []syncdomain.Attachment{*before}
	}
	if after != nil {
		afterAttachments = []syncdomain.Attachment{*after}
	}
	return diffParts(nil, nil, beforeAttachments, afterAttachments)
}

func projectionDiff(before, after syncdomain.Projection) variationDiffFields {
	beforeJSON, _ := json.Marshal(before)
	afterJSON, _ := json.Marshal(after)
	if string(beforeJSON) == string(afterJSON) {
		return nil
	}
	return variationDiffFields{"projection": {Before: beforeJSON, After: afterJSON}}
}

// diffParts returns each part that differs, or nil.
func diffParts(beforeVariation, afterVariation json.RawMessage, beforeAttachments, afterAttachments []syncdomain.Attachment) variationDiffFields {
	fields := variationDiffFields{}
	addField := func(name string, before, after json.RawMessage) {
		if string(before) != string(after) {
			fields[name] = variationFieldDiff{Before: before, After: after}
		}
	}
	addField("variation", beforeVariation, afterVariation)
	beforeTools, beforeSkills := attachmentJSON(beforeAttachments)
	afterTools, afterSkills := attachmentJSON(afterAttachments)
	addField("tools", beforeTools, afterTools)
	addField("skills", beforeSkills, afterSkills)

	if len(fields) == 0 {
		return nil
	}
	return fields
}

// variationJSON returns the variation without its attachment references. The
// "tools" and "skills" parts show those changes.
func variationJSON(variation *syncdomain.Variation) json.RawMessage {
	if variation == nil {
		return nil
	}
	withoutRefs := *variation
	withoutRefs.Model = syncdomain.CanonicalModel(withoutRefs.Model, withoutRefs.Tools)
	withoutRefs.Tools, withoutRefs.Skills = nil, nil
	data, _ := json.Marshal(withoutRefs)
	return data
}

// attachmentJSON returns the canonical content of the tools and of the skills.
func attachmentJSON(attachments []syncdomain.Attachment) (tools, skills json.RawMessage) {
	var toolContent []syncdomain.Tool
	var skillContent []syncdomain.Skill
	for _, attachment := range attachments {
		canonical := syncdomain.CanonicalAttachment(attachment)
		switch {
		case canonical.Tool != nil:
			toolContent = append(toolContent, *canonical.Tool)
		case canonical.Skill != nil:
			skillContent = append(skillContent, *canonical.Skill)
		}
	}
	if len(toolContent) != 0 {
		tools, _ = json.Marshal(toolContent)
	}
	if len(skillContent) != 0 {
		skills, _ = json.Marshal(skillContent)
	}
	return tools, skills
}

// attachmentPinDiff reports the references in a server variation that do not
// pin the latest version. It returns the current and the latest versions.
func attachmentPinDiff(variation *syncdomain.Variation) (current, latest json.RawMessage, stale bool) {
	if variation == nil {
		return nil, nil, false
	}
	currentPins := map[string]map[string]int{"tools": {}, "skills": {}}
	latestPins := map[string]map[string]int{"tools": {}, "skills": {}}
	for _, kind := range syncdomain.AttachmentKinds {
		group := string(kind) + "s"
		for _, ref := range variation.Refs(kind) {
			if attachment, ok := variation.Attachment(kind, ref.Key); ok && attachment.Version != ref.Version {
				currentPins[group][ref.Key] = ref.Version
				latestPins[group][ref.Key] = attachment.Version
				stale = true
			}
		}
	}
	if !stale {
		return nil, nil, false
	}
	current, _ = json.Marshal(currentPins)
	latest, _ = json.Marshal(latestPins)
	return current, latest, true
}

// changedAttachmentIDs returns the tools and skills whose content differs
// between the server and the local variation. A tool or skill row returns its
// own identity when its two sides differ, so that a conflict choice for it
// also covers each variation that changes the same content.
func changedAttachmentIDs(resource PlannedResource) []ResourceID {
	if resource.ID.Kind != syncdomain.KindVariation {
		if resource.LocalFingerprint != resource.ServerFingerprint {
			return []ResourceID{resource.ID}
		}
		return nil
	}
	before := attachmentsByID(resource.ID.ProjectKey, resource.Server)
	after := attachmentsByID(resource.ID.ProjectKey, resource.Local)

	var changed []ResourceID
	for id, attachment := range before {
		if other, exists := after[id]; !exists || !syncdomain.SameAttachmentContent(attachment, other) {
			changed = append(changed, id)
		}
	}
	for id := range after {
		if _, exists := before[id]; !exists {
			changed = append(changed, id)
		}
	}
	slices.SortFunc(changed, syncdomain.CompareResourceIDs)
	return changed
}

func attachmentsByID(projectKey string, variation *syncdomain.Variation) map[ResourceID]syncdomain.Attachment {
	attachments := map[ResourceID]syncdomain.Attachment{}
	if variation != nil {
		for _, attachment := range variation.Attachments {
			attachments[attachment.ID(projectKey)] = attachment
		}
	}
	return attachments
}
