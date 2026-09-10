package sync

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

const RootDir = ".launchdarkly"

type Kind string

const (
	KindVariation Kind = "variation"
	KindTool      Kind = "tool"
)

type Fingerprint string

func Hash(payload []byte) Fingerprint {
	sum := sha256.Sum256(payload)

	return Fingerprint("sha256." + hex.EncodeToString(sum[:]))
}

type SyncedResource struct {
	Kind        Kind
	ProjectKey  string
	LookupKey   string
	Payload     json.RawMessage
	Fingerprint Fingerprint
	Upsert      bool
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

type ToolRef struct {
	Key              string         `json:"key" yaml:"key"`
	Version          int            `json:"version" yaml:"version"`
	CustomParameters map[string]any `json:"customParameters,omitempty" yaml:"customParameters,omitempty"`
}

type SkillRef struct {
	Key     string `json:"key" yaml:"key"`
	Version int    `json:"version" yaml:"version"`
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
	Tools              []ToolRef      `json:"tools,omitempty" yaml:"tools,omitempty"`
	Skills             []SkillRef     `json:"skills,omitempty" yaml:"skills,omitempty"`
	Messages           []Message      `json:"messages,omitempty" yaml:"-"`
}
