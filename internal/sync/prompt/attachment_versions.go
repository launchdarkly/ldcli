package prompt

import (
	"errors"
	"fmt"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
	syncapi "github.com/launchdarkly/ldcli/internal/sync/api"
)

// attachmentResolver publishes the local content of each tool and skill, and
// returns the LaunchDarkly version that has that content. A variation must
// pin that version when sync writes it.
//
// Many variations can share an attachment. The resolver keeps each result and
// each failure, so that it writes an attachment at most once in one execution.
type attachmentResolver struct {
	client   syncapi.Client
	resolved map[ResourceID]syncdomain.Attachment
	failures map[ResourceID]error
}

func newAttachmentResolver(client syncapi.Client) *attachmentResolver {
	return &attachmentResolver{
		client:   client,
		resolved: make(map[ResourceID]syncdomain.Attachment),
		failures: make(map[ResourceID]error),
	}
}

// resolveVariation returns a copy of the variation that pins the published
// version of each attachment.
func (resolver *attachmentResolver) resolveVariation(projectKey string, variation syncdomain.Variation) (syncdomain.Variation, error) {
	return variation.PinAttachments(func(local syncdomain.Attachment) (int, error) {
		published, err := resolver.resolve(projectKey, local)
		return published.Version, err
	})
}

// resolve returns the LaunchDarkly attachment that has the local content. If
// the content differs, resolve writes it and reads the new version.
func (resolver *attachmentResolver) resolve(projectKey string, local syncdomain.Attachment) (syncdomain.Attachment, error) {
	id := local.ID(projectKey)
	if err, failed := resolver.failures[id]; failed {
		return syncdomain.Attachment{}, err
	}
	if attachment, ok := resolver.resolved[id]; ok {
		return attachment, nil
	}

	attachment, err := resolver.publish(projectKey, local)
	if err != nil {
		resolver.failures[id] = err
		return syncdomain.Attachment{}, err
	}
	resolver.resolved[id] = attachment
	return attachment, nil
}

func (resolver *attachmentResolver) publish(projectKey string, local syncdomain.Attachment) (syncdomain.Attachment, error) {
	remote, err := resolver.client.ReadAttachment(projectKey, local.Kind, local.Key())
	var writeErr error
	switch {
	case err == nil && syncdomain.SameAttachmentContent(local, remote):
		return remote, nil
	case err == nil:
		writeErr = resolver.client.UpdateAttachment(projectKey, local)
	case syncapi.IsNotFound(err) && local.Kind == syncdomain.AttachmentTool && local.Upsert:
		writeErr = resolver.client.CreateAttachment(projectKey, local)
	case syncapi.IsNotFound(err) && local.Kind == syncdomain.AttachmentTool:
		return syncdomain.Attachment{}, fmt.Errorf(
			"tool %q does not exist in LaunchDarkly; add \"upsert\": true to its local JSON file to create it during sync",
			local.Key(),
		)
	default:
		return syncdomain.Attachment{}, err
	}
	if writeErr != nil && !syncapi.MutationMayHaveSucceeded(writeErr) {
		return syncdomain.Attachment{}, writeErr
	}

	// LaunchDarkly assigns the version, so read the attachment again. The read
	// also finds the result of a write whose response was lost.
	observed, err := resolver.client.ReadAttachment(projectKey, local.Kind, local.Key())
	if err != nil {
		return syncdomain.Attachment{}, errors.Join(writeErr, err)
	}
	if !syncdomain.SameAttachmentContent(local, observed) {
		if writeErr == nil {
			writeErr = fmt.Errorf("%s %q changed concurrently", local.Kind, local.Key())
		}
		return syncdomain.Attachment{}, writeErr
	}
	return observed, nil
}
