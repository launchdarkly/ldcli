package sync

import (
	"encoding/json"
	"strings"
)

// RootDir is the repository-relative directory containing sync state.
const RootDir = ".launchdarkly"

// Kind identifies a synchronized resource type.
type Kind string

const (
	KindVariation Kind = "variation"
)

// ResourceID uniquely identifies a synchronized resource.
type ResourceID struct {
	Kind       Kind
	ProjectKey string
	LookupKey  string
}

// CompareResourceIDs orders resource identities for deterministic plans and output.
func CompareResourceIDs(left, right ResourceID) int {
	if result := strings.Compare(string(left.Kind), string(right.Kind)); result != 0 {
		return result
	}
	if result := strings.Compare(left.ProjectKey, right.ProjectKey); result != 0 {
		return result
	}
	return strings.Compare(left.LookupKey, right.LookupKey)
}

// SyncedResource contains one compiled local resource.
type SyncedResource struct {
	Kind       Kind
	ProjectKey string
	LookupKey  string
	Payload    json.RawMessage
	Upsert     bool
}

// VariationMode identifies how a prompt variation stores its content.
type VariationMode string

const (
	VariationModeAgent      VariationMode = "agent"
	VariationModeCompletion VariationMode = "completion"
)

// Valid reports whether the mode is supported for synchronized config variations.
func (mode VariationMode) Valid() bool {
	switch mode {
	case VariationModeAgent, VariationModeCompletion:
		return true
	default:
		return false
	}
}

// Message is one role/content pair in a completion prompt.
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// Variation is the common prompt variation representation used by sync.
type Variation struct {
	Mode               VariationMode  `json:"mode" yaml:"mode"`
	Key                string         `json:"key" yaml:"key"`
	Name               string         `json:"name" yaml:"name"`
	Instructions       string         `json:"instructions,omitempty" yaml:"-"`
	ModelConfigKey     string         `json:"modelConfigKey,omitempty" yaml:"modelConfigKey,omitempty"`
	ModelConfigVersion int            `json:"modelConfigVersion,omitempty" yaml:"modelConfigVersion,omitempty"`
	Model              map[string]any `json:"model,omitempty" yaml:"model,omitempty"`
	OutputFormat       map[string]any `json:"outputFormat,omitempty" yaml:"outputFormat,omitempty"`
	Messages           []Message      `json:"messages,omitempty" yaml:"-"`
}
