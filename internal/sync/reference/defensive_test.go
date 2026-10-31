package reference

import (
	"testing"

	"github.com/stretchr/testify/require"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
)

func TestStructuredAdaptersRejectMalformedSources(t *testing.T) {
	tests := []struct {
		name, format, source string
	}{
		{name: "JSON duplicate", format: "json", source: `{"key":1,"key":2}`},
		{name: "JSON trailing", format: "json", source: `{"key":1} trailing`},
		{name: "YAML duplicate", format: "yaml", source: "key: one\nkey: two\n"},
		{name: "TOML duplicate", format: "toml", source: "key = 1\nkey = 2\n"},
	}
	id := syncdomain.VariationID("project", "config", "default")
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := ParseSource(
				sourceBundle("source", []byte(test.source)),
				id,
				syncdomain.VariationModeAgent,
				syncdomain.Reference{
					File: "source", Format: test.format, Select: "$",
					Map: map[string]string{"prompt": "key"},
				},
			)
			require.Error(t, err)
		})
	}
}
