// Package manifest records the state of each resource after the last
// successful sync. Sync compares local files and LaunchDarkly with this
// baseline to find which side changed.
package manifest

import (
	"fmt"
	"regexp"
	"slices"
	"strings"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
)

var fingerprintPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

// Manifest is the baseline of every tracked resource.
type Manifest struct {
	Resources []Resource
}

// Resource is the baseline of one resource. The YAML tags are the format of
// the sync.lock file.
type Resource struct {
	ResourceKind syncdomain.Kind `yaml:"kind"`
	ProjectKey   string          `yaml:"project"`
	LookupKey    string          `yaml:"key"`
	Fingerprint  string          `yaml:"fingerprint"`
	// Version is the version of the remote manifest entry, which LaunchDarkly
	// uses for optimistic locking. Only the remote manifest has it. The lock
	// does not store it, so that a lock entry changes only with its content.
	Version int `yaml:"-"`
	// Ref is the linked prompt file of a variation. Only sync.lock stores it,
	// so that sync can restore a missing variation file with its link.
	Ref *syncdomain.Reference `yaml:"ref,omitempty"`
}

// ID returns the identity of the resource.
func (resource Resource) ID() syncdomain.ResourceID {
	return syncdomain.ResourceID{Kind: resource.ResourceKind, ProjectKey: resource.ProjectKey, LookupKey: resource.LookupKey}
}

// New returns an empty manifest.
func New() Manifest {
	return Manifest{Resources: []Resource{}}
}

// Clone returns a manifest that changes independently of the original.
func (manifest Manifest) Clone() Manifest {
	return Manifest{Resources: append([]Resource{}, manifest.Resources...)}
}

// SetFingerprint records the synchronized state of one resource.
func (manifest *Manifest) SetFingerprint(id syncdomain.ResourceID, fingerprint string) {
	if index := manifest.index(id); index >= 0 {
		manifest.Resources[index].Fingerprint = fingerprint
		return
	}
	manifest.Resources = append(manifest.Resources, Resource{
		ResourceKind: id.Kind,
		ProjectKey:   id.ProjectKey,
		LookupKey:    id.LookupKey,
		Fingerprint:  fingerprint,
	})
}

// Remove stops tracking one resource.
func (manifest *Manifest) Remove(id syncdomain.ResourceID) {
	if index := manifest.index(id); index >= 0 {
		manifest.Resources = slices.Delete(manifest.Resources, index, index+1)
	}
}

// SetRefs records the linked prompt file of each variation. A variation
// without a local file keeps its recorded link.
func (manifest *Manifest) SetRefs(variations []syncdomain.SyncedResource) {
	for _, variation := range variations {
		if index := manifest.index(variation.ID()); index >= 0 {
			manifest.Resources[index].Ref = variation.Ref
		}
	}
}

// Ref returns the recorded linked prompt file of a resource, or nil.
func (manifest Manifest) Ref(id syncdomain.ResourceID) *syncdomain.Reference {
	if index := manifest.index(id); index >= 0 {
		return manifest.Resources[index].Ref
	}
	return nil
}

// SetAttachments records the state of each tool and skill. Many variations
// can share one attachment, but the manifest tracks it once.
func (manifest *Manifest) SetAttachments(projectKey string, attachments []syncdomain.Attachment) error {
	return manifest.setAttachments(projectKey, attachments, true)
}

// SetAttachmentsIfMissing records only the attachments that the manifest does
// not track. An existing baseline does not move past an unsynchronized edit.
func (manifest *Manifest) SetAttachmentsIfMissing(projectKey string, attachments []syncdomain.Attachment) error {
	return manifest.setAttachments(projectKey, attachments, false)
}

func (manifest *Manifest) setAttachments(projectKey string, attachments []syncdomain.Attachment, overwrite bool) error {
	for _, attachment := range attachments {
		id := attachment.ID(projectKey)
		if !overwrite && manifest.index(id) >= 0 {
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

// RemoveUnusedAttachments stops tracking each tool and skill that none of the
// variations references.
func (manifest *Manifest) RemoveUnusedAttachments(variations []syncdomain.SyncedResource) {
	used := make(map[syncdomain.ResourceID]struct{})
	for _, variation := range variations {
		for _, attachment := range variation.Variation.Attachments {
			used[attachment.ID(variation.ProjectKey)] = struct{}{}
		}
	}
	manifest.Resources = slices.DeleteFunc(manifest.Resources, func(resource Resource) bool {
		if resource.ResourceKind == syncdomain.KindVariation {
			return false
		}
		_, ok := used[resource.ID()]
		return !ok
	})
}

// WithChanges returns a copy of the manifest with the entry changes from
// before to after. An entry that is the same in before and after keeps the
// value that the manifest has, which can come from another working copy.
func (manifest Manifest) WithChanges(before, after Manifest) Manifest {
	result := manifest.Clone()
	for _, resource := range after.Resources {
		if index := before.index(resource.ID()); index < 0 || before.Resources[index].Fingerprint != resource.Fingerprint {
			result.SetFingerprint(resource.ID(), resource.Fingerprint)
		}
	}
	for _, resource := range before.Resources {
		if after.index(resource.ID()) < 0 {
			result.Remove(resource.ID())
		}
	}
	return result
}

// WithVersionsFrom returns a copy of the manifest in which each entry has the
// version of the same entry in source, or 0 if source does not have it.
func (manifest Manifest) WithVersionsFrom(source Manifest) Manifest {
	versions := make(map[syncdomain.ResourceID]int, len(source.Resources))
	for _, resource := range source.Resources {
		versions[resource.ID()] = resource.Version
	}
	result := manifest.Clone()
	for index := range result.Resources {
		result.Resources[index].Version = versions[result.Resources[index].ID()]
	}
	return result
}

// ProjectKeys returns each project that has an entry, in order.
func (manifest Manifest) ProjectKeys() []string {
	keys := make([]string, 0, len(manifest.Resources))
	for _, resource := range manifest.Resources {
		keys = append(keys, resource.ProjectKey)
	}
	return uniqueSorted(keys)
}

// Validate makes sure that each entry has a safe identity, a valid
// fingerprint, and a unique identity.
func (manifest Manifest) Validate() error {
	seen := make(map[syncdomain.ResourceID]struct{}, len(manifest.Resources))
	for _, resource := range manifest.Resources {
		if err := syncdomain.ValidateKey(string(resource.ResourceKind)); err != nil {
			return fmt.Errorf("invalid resource kind %q: %w", resource.ResourceKind, err)
		}
		if err := syncdomain.ValidateKey(resource.ProjectKey); err != nil {
			return fmt.Errorf("invalid project key %q: %w", resource.ProjectKey, err)
		}
		for _, segment := range strings.Split(resource.LookupKey, "/") {
			if err := syncdomain.ValidateKey(segment); err != nil {
				return fmt.Errorf("invalid lookup key %q: %w", resource.LookupKey, err)
			}
		}

		id := resource.ID()
		switch {
		case !fingerprintPattern.MatchString(resource.Fingerprint):
			return fmt.Errorf("invalid fingerprint for %s", id)
		case resource.Version < 0:
			return fmt.Errorf("invalid version for %s", id)
		}
		if _, duplicate := seen[id]; duplicate {
			return fmt.Errorf("duplicate manifest resource %s", id)
		}
		seen[id] = struct{}{}
	}
	return nil
}

// Sort orders the entries by identity.
func (manifest *Manifest) Sort() {
	slices.SortFunc(manifest.Resources, func(left, right Resource) int {
		return syncdomain.CompareResourceIDs(left.ID(), right.ID())
	})
}

func (manifest Manifest) index(id syncdomain.ResourceID) int {
	return slices.IndexFunc(manifest.Resources, func(resource Resource) bool { return resource.ID() == id })
}
