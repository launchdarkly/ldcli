package prompt

import (
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

// attachOptions are the input of one attach. An empty field means that the
// user did not give the value. In interactive mode, the user then chooses it.
type attachOptions struct {
	RepositoryRoot string
	ProjectKey     string
	// VariationID is the lookup key of the variation, "config-key/variation-key".
	VariationID string
	Kind        syncdomain.AttachmentKind
	Key         string
	Interactive bool
	Input       io.Reader
	Output      io.Writer
}

// attachToVariation adds a tool or a skill to one local variation, and writes
// the file of the attachment. The next sync applies the change.
func attachToVariation(store synclocal.Store, client syncapi.Client, options attachOptions) error {
	variations, err := synclocal.CompileWorkspace(options.RepositoryRoot)
	if err != nil {
		return err
	}
	projectKey, err := selectAttachmentProject(variations, options)
	if err != nil {
		return err
	}
	target, err := selectManagedVariation(variations, projectKey, options)
	if err != nil {
		return err
	}
	attachment, err := selectAttachment(client, projectKey, options)
	if err != nil {
		return err
	}

	// Other local variations can share the attachment file. Keep its local
	// content, so that attach never overwrites a local edit. The next sync
	// shows a conflict if the content differs from LaunchDarkly.
	if local, ok := localAttachment(variations, attachment.ID(projectKey)); ok {
		attachment = local
	}
	variation := target.Variation
	variation.Attach(attachment)
	if err := variation.Validate(); err != nil {
		return err
	}

	configKey, _, err := target.ID().VariationKeys()
	if err != nil {
		return err
	}
	_, err = store.ReplaceVariations([]synclocal.VariationReplacement{{
		ProjectKey: projectKey, ConfigKey: configKey, Variation: variation,
	}})
	return err
}

// localAttachment finds the local content of an attachment.
func localAttachment(variations []syncdomain.SyncedResource, id ResourceID) (syncdomain.Attachment, bool) {
	for _, variation := range variations {
		if variation.ProjectKey != id.ProjectKey {
			continue
		}
		if attachment, ok := variation.Variation.Attachment(syncdomain.AttachmentKind(id.Kind), id.LookupKey); ok {
			return attachment, true
		}
	}
	return syncdomain.Attachment{}, false
}

// selectAttachmentProject returns the project of the attachment. A workspace
// with one project uses it. Otherwise the user names or chooses the project.
func selectAttachmentProject(variations []syncdomain.SyncedResource, options attachOptions) (string, error) {
	var projectKeys []string
	for _, variation := range variations {
		projectKeys = append(projectKeys, variation.ProjectKey)
	}
	slices.Sort(projectKeys)
	projectKeys = slices.Compact(projectKeys)

	switch {
	case options.ProjectKey != "" && slices.Contains(projectKeys, options.ProjectKey):
		return options.ProjectKey, nil
	case options.ProjectKey != "":
		return "", fmt.Errorf("project %q is not managed by this workspace", options.ProjectKey)
	case len(projectKeys) == 0:
		return "", errors.New("no synchronized projects are available")
	case len(projectKeys) == 1:
		return projectKeys[0], nil
	case !options.Interactive:
		return "", errors.New("--to is required when the workspace manages multiple projects")
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

// selectManagedVariation returns the local variation that gets the
// attachment. A skill needs an agent variation.
func selectManagedVariation(
	variations []syncdomain.SyncedResource,
	projectKey string,
	options attachOptions,
) (syncdomain.SyncedResource, error) {
	var choices []syncinteractive.Choice[syncdomain.SyncedResource]
	for _, variation := range variations {
		if variation.ProjectKey != projectKey {
			continue
		}
		if options.Kind == syncdomain.AttachmentSkill && variation.Variation.Mode != syncdomain.VariationModeAgent {
			if options.VariationID == variation.LookupKey {
				return syncdomain.SyncedResource{}, errors.New("skills can only be attached to agent-mode configs")
			}
			continue
		}
		choices = append(choices, syncinteractive.Choice[syncdomain.SyncedResource]{
			Title: variation.Variation.Name, Description: "Key: " + variation.LookupKey, Value: variation,
		})
	}

	if options.VariationID != "" {
		for _, choice := range choices {
			if choice.Value.LookupKey == options.VariationID {
				return choice.Value, nil
			}
		}
		return syncdomain.SyncedResource{}, fmt.Errorf("variation %q is not managed in project %q", options.VariationID, projectKey)
	}
	if !options.Interactive {
		return syncdomain.SyncedResource{}, errors.New("--to is required without interactive input")
	}
	if len(choices) == 0 {
		return syncdomain.SyncedResource{}, fmt.Errorf("no eligible variations are managed in project %q", projectKey)
	}

	title := fmt.Sprintf("Choose a synced variation to attach the %s to", options.Kind)
	selected, canceled, err := syncinteractive.Select(options.Input, options.Output, title, choices)
	if canceled {
		return syncdomain.SyncedResource{}, errAttachmentCanceled
	}
	return selected, err
}

// selectAttachment reads the attachment that the user named, or asks the user
// to search for one. It returns the latest version.
func selectAttachment(client syncapi.Client, projectKey string, options attachOptions) (syncdomain.Attachment, error) {
	if options.Key != "" {
		return client.ReadAttachment(projectKey, options.Kind, options.Key)
	}
	if !options.Interactive {
		return syncdomain.Attachment{}, fmt.Errorf("%s key is required without interactive input", options.Kind)
	}

	kind := string(options.Kind)
	attachment, canceled, err := syncinteractive.SearchSelect(syncinteractive.SearchOptions[syncdomain.Attachment]{
		Input:             options.Input,
		Output:            options.Output,
		SearchTitle:       "Search " + kind + "s",
		SearchPlaceholder: "Name or key",
		SelectTitle:       "Choose a " + kind,
		ItemName:          kind + "s",
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
	limit, offset int,
) ([]syncdomain.Attachment, int, error) {
	page, err := client.SearchAttachments(projectKey, kind, query, limit, offset)
	return page.Items, page.TotalCount, err
}

// attachmentChoice shows the name of a skill, or the key of a tool, which has no name.
func attachmentChoice(attachment syncdomain.Attachment) syncinteractive.Choice[syncdomain.Attachment] {
	if attachment.Skill != nil {
		return syncinteractive.Choice[syncdomain.Attachment]{
			Title:       attachment.Skill.Name,
			Description: fmt.Sprintf("Key: %s · Latest version: %d", attachment.Key(), attachment.Version),
			Value:       attachment,
		}
	}
	return syncinteractive.Choice[syncdomain.Attachment]{
		Title:       "Key: " + attachment.Key(),
		Description: fmt.Sprintf("Latest version: %d", attachment.Version),
		Value:       attachment,
	}
}
