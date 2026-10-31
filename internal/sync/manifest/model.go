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

// FormatVersion is the local sync manifest and sync.lock schema.
const FormatVersion = 1

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
	// FingerprintSchema identifies the meaning of a descriptor-backed fingerprint.
	FingerprintSchema string `yaml:"fingerprintSchema,omitempty"`
	// ReferenceDigest detects a descriptor mapping change.
	ReferenceDigest string `yaml:"referenceDigest,omitempty"`
	// AdapterBehaviorVersion and AdapterBehaviorDigest detect adapter changes.
	AdapterBehaviorVersion int    `yaml:"adapterBehaviorVersion,omitempty"`
	AdapterBehaviorDigest  string `yaml:"adapterBehaviorDigest,omitempty"`
	// IndexedSelections detect a changed item at an indexed source path.
	IndexedSelections []syncdomain.IndexedSelection `yaml:"indexedSelections,omitempty"`
	// Version is the version of the remote manifest entry, which LaunchDarkly
	// uses for optimistic locking. Only the remote manifest has it. The lock
	// does not store it, so that a lock entry changes only with its content.
	Version int `yaml:"-"`
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

// SetSourceMetadata records generated metadata for a descriptor-backed resource.
func (manifest *Manifest) SetSourceMetadata(id syncdomain.ResourceID, reference *syncdomain.Reference) error {
	index := manifest.index(id)
	if index < 0 {
		return nil
	}
	resource := &manifest.Resources[index]
	if reference == nil {
		resource.FingerprintSchema = ""
		resource.ReferenceDigest = ""
		resource.AdapterBehaviorVersion = 0
		resource.AdapterBehaviorDigest = ""
		resource.IndexedSelections = nil
		return nil
	}
	digest, err := syncdomain.ReferenceDigest(*reference)
	if err != nil {
		return err
	}
	resource.FingerprintSchema = syncdomain.ProjectionFingerprintSchema
	resource.ReferenceDigest = digest
	resource.AdapterBehaviorVersion = syncdomain.AdapterBehaviorVersion
	resource.AdapterBehaviorDigest = syncdomain.AdapterBehaviorDigest
	resource.IndexedSelections = nil
	return nil
}

// SetIndexedSelections records the indexed source items for one resource.
func (manifest *Manifest) SetIndexedSelections(
	id syncdomain.ResourceID,
	selections []syncdomain.IndexedSelection,
) {
	index := manifest.index(id)
	if index < 0 {
		return
	}
	selections = slices.Clone(selections)
	slices.SortFunc(selections, func(left, right syncdomain.IndexedSelection) int {
		return strings.Compare(left.Path, right.Path)
	})
	manifest.Resources[index].IndexedSelections = selections
}

// Remove stops tracking one resource.
func (manifest *Manifest) Remove(id syncdomain.ResourceID) {
	if index := manifest.index(id); index >= 0 {
		manifest.Resources = slices.Delete(manifest.Resources, index, index+1)
	}
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
		case resource.FingerprintSchema != "" && resource.FingerprintSchema != syncdomain.ProjectionFingerprintSchema:
			return fmt.Errorf("unsupported fingerprint schema %q for %s", resource.FingerprintSchema, id)
		case resource.FingerprintSchema != "" &&
			(resource.ReferenceDigest == "" || resource.AdapterBehaviorVersion == 0 || resource.AdapterBehaviorDigest == ""):
			return fmt.Errorf("incomplete source metadata for %s", id)
		case resource.FingerprintSchema == "" &&
			(resource.ReferenceDigest != "" || resource.AdapterBehaviorVersion != 0 ||
				resource.AdapterBehaviorDigest != "" || len(resource.IndexedSelections) != 0):
			return fmt.Errorf("source metadata without a fingerprint schema for %s", id)
		case resource.ReferenceDigest != "" && !fingerprintPattern.MatchString(resource.ReferenceDigest):
			return fmt.Errorf("invalid reference digest for %s", id)
		case resource.AdapterBehaviorDigest != "" && !fingerprintPattern.MatchString(resource.AdapterBehaviorDigest):
			return fmt.Errorf("invalid adapter behavior digest for %s", id)
		case resource.AdapterBehaviorVersion < 0:
			return fmt.Errorf("invalid adapter behavior version for %s", id)
		case resource.Version < 0:
			return fmt.Errorf("invalid version for %s", id)
		}
		for index, selection := range resource.IndexedSelections {
			switch {
			case selection.Path == "":
				return fmt.Errorf("indexed selection path is empty for %s", id)
			case selection.CollectionLength <= 0:
				return fmt.Errorf("invalid indexed collection length for %s", id)
			case !fingerprintPattern.MatchString(selection.Fingerprint):
				return fmt.Errorf("invalid indexed selection fingerprint for %s", id)
			case index > 0 && resource.IndexedSelections[index-1].Path >= selection.Path:
				return fmt.Errorf("indexed selections are not in path order for %s", id)
			}
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
