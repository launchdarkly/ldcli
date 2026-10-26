package sync

import (
	"errors"
	"fmt"
	"slices"
	"strings"
)

// Scope is the resource kinds that one sync command plans.
type Scope string

const (
	ScopePrompts Scope = "prompts"
	ScopeTools   Scope = "tools"
	ScopeSkills  Scope = "skills"
	ScopeAll     Scope = "all"
)

// Includes reports whether the scope plans resources of kind. The empty scope
// plans prompt variations.
func (scope Scope) Includes(kind Kind) bool {
	switch scope {
	case "", ScopePrompts:
		return kind == KindVariation
	case ScopeTools:
		return kind == KindTool
	case ScopeSkills:
		return kind == KindSkill
	case ScopeAll:
		return true
	default:
		return false
	}
}

// KeyKind returns the kind of resource that --key names in the scope. A sync
// of all kinds takes no --key.
func (scope Scope) KeyKind() Kind {
	switch scope {
	case ScopeTools:
		return KindTool
	case ScopeSkills:
		return KindSkill
	default:
		return KindVariation
	}
}

// Filter is the resources that the --project, --config-key, and --key flags
// name. A sync keeps only those resources. An add or a detach uses them
// instead of asking. The zero Filter names every resource.
type Filter struct {
	Project   string
	ConfigKey string
	// Keys are the keys of resources of one kind, for example variation keys.
	Keys []string
}

// IsZero reports whether no target flag is set.
func (filter Filter) IsZero() bool {
	return filter.Project == "" && filter.ConfigKey == "" && len(filter.Keys) == 0
}

// Validate makes sure that each flag has its parent flag, and that no value is
// blank, unsafe, or repeated. kind is the kind of resource that --key names.
func (filter Filter) Validate(kind Kind) error {
	if err := checkFlagValue("--project", filter.Project); err != nil {
		return err
	}
	if err := checkFlagValue("--config-key", filter.ConfigKey); err != nil {
		return err
	}
	for _, key := range filter.Keys {
		if key == "" {
			return errors.New("--key cannot be blank")
		}
		if err := checkFlagValue("--key", key); err != nil {
			return err
		}
	}

	switch {
	case len(filter.Keys) != 0 && filter.Project == "":
		return errors.New("--key needs --project")
	case filter.ConfigKey != "" && filter.Project == "":
		return errors.New("--config-key needs --project")
	case len(filter.Keys) != 0 && kind == KindVariation && filter.ConfigKey == "":
		return errors.New("--key needs --config-key. In sync prompts, --key is a variation key.")
	}

	ids := filter.IDs(kind)
	for index, id := range ids {
		if slices.Contains(ids[:index], id) {
			return fmt.Errorf("%s was selected more than once", id.Label())
		}
	}
	return nil
}

// IDs returns the resources of kind that --key names.
func (filter Filter) IDs(kind Kind) []ResourceID {
	ids := make([]ResourceID, 0, len(filter.Keys))
	for _, key := range filter.Keys {
		if kind == KindVariation {
			ids = append(ids, VariationID(filter.Project, filter.ConfigKey, key))
		} else {
			ids = append(ids, ResourceID{Kind: kind, ProjectKey: filter.Project, LookupKey: key})
		}
	}
	return ids
}

// Includes reports whether the filter keeps the resource. --config-key keeps
// only variations of that config.
func (filter Filter) Includes(id ResourceID) bool {
	if filter.Project != "" && id.ProjectKey != filter.Project {
		return false
	}
	key := id.LookupKey
	if id.Kind == KindVariation {
		configKey, variationKey, err := id.VariationKeys()
		if err != nil || filter.ConfigKey != "" && configKey != filter.ConfigKey {
			return false
		}
		key = variationKey
	} else if filter.ConfigKey != "" {
		return false
	}
	return len(filter.Keys) == 0 || slices.Contains(filter.Keys, key)
}

// checkFlagValue rejects a blank or unsafe flag value. An empty value is a
// flag that is not set.
func checkFlagValue(flag, value string) error {
	switch {
	case value == "":
		return nil
	case strings.TrimSpace(value) == "":
		return fmt.Errorf("%s cannot be blank", flag)
	}
	if err := ValidateKey(value); err != nil {
		return fmt.Errorf("invalid %s %q: %w", flag, value, err)
	}
	return nil
}
