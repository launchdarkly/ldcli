package manifest

import (
	"fmt"
	"regexp"
	"slices"
	"strings"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
)

var fingerprintPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

// Manifest records the common resource state accepted by the last successful
// synchronization.
type Manifest struct {
	Resources []Resource
}

// Resource identifies one tracked resource and its last synchronized
// fingerprint.
type Resource struct {
	ResourceKind syncdomain.Kind
	ProjectKey   string
	LookupKey    string
	Fingerprint  string
	Version      int
}

// ID returns the common identity represented by this manifest entry.
func (resource Resource) ID() syncdomain.ResourceID {
	return syncdomain.ResourceID{Kind: resource.ResourceKind, ProjectKey: resource.ProjectKey, LookupKey: resource.LookupKey}
}

// New returns an empty current-version manifest.
func New() Manifest {
	return Manifest{Resources: []Resource{}}
}

// SetFingerprint records the last synchronized state for one resource.
func (manifest *Manifest) SetFingerprint(id syncdomain.ResourceID, fingerprint string) {
	for index := range manifest.Resources {
		if manifest.Resources[index].ID() == id {
			manifest.Resources[index].Fingerprint = fingerprint
			return
		}
	}
	manifest.Resources = append(manifest.Resources, Resource{
		ResourceKind: id.Kind,
		ProjectKey:   id.ProjectKey,
		LookupKey:    id.LookupKey,
		Fingerprint:  fingerprint,
	})
}

// SetAttachments records the canonical state of shared dependencies once,
// regardless of how many variations reference them.
func (manifest *Manifest) SetAttachments(projectKey string, attachments []syncdomain.Attachment) error {
	return manifest.setAttachments(projectKey, attachments, true)
}

// SetAttachmentsIfMissing establishes baselines for newly tracked
// dependencies without advancing existing baselines past unsynchronized edits.
func (manifest *Manifest) SetAttachmentsIfMissing(projectKey string, attachments []syncdomain.Attachment) error {
	return manifest.setAttachments(projectKey, attachments, false)
}

// setAttachments writes canonical dependency baselines with explicit overwrite behavior.
func (manifest *Manifest) setAttachments(projectKey string, attachments []syncdomain.Attachment, overwrite bool) error {
	for _, attachment := range attachments {
		id := syncdomain.ResourceID{
			Kind:       syncdomain.Kind(attachment.Kind),
			ProjectKey: projectKey,
			LookupKey:  attachment.Key(),
		}
		if !overwrite && manifest.has(id) {
			continue
		}
		fingerprint, err := syncdomain.FingerprintAttachment(projectKey, attachment)
		if err != nil {
			return err
		}
		manifest.SetFingerprint(id, fingerprint)
	}
	return nil
}

// has reports whether one resource identity is already tracked.
func (manifest Manifest) has(id syncdomain.ResourceID) bool {
	return slices.ContainsFunc(manifest.Resources, func(resource Resource) bool { return resource.ID() == id })
}

// RemoveUnreferencedAttachments removes dependency baselines that no managed
// variation references after a successful synchronization.
func (manifest *Manifest) RemoveUnreferencedAttachments(referenced map[syncdomain.ResourceID]struct{}) {
	manifest.Resources = slices.DeleteFunc(manifest.Resources, func(resource Resource) bool {
		if resource.ResourceKind != syncdomain.KindTool && resource.ResourceKind != syncdomain.KindSkill {
			return false
		}
		_, ok := referenced[resource.ID()]
		return !ok
	})
}

// Remove deletes one resource from the manifest.
func (manifest *Manifest) Remove(id syncdomain.ResourceID) {
	for index, resource := range manifest.Resources {
		if resource.ID() == id {
			manifest.Resources = append(manifest.Resources[:index], manifest.Resources[index+1:]...)
			return
		}
	}
}

// Validate checks the manifest schema and resource identities.
func (manifest Manifest) Validate() error {
	seen := make(map[syncdomain.ResourceID]struct{}, len(manifest.Resources))
	for _, resource := range manifest.Resources {
		if err := validatePathSegment("resource kind", string(resource.ResourceKind)); err != nil {
			return err
		}
		if err := validatePathSegment("project key", resource.ProjectKey); err != nil {
			return err
		}
		if err := validateLookupKey(resource.LookupKey); err != nil {
			return err
		}
		if !fingerprintPattern.MatchString(resource.Fingerprint) {
			return fmt.Errorf("invalid fingerprint for %s/%s", resource.ProjectKey, resource.LookupKey)
		}
		if resource.Version < 0 {
			return fmt.Errorf("invalid version for %s/%s", resource.ProjectKey, resource.LookupKey)
		}

		identity := resource.ID()
		if _, exists := seen[identity]; exists {
			return fmt.Errorf("duplicate manifest resource %s/%s", resource.ProjectKey, resource.LookupKey)
		}
		seen[identity] = struct{}{}
	}
	return nil
}

// Sort orders resources deterministically for stable Git diffs.
func (manifest *Manifest) Sort() {
	slices.SortFunc(manifest.Resources, func(left, right Resource) int {
		if result := strings.Compare(string(left.ResourceKind), string(right.ResourceKind)); result != 0 {
			return result
		}
		if result := strings.Compare(left.ProjectKey, right.ProjectKey); result != 0 {
			return result
		}
		return strings.Compare(left.LookupKey, right.LookupKey)
	})
}

// validateLookupKey checks every slash-delimited resource identity segment.
func validateLookupKey(value string) error {
	for _, segment := range strings.Split(value, "/") {
		if err := validatePathSegment("lookup segment", segment); err != nil {
			return fmt.Errorf("invalid lookup key %q: %w", value, err)
		}
	}
	return nil
}

// validatePathSegment rejects values that are empty, unsafe, or non-portable.
func validatePathSegment(name, value string) error {
	if value == "" || value == "." || value == ".." || strings.ContainsAny(value, `/\`) || strings.IndexByte(value, 0) >= 0 {
		return fmt.Errorf("invalid %s %q", name, value)
	}
	return nil
}
