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

	fields := variationDiffFields{}
	addField := func(name string, before, after json.RawMessage) {
		if string(before) != string(after) {
			fields[name] = variationFieldDiff{Before: before, After: after}
		}
	}
	addField("variation", variationJSON(before), variationJSON(after))
	beforeTools, beforeSkills := attachmentJSON(before)
	afterTools, afterSkills := attachmentJSON(after)
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
	withoutRefs.Tools, withoutRefs.Skills = nil, nil
	data, _ := json.Marshal(withoutRefs)
	return data
}

// attachmentJSON returns the canonical content of the tools and of the skills.
func attachmentJSON(variation *syncdomain.Variation) (tools, skills json.RawMessage) {
	if variation == nil {
		return nil, nil
	}
	var toolContent []syncdomain.Tool
	var skillContent []syncdomain.Skill
	for _, attachment := range variation.Attachments {
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
// between the server and the local variation.
func changedAttachmentIDs(resource PlannedResource) []ResourceID {
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
