package prompt

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
	syncapi "github.com/launchdarkly/ldcli/internal/sync/api"
	syncinteractive "github.com/launchdarkly/ldcli/internal/sync/interactive"
	synclocal "github.com/launchdarkly/ldcli/internal/sync/local"
)

const attachmentSearchPageSize = 25

var errAttachmentCanceled = errors.New("attachment selection canceled")

type attachOptions struct {
	RepositoryRoot string
	ProjectKey     string
	VariationID    string
	Kind           syncdomain.AttachmentKind
	Key            string
	Interactive    bool
	Input          io.Reader
	Output         io.Writer
}

// attachToVariation materializes the selected latest dependency and adds its
// key to one existing local variation. The normal sync pipeline applies it.
func attachToVariation(store synclocal.Store, client syncapi.Client, options attachOptions) error {
	resources, err := synclocal.CompileWorkspace(options.RepositoryRoot)
	if err != nil {
		return err
	}
	projectKey, err := selectAttachmentProject(resources, options)
	if err != nil {
		return err
	}
	variationID, variation, err := selectManagedVariation(resources, projectKey, options)
	if err != nil {
		return err
	}
	attachment, err := selectAttachment(client, projectKey, options)
	if err != nil {
		return err
	}
	// Attachment files are shared by every local consumer. Reuse existing
	// canonical content so attaching another variation never overwrites edits
	// before the normal sync conflict flow can review them.
	if local, ok := localAttachment(resources, projectKey, attachment.Kind, attachment.Key()); ok {
		attachment = local
	}

	addAttachmentContent(&variation, attachment)
	if err := variation.NormalizeAttachments(); err != nil {
		return err
	}
	configKey, _, err := splitVariationLookupKey(variationID)
	if err != nil {
		return err
	}
	return store.AttachVariation(projectKey, configKey, variation)
}

// localAttachment finds canonical content already shared by managed variations.
func localAttachment(
	resources []syncdomain.SyncedResource,
	projectKey string,
	kind syncdomain.AttachmentKind,
	key string,
) (syncdomain.Attachment, bool) {
	for _, resource := range resources {
		if resource.ProjectKey != projectKey {
			continue
		}
		for _, attachment := range resource.Attachments {
			if attachment.Kind == kind && attachment.Key() == key {
				return attachment, true
			}
		}
	}
	return syncdomain.Attachment{}, false
}

// selectAttachmentProject resolves an explicit project or guides the user
// through locally managed projects.
func selectAttachmentProject(resources []syncdomain.SyncedResource, options attachOptions) (string, error) {
	var projectKeys []string
	for _, resource := range resources {
		if resource.Kind == syncdomain.KindVariation && !slices.Contains(projectKeys, resource.ProjectKey) {
			projectKeys = append(projectKeys, resource.ProjectKey)
		}
	}
	slices.Sort(projectKeys)
	if options.ProjectKey != "" {
		if slices.Contains(projectKeys, options.ProjectKey) {
			return options.ProjectKey, nil
		}
		return "", fmt.Errorf("project %q is not managed by this workspace", options.ProjectKey)
	}
	switch len(projectKeys) {
	case 0:
		return "", fmt.Errorf("no synchronized projects are available")
	case 1:
		return projectKeys[0], nil
	}
	if !options.Interactive {
		return "", fmt.Errorf("--to is required when the workspace manages multiple projects")
	}

	choices := make([]syncinteractive.Choice[string], 0, len(projectKeys))
	for _, key := range projectKeys {
		choices = append(choices, syncinteractive.Choice[string]{Title: "Key: " + key, Value: key})
	}
	selected, canceled, err := syncinteractive.Select(options.Input, options.Output, "Choose a LaunchDarkly project", choices)
	if canceled {
		return "", errAttachmentCanceled
	}
	return selected, err
}

// selectAttachment resolves a direct key or searches the server catalog.
func selectAttachment(client syncapi.Client, projectKey string, options attachOptions) (syncdomain.Attachment, error) {
	if options.Key != "" {
		return client.ReadAttachment(projectKey, options.Kind, options.Key)
	}
	if !options.Interactive {
		return syncdomain.Attachment{}, fmt.Errorf("%s key is required without interactive input", options.Kind)
	}

	attachment, canceled, err := syncinteractive.SearchSelect(syncinteractive.SearchOptions[syncdomain.Attachment]{
		Input:             options.Input,
		Output:            options.Output,
		SearchTitle:       "Search " + string(options.Kind) + "s",
		SearchPlaceholder: "Name or key",
		SelectTitle:       "Choose a " + string(options.Kind),
		ItemName:          string(options.Kind) + "s",
		PageSize:          attachmentSearchPageSize,
		Fetch: func(query string, limit, offset int) ([]syncdomain.Attachment, int, error) {
			return searchAttachmentPage(client, projectKey, options.Kind, query, limit, offset)
		},
		Choice: attachmentChoice,
	})
	if err != nil {
		return syncdomain.Attachment{}, err
	}
	if canceled {
		return syncdomain.Attachment{}, errAttachmentCanceled
	}
	return client.ReadAttachment(projectKey, options.Kind, attachment.Key())
}

func searchAttachmentPage(
	client syncapi.Client,
	projectKey string,
	kind syncdomain.AttachmentKind,
	query string,
	limit int,
	offset int,
) ([]syncdomain.Attachment, int, error) {
	page, err := client.SearchAttachments(projectKey, kind, query, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	return page.Items, page.TotalCount, nil
}

func attachmentChoice(attachment syncdomain.Attachment) syncinteractive.Choice[syncdomain.Attachment] {
	title := "Key: " + attachment.Key()
	description := fmt.Sprintf("Latest version: %d", attachment.Version)
	if attachment.Skill != nil {
		title = attachment.Skill.Name
		description = fmt.Sprintf("Key: %s · Latest version: %d", attachment.Key(), attachment.Version)
	}
	return syncinteractive.Choice[syncdomain.Attachment]{Title: title, Description: description, Value: attachment}
}

// selectManagedVariation resolves an explicit consumer or prompts from
// variations already represented in the local workspace.
func selectManagedVariation(
	resources []syncdomain.SyncedResource,
	projectKey string,
	options attachOptions,
) (string, syncdomain.Variation, error) {
	choices := make([]syncinteractive.Choice[managedVariation], 0)
	for _, resource := range resources {
		if resource.Kind != syncdomain.KindVariation || resource.ProjectKey != projectKey {
			continue
		}
		var variation syncdomain.Variation
		if err := json.Unmarshal(resource.Payload, &variation); err != nil {
			return "", syncdomain.Variation{}, err
		}
		variation.Attachments = resource.Attachments
		if options.Kind == syncdomain.AttachmentSkill && variation.Mode != syncdomain.VariationModeAgent {
			if options.VariationID == resource.LookupKey {
				return "", syncdomain.Variation{}, fmt.Errorf("skills can only be attached to agent-mode configs")
			}
			continue
		}
		choices = append(choices, syncinteractive.Choice[managedVariation]{
			Title: variation.Name, Description: "Key: " + resource.LookupKey,
			Value: managedVariation{id: resource.LookupKey, variation: variation},
		})
	}

	if options.VariationID != "" {
		for _, choice := range choices {
			if choice.Value.id == options.VariationID {
				return choice.Value.id, choice.Value.variation, nil
			}
		}
		return "", syncdomain.Variation{}, fmt.Errorf("variation %q is not managed in project %q", options.VariationID, projectKey)
	}
	if !options.Interactive {
		return "", syncdomain.Variation{}, fmt.Errorf("--to is required without interactive input")
	}
	if len(choices) == 0 {
		return "", syncdomain.Variation{}, fmt.Errorf("no eligible variations are managed in project %q", projectKey)
	}

	title := fmt.Sprintf("Choose a synced variation to attach the %s to", options.Kind)
	selected, canceled, err := syncinteractive.Select(options.Input, options.Output, title, choices)
	if canceled {
		return "", syncdomain.Variation{}, errAttachmentCanceled
	}
	return selected.id, selected.variation, err
}

type managedVariation struct {
	id        string
	variation syncdomain.Variation
}

// addAttachmentContent adds one key and its canonical dependency content.
func addAttachmentContent(variation *syncdomain.Variation, attachment syncdomain.Attachment) {
	switch attachment.Kind {
	case syncdomain.AttachmentTool:
		addAttachmentRefByKind(&variation.Tools, syncdomain.AttachmentRef{Key: attachment.Key()})
	case syncdomain.AttachmentSkill:
		addAttachmentRefByKind(&variation.Skills, syncdomain.AttachmentRef{Key: attachment.Key()})
	}
	variation.SetAttachment(attachment)
}

// addAttachmentRefByKind inserts a key once while preserving current order.
func addAttachmentRefByKind(refs *[]syncdomain.AttachmentRef, ref syncdomain.AttachmentRef) {
	if slices.ContainsFunc(*refs, func(existing syncdomain.AttachmentRef) bool { return existing.Key == ref.Key }) {
		return
	}
	*refs = append(*refs, ref)
}
