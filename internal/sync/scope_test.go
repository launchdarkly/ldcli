package sync

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFilterValidateNeedsParentsAndRejectsBadValues(t *testing.T) {
	tests := map[string]struct {
		filter Filter
		want   string
	}{
		"key without project":    {Filter{Keys: []string{"default"}}, "--key needs --project"},
		"config without project": {Filter{ConfigKey: "support"}, "--config-key needs --project"},
		"key without config": {
			Filter{Project: "demo", Keys: []string{"default"}},
			"--key needs --config-key. In sync prompts, --key is a variation key.",
		},
		"blank project": {Filter{Project: "  "}, "--project cannot be blank"},
		"blank key":     {Filter{Project: "demo", ConfigKey: "support", Keys: []string{""}}, "--key cannot be blank"},
		"unsafe key": {
			Filter{Project: "demo", ConfigKey: "support", Keys: []string{"a/b"}},
			`invalid --key "a/b": must be a single path segment`,
		},
		"repeated key": {
			Filter{Project: "demo", ConfigKey: "support", Keys: []string{"default", "default"}},
			"variation demo/support/default was selected more than once",
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			require.EqualError(t, test.filter.Validate(KindVariation), test.want)
		})
	}
	require.NoError(t, Filter{}.Validate(KindVariation))
	require.NoError(t, Filter{Project: "demo", ConfigKey: "support", Keys: []string{"a", "b"}}.Validate(KindVariation))
}

func TestFilterIncludesOnlyTheNamedResources(t *testing.T) {
	first := VariationID("demo", "support", "first")
	second := VariationID("demo", "support", "second")
	otherConfig := VariationID("demo", "chat", "first")
	otherProject := VariationID("other", "support", "first")
	tool := ResourceID{Kind: KindTool, ProjectKey: "demo", LookupKey: "first"}

	tests := map[string]struct {
		filter Filter
		want   []ResourceID
	}{
		"empty":   {Filter{}, []ResourceID{first, second, otherConfig, otherProject, tool}},
		"project": {Filter{Project: "demo"}, []ResourceID{first, second, otherConfig, tool}},
		"config":  {Filter{Project: "demo", ConfigKey: "support"}, []ResourceID{first, second}},
		"key":     {Filter{Project: "demo", ConfigKey: "support", Keys: []string{"first"}}, []ResourceID{first}},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			var included []ResourceID
			for _, id := range []ResourceID{first, second, otherConfig, otherProject, tool} {
				if test.filter.Includes(id) {
					included = append(included, id)
				}
			}
			assert.Equal(t, test.want, included)
		})
	}
}

func TestFilterIDsNamesEachKey(t *testing.T) {
	filter := Filter{Project: "demo", ConfigKey: "support", Keys: []string{"first", "second"}}

	assert.Equal(t, []ResourceID{
		VariationID("demo", "support", "first"), VariationID("demo", "support", "second"),
	}, filter.IDs(KindVariation))
	assert.True(t, Filter{}.IsZero())
	assert.False(t, filter.IsZero())
}
