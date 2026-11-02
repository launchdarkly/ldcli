package reference_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
	"github.com/launchdarkly/ldcli/internal/sync/reference"
)

type externalFakeAdapter struct{}

func (externalFakeAdapter) Format() string { return "external-fake" }
func (externalFakeAdapter) Inspect(bundle reference.SourceBundle) (reference.Inspection, error) {
	document := bundle.Primary
	return reference.NewInspection(&reference.Node{
		Value: string(document.Data), Start: 0, End: len(document.Data),
		Capability: syncdomain.CapabilityDirect,
		Encode:     func(value any) ([]byte, error) { return []byte(value.(string)), nil },
	}), nil
}
func (externalFakeAdapter) Defaults(syncdomain.Kind) (string, map[string]string) {
	return "$", map[string]string{"prompt": "$"}
}

func TestExternalAdapterNeedsOnlyRegistration(t *testing.T) {
	require.NoError(t, reference.Register(externalFakeAdapter{}))
	id := syncdomain.VariationID("project", "config", "default")

	projection, err := reference.ParseSource(
		reference.SourceBundle{
			Primary: reference.Document{Path: "prompt.fake", Data: []byte("External adapter")},
		},
		id,
		syncdomain.VariationModeAgent,
		syncdomain.Reference{File: "prompt.fake", Format: "external-fake"},
	)

	require.NoError(t, err)
	require.Equal(t, "External adapter", *projection.(syncdomain.VariationProjection).Instructions)
}
