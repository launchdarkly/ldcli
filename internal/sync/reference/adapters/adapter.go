// Package adapters defines the contract between sync and the external file
// formats that a linked variation can use.
package adapters

import syncdomain "github.com/launchdarkly/ldcli/internal/sync"

// Adapter converts between one external file format and a Prompt.
type Adapter interface {
	Parse([]byte) (Prompt, error)
	Render(Prompt) ([]byte, error)
}

// Prompt is the content that an external file can supply. An empty field
// means that the file format does not store that value.
type Prompt struct {
	Mode     syncdomain.VariationMode
	Key      string
	Name     string
	Messages []syncdomain.Message
}
