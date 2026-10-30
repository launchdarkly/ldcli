package reference

import (
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
)

func TestLangChainPlaceholderTranslationRoundTrip(t *testing.T) {
	source := []byte(`from langchain_core.prompts import PromptTemplate
prompt = PromptTemplate.from_template("Hello {customer}.")
`)
	id := syncdomain.VariationID("project", "config", "default")
	reference := syncdomain.Reference{
		File: "prompt.py", Format: "python", Select: `prompts[id="prompt"]`,
		Map: map[string]string{"prompt": "template"},
	}
	projection, err := ParseSource(
		sourceBundle(reference.File, source), id, syncdomain.VariationModeAgent, reference,
	)
	require.NoError(t, err)
	assert.Equal(t, "Hello {{customer}}.", *projection.(syncdomain.VariationProjection).Instructions)

	next := "Welcome {{customer_name}}."
	result, err := UpdateSource(
		sourceBundle(reference.File, source), id, syncdomain.VariationModeAgent, reference,
		syncdomain.VariationProjection{Mode: syncdomain.VariationModeAgent, Instructions: &next},
	)
	require.NoError(t, err)
	updated := applyConformanceEdits(t, source, directEdits(t, result))
	assert.Contains(t, string(updated), `"Welcome {customer_name}."`)
}

func TestDollarPlaceholderTranslationRoundTrip(t *testing.T) {
	source := []byte(`import { generateText } from "ai";
const result = generateText({ prompt: "Hello ${customer}." });
`)
	id := syncdomain.VariationID("project", "config", "default")
	reference := syncdomain.Reference{
		File: "prompt.ts", Format: "typescript", Select: `prompts[id="result"]`,
		Map: map[string]string{"prompt": "prompt"},
	}
	projection, err := ParseSource(
		sourceBundle(reference.File, source), id, syncdomain.VariationModeAgent, reference,
	)
	require.NoError(t, err)
	assert.Equal(t, "Hello {{customer}}.", *projection.(syncdomain.VariationProjection).Instructions)

	next := "Welcome {{customer_name}}."
	result, err := UpdateSource(
		sourceBundle(reference.File, source), id, syncdomain.VariationModeAgent, reference,
		syncdomain.VariationProjection{Mode: syncdomain.VariationModeAgent, Instructions: &next},
	)
	require.NoError(t, err)
	updated := applyConformanceEdits(t, source, directEdits(t, result))
	assert.Contains(t, string(updated), `"Welcome ${customer_name}."`)
}

func TestAmbiguousPlaceholdersAreReadOnly(t *testing.T) {
	tests := []struct {
		name, template string
	}{
		{name: "escaped braces", template: `Use {{literal}} braces.`},
		{name: "JSON example", template: `Return {"status":"ok"}.`},
		{name: "format conversion", template: `Show {value!r}.`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			source := []byte("from langchain_core.prompts import PromptTemplate\nprompt = PromptTemplate.from_template(" +
				strconv.Quote(test.template) + ")\n")
			id := syncdomain.VariationID("project", "config", "default")
			reference := syncdomain.Reference{
				File: "prompt.py", Format: "python", Select: `prompts[id="prompt"]`,
				Map: map[string]string{"prompt": "template"},
			}
			projection, err := ParseSource(
				sourceBundle(reference.File, source), id, syncdomain.VariationModeAgent, reference,
			)
			require.NoError(t, err)
			state, ok := syncdomain.ProjectionFieldState(projection, syncdomain.FieldPrompt)
			require.True(t, ok)
			assert.Equal(t, syncdomain.CapabilityReadOnly, state.Capability)
			assert.Equal(t, syncdomain.ErrorPlaceholderAmbiguous, state.Code)
		})
	}
}
