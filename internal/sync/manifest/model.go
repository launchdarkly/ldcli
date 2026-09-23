package manifest

import (
	"fmt"
	"regexp"
	"slices"
	"strings"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
)

// FormatVersion is the current manifest schema version.
const FormatVersion = 1

var fingerprintPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

// Manifest records the common resource state accepted by the last successful
// synchronization.
type Manifest struct {
	FormatVersion int        `yaml:"formatVersion"`
	Resources     []Resource `yaml:"resources"`
}

// Resource identifies one tracked resource and its last synchronized
// fingerprint.
type Resource struct {
	ResourceKind syncdomain.Kind `yaml:"resourceKind"`
	ProjectKey   string          `yaml:"projectKey"`
	LookupKey    string          `yaml:"lookupKey"`
	Fingerprint  string          `yaml:"fingerprint"`
}

// ID returns the common identity represented by this manifest entry.
func (resource Resource) ID() syncdomain.ResourceID {
	return syncdomain.ResourceID{Kind: resource.ResourceKind, ProjectKey: resource.ProjectKey, LookupKey: resource.LookupKey}
}

// New returns an empty current-version manifest.
func New() Manifest {
	return Manifest{FormatVersion: FormatVersion, Resources: []Resource{}}
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
	if manifest.FormatVersion != FormatVersion {
		return fmt.Errorf("unsupported manifest formatVersion %d", manifest.FormatVersion)
	}

	seen := make(map[string]struct{}, len(manifest.Resources))
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

		identity := resourceIdentity(resource)
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

func resourceIdentity(resource Resource) string {
	id := resource.ID()
	return string(id.Kind) + "\x00" + id.ProjectKey + "\x00" + id.LookupKey
}

func validateLookupKey(value string) error {
	for _, segment := range strings.Split(value, "/") {
		if err := validatePathSegment("lookup segment", segment); err != nil {
			return fmt.Errorf("invalid lookup key %q: %w", value, err)
		}
	}
	return nil
}

func validatePathSegment(name, value string) error {
	if value == "" || value == "." || value == ".." || strings.ContainsAny(value, `/\`) || strings.IndexByte(value, 0) >= 0 {
		return fmt.Errorf("invalid %s %q", name, value)
	}
	return nil
}
