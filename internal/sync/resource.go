package sync

import "encoding/json"

const RootDir = ".launchdarkly"

type Kind string

const (
	KindVariation Kind = "variation"
)

type SyncedResource struct {
	Kind       Kind
	ProjectKey string
	LookupKey  string
	Payload    json.RawMessage
	Upsert     bool
}

type VariationMode string

const (
	VariationModeAgent      VariationMode = "agent"
	VariationModeCompletion VariationMode = "completion"
)

func (m VariationMode) Valid() bool {
	switch m {
	case VariationModeAgent, VariationModeCompletion:
		return true
	default:
		return false
	}
}

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type Variation struct {
	Mode               VariationMode  `json:"mode" yaml:"mode"`
	Key                string         `json:"key" yaml:"key"`
	Name               string         `json:"name" yaml:"name"`
	Description        string         `json:"description,omitempty" yaml:"description,omitempty"`
	Instructions       string         `json:"instructions,omitempty" yaml:"-"`
	ModelConfigKey     string         `json:"modelConfigKey,omitempty" yaml:"modelConfigKey,omitempty"`
	ModelConfigVersion int            `json:"modelConfigVersion,omitempty" yaml:"modelConfigVersion,omitempty"`
	Model              map[string]any `json:"model,omitempty" yaml:"model,omitempty"`
	OutputFormat       map[string]any `json:"outputFormat,omitempty" yaml:"outputFormat,omitempty"`
	Messages           []Message      `json:"messages,omitempty" yaml:"-"`
}
