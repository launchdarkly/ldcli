// Package sync is the domain model that every prompt sync component shares.
// It defines resource identities, variations, attachments, and the
// fingerprints that the manifest stores.
package sync

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// RootDir is the repository-relative directory that holds the sync files.
const RootDir = ".launchdarkly"

// Kind identifies the type of a synchronized resource.
type Kind string

const (
	KindVariation Kind = "variation"
	KindTool      Kind = "tool"
	KindSkill     Kind = "skill"
)

// ResourceID identifies one synchronized resource.
//
// For a variation, LookupKey is "config-key/variation-key". For a tool or a
// skill, LookupKey is the attachment key.
type ResourceID struct {
	Kind       Kind
	ProjectKey string
	LookupKey  string
}

// VariationID returns the identity of one config variation.
func VariationID(projectKey, configKey, variationKey string) ResourceID {
	return ResourceID{
		Kind:       KindVariation,
		ProjectKey: projectKey,
		LookupKey:  configKey + "/" + variationKey,
	}
}

// ParseVariationSelector parses the "project-key/config-key/variation-key"
// selector that the sync commands accept.
func ParseVariationSelector(selector string) (ResourceID, error) {
	parts := strings.Split(selector, "/")
	if len(parts) != 3 || ValidateKey(parts[0]) != nil || ValidateKey(parts[1]) != nil || ValidateKey(parts[2]) != nil {
		return ResourceID{}, fmt.Errorf("invalid variation %q; expected project-key/config-key/variation-key", selector)
	}
	return VariationID(parts[0], parts[1], parts[2]), nil
}

// String returns the "project-key/lookup-key" form used in messages.
func (id ResourceID) String() string {
	return id.ProjectKey + "/" + id.LookupKey
}

// Label returns the kind and the identity, for example "tool demo/search".
func (id ResourceID) Label() string {
	return string(id.Kind) + " " + id.String()
}

// VariationKeys splits a variation lookup key into its config key and its
// variation key.
func (id ResourceID) VariationKeys() (configKey, variationKey string, err error) {
	configKey, variationKey, ok := strings.Cut(id.LookupKey, "/")
	if id.Kind != KindVariation || !ok || configKey == "" || variationKey == "" || strings.Contains(variationKey, "/") {
		return "", "", fmt.Errorf("invalid variation lookup key %q", id.LookupKey)
	}
	return configKey, variationKey, nil
}

// CompareResourceIDs orders identities by kind, project, and lookup key. Plans,
// manifests, and output use this order so that results are deterministic.
//
// Sync applies a plan in this order. The kind names sort skills and tools
// before the variations that pin them, so a variation pins the version that
// the same sync published.
func CompareResourceIDs(left, right ResourceID) int {
	if result := strings.Compare(string(left.Kind), string(right.Kind)); result != 0 {
		return result
	}
	if result := strings.Compare(left.ProjectKey, right.ProjectKey); result != 0 {
		return result
	}
	return strings.Compare(left.LookupKey, right.LookupKey)
}

// ValidateKey makes sure that a project, config, variation, or attachment key
// is one safe path segment. Every key becomes part of a local file path.
func ValidateKey(key string) error {
	switch {
	case key == "":
		return errors.New("must not be empty")
	case key == "." || key == ".." || strings.ContainsAny(key, `/\`):
		return errors.New("must be a single path segment")
	case strings.IndexByte(key, 0) >= 0:
		return errors.New("must not contain a null byte")
	default:
		return nil
	}
}

// These limits copy the create validation of the LaunchDarkly API for
// variations, tools, and skills (AIConfigVariationPost, AIToolPost, and
// AgentSkillPost). LaunchDarkly stays the authority. The copy lets a plan
// report a bad key before the apply starts. Lengths count bytes, as
// LaunchDarkly does.
const (
	createKeyMaxLength         = 256
	skillKeyMaxLength          = 64
	createNameMaxLength        = 256
	createDescriptionMaxLength = 1024
)

var (
	createKeyPattern     = regexp.MustCompile(`^[a-zA-Z0-9][._\-a-zA-Z0-9]*$`)
	skillKeyPattern      = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)
	windowsReservedNames = regexp.MustCompile(`^(con|prn|aux|nul|com[0-9]|lpt[0-9])$`)
)

// ValidateCreate checks the key, the name, and the description of a resource
// that sync creates in LaunchDarkly.
func ValidateCreate(kind Kind, key, name, description string) error {
	switch {
	case kind == KindSkill && !skillKeyPattern.MatchString(key):
		return fmt.Errorf("skill key %q must use only lowercase letters, digits, and hyphens, and start with a letter or digit", key)
	case kind == KindSkill && len(key) > skillKeyMaxLength:
		return fmt.Errorf("skill key %q must be at most %d characters", key, skillKeyMaxLength)
	case kind == KindSkill && windowsReservedNames.MatchString(key):
		return fmt.Errorf("skill key %q is a reserved file name on Windows", key)
	case kind != KindSkill && !createKeyPattern.MatchString(key):
		return fmt.Errorf("%s key %q must use only letters, digits, \".\", \"_\", and \"-\", and start with a letter or digit", kind, key)
	case len(key) > createKeyMaxLength:
		return fmt.Errorf("%s key %q must be at most %d characters", kind, key, createKeyMaxLength)
	case len(name) > createNameMaxLength:
		return fmt.Errorf("%s name must be at most %d characters", kind, createNameMaxLength)
	case len(description) > createDescriptionMaxLength:
		return fmt.Errorf("%s description must be at most %d characters", kind, createDescriptionMaxLength)
	default:
		return nil
	}
}

// Reference links a variation to a prompt file elsewhere in the repository.
// Format names the adapter that reads and writes the file.
type Reference struct {
	File   string `yaml:"file"`
	Format string `yaml:"format"`
}

// SyncedResource is one variation compiled from the local workspace.
type SyncedResource struct {
	Kind       Kind
	ProjectKey string
	LookupKey  string
	// Upsert lets sync create the variation in LaunchDarkly when it is absent.
	Upsert    bool
	Variation Variation
}

// ID returns the identity of the compiled resource.
func (resource SyncedResource) ID() ResourceID {
	return ResourceID{Kind: resource.Kind, ProjectKey: resource.ProjectKey, LookupKey: resource.LookupKey}
}
