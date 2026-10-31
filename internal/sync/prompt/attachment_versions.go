package prompt

import (
	"errors"
	"fmt"
	"slices"

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

// settle records the result of a tool or skill row. A pull settles the
// LaunchDarkly content. A push settles through resolve.
func (resolver *attachmentResolver) settle(id ResourceID, attachment syncdomain.Attachment, err error) {
	if err != nil {
		resolver.failures[id] = err
		return
	}
	resolver.resolved[id] = attachment
}

// settledRow returns the variation row with the content that this execution
// already gave each of its tools and skills. Without it, a variation pull
// writes the old LaunchDarkly copy over a tool that a row before it pushed,
// and a variation push publishes the old local copy over a tool that a row
// before it pulled. If a tool or skill of the row failed, the row fails too.
func (resolver *attachmentResolver) settledRow(resource PlannedResource) (PlannedResource, error) {
	side, fingerprint := &resource.Local, &resource.LocalFingerprint
	localSide := true
	switch {
	case resource.Action == ActionUpdateLocal:
		side, fingerprint = &resource.Server, &resource.ServerFingerprint
		localSide = false
	case !resource.Action.changesServer():
		return resource, nil
	}
	if *side == nil {
		return resource, nil
	}
	for _, attachment := range (*side).Attachments {
		if err, failed := resolver.failures[attachment.ID(resource.ID.ProjectKey)]; failed {
			return resource, err
		}
	}

	attachments, changed := resolver.settledAttachments(resource.ID.ProjectKey, (*side).Attachments)
	if !changed {
		return resource, nil
	}
	variation := **side
	variation.Attachments = attachments
	var value string
	var err error
	if resource.Source != nil {
		projection, projectionErr := syncdomain.ProjectVariation(variation, *resource.Source)
		if projectionErr != nil {
			return resource, projectionErr
		}
		value, err = syncdomain.FingerprintProjection(resource.ID, projection)
		if localSide {
			resource.LocalProjection = projection
		} else {
			resource.ServerProjection = projection
		}
	} else {
		value, err = syncdomain.FingerprintVariation(resource.ID.ProjectKey, resource.ID.LookupKey, variation)
	}
	if err != nil {
		return resource, err
	}
	*side, *fingerprint = &variation, value
	return resource, nil
}

// settledAttachments returns a copy of the attachments in which each one that
// this execution settled has the settled content. It reports whether any
// content changed. Each attachment keeps its upsert flag.
func (resolver *attachmentResolver) settledAttachments(
	projectKey string,
	attachments []syncdomain.Attachment,
) ([]syncdomain.Attachment, bool) {
	result := slices.Clone(attachments)
	changed := false
	for index, attachment := range result {
		if settled, ok := resolver.resolved[attachment.ID(projectKey)]; ok && !syncdomain.SameAttachmentContent(settled, attachment) {
			settled.Upsert = attachment.Upsert
			result[index], changed = settled, true
		}
	}
	return result, changed
}

// resolveVariation returns a copy of the variation that pins the published
// version of each attachment.
func (resolver *attachmentResolver) resolveVariation(projectKey string, variation syncdomain.Variation) (syncdomain.Variation, error) {
	return variation.PinAttachments(func(local syncdomain.Attachment) (int, error) {
		published, err := resolver.resolve(projectKey, local, false)
		return published.Version, err
	})
}

// resolve returns the LaunchDarkly attachment that has the local content. If
// the content differs, resolve writes it and reads the new version. create is
// true when the plan found no copy in LaunchDarkly, and then resolve never
// updates a copy with other content.
func (resolver *attachmentResolver) resolve(projectKey string, local syncdomain.Attachment, create bool) (syncdomain.Attachment, error) {
	id := local.ID(projectKey)
	if err, failed := resolver.failures[id]; failed {
		return syncdomain.Attachment{}, err
	}
	if attachment, ok := resolver.resolved[id]; ok {
		return attachment, nil
	}

	attachment, err := resolver.publish(projectKey, local, create)
	if err != nil {
		resolver.failures[id] = err
		return syncdomain.Attachment{}, err
	}
	resolver.resolved[id] = attachment
	return attachment, nil
}

func (resolver *attachmentResolver) publish(projectKey string, local syncdomain.Attachment, create bool) (syncdomain.Attachment, error) {
	remote, readErr := resolver.client.ReadAttachment(projectKey, local.Kind, local.Key())
	var writeErr error
	switch {
	case readErr == nil && syncdomain.SameAttachmentContent(local, remote):
		return remote, nil
	case readErr == nil && create:
		// Another working copy created it after the plan.
		return syncdomain.Attachment{}, fmt.Errorf("%s %q changed concurrently", local.Kind, local.Key())
	case readErr == nil:
		writeErr = resolver.client.UpdateAttachment(projectKey, local)
	case syncapi.IsNotFound(readErr) && local.Upsert:
		writeErr = resolver.client.CreateAttachment(projectKey, local)
	case syncapi.IsNotFound(readErr):
		return syncdomain.Attachment{}, errors.New(missingAttachment(local.ID(projectKey)))
	default:
		return syncdomain.Attachment{}, readErr
	}
	// A 409 from a create means that the attachment exists now, for example
	// because another working copy created it. The read below decides.
	createConflict := readErr != nil && syncapi.IsConflict(writeErr)
	if writeErr != nil && !syncapi.MutationMayHaveSucceeded(writeErr) && !createConflict {
		return syncdomain.Attachment{}, writeErr
	}

	// LaunchDarkly assigns the version, so read the attachment again. The read
	// also finds the result of a write whose response was lost.
	observed, err := resolver.client.ReadAttachment(projectKey, local.Kind, local.Key())
	if err != nil {
		return syncdomain.Attachment{}, errors.Join(writeErr, err)
	}
	if !syncdomain.SameAttachmentContent(local, observed) {
		switch {
		case writeErr == nil:
			writeErr = fmt.Errorf("%s %q changed concurrently", local.Kind, local.Key())
		case createConflict:
			writeErr = fmt.Errorf("%s %q changed concurrently: %w", local.Kind, local.Key(), writeErr)
		}
		return syncdomain.Attachment{}, writeErr
	}
	return observed, nil
}
