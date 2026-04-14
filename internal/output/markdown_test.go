package output

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMarkdownTableOutput(t *testing.T) {
	t.Run("flags list produces markdown table", func(t *testing.T) {
		cols := GetListColumns("flags")
		items := []resource{
			{
				"key":       "my-flag",
				"name":      "My Feature Flag",
				"kind":      "boolean",
				"temporary": true,
				"tags":      []interface{}{"beta", "frontend"},
			},
			{
				"key":       "dark-mode",
				"name":      "Dark Mode Toggle",
				"kind":      "boolean",
				"temporary": false,
				"tags":      []interface{}{"ui"},
			},
		}

		result := MarkdownTableOutput(items, cols)
		lines := strings.Split(result, "\n")

		assert.Len(t, lines, 4)
		assert.Equal(t, "| KEY | NAME | KIND | TEMPORARY | TAGS |", lines[0])
		assert.Equal(t, "| --- | --- | --- | --- | --- |", lines[1])
		assert.Contains(t, lines[2], "my-flag")
		assert.Contains(t, lines[2], "My Feature Flag")
		assert.Contains(t, lines[2], "yes")
		assert.Contains(t, lines[2], "beta, frontend")
		assert.Contains(t, lines[3], "dark-mode")
		assert.Contains(t, lines[3], "no")
	})

	t.Run("empty items produces header only", func(t *testing.T) {
		cols := GetListColumns("flags")
		result := MarkdownTableOutput([]resource{}, cols)
		lines := strings.Split(result, "\n")

		assert.Len(t, lines, 2)
		assert.Contains(t, lines[0], "KEY")
		assert.Contains(t, lines[1], "---")
	})

	t.Run("escapes pipe characters in values", func(t *testing.T) {
		cols := []ColumnDef{
			{Header: "KEY", Field: "key"},
			{Header: "NAME", Field: "name"},
		}
		items := []resource{
			{"key": "a|b", "name": "test"},
		}

		result := MarkdownTableOutput(items, cols)

		assert.Contains(t, result, `a\|b`)
	})
}

func TestMarkdownKeyValueOutput(t *testing.T) {
	t.Run("produces bullet list", func(t *testing.T) {
		cols := GetSingularColumns("flags")
		r := resource{
			"key":          "my-flag",
			"name":         "My Feature Flag",
			"kind":         "boolean",
			"temporary":    true,
			"creationDate": float64(1718438400000),
			"tags":         []interface{}{"beta"},
		}

		result := MarkdownKeyValueOutput(r, cols)

		assert.Contains(t, result, "- **Key:** my-flag")
		assert.Contains(t, result, "- **Name:** My Feature Flag")
		assert.Contains(t, result, "- **Kind:** boolean")
		assert.Contains(t, result, "- **Temporary:** yes")
		assert.Contains(t, result, "- **Tags:** beta")
	})
}

func TestMarkdownSingularOutput(t *testing.T) {
	t.Run("flags get produces rich output with env table", func(t *testing.T) {
		r := resource{
			"key":         "test-feature-flag",
			"name":        "Test Feature Flag",
			"description": "Example description of what the feature flag does.",
			"kind":        "boolean",
			"temporary":   true,
			"tags":        []interface{}{"test-tag"},
			"environments": map[string]interface{}{
				"production": map[string]interface{}{
					"on":          true,
					"fallthrough": map[string]interface{}{"variation": float64(1)},
					"rules":       []interface{}{},
				},
				"staging": map[string]interface{}{
					"on":          false,
					"fallthrough": map[string]interface{}{"variation": float64(0)},
					"rules":       []interface{}{map[string]interface{}{"id": "rule1"}},
				},
			},
			"variations": []interface{}{
				map[string]interface{}{"value": true, "name": "Available"},
				map[string]interface{}{"value": false, "name": "Unavailable"},
			},
		}

		result := MarkdownSingularOutput(r, "flags")

		assert.Contains(t, result, "## test-feature-flag")
		assert.Contains(t, result, "Example description of what the feature flag does.")
		assert.Contains(t, result, "| Environment | Status | Fallthrough | Rules |")
		assert.Contains(t, result, "| production | ON | Unavailable (false) | 0 |")
		assert.Contains(t, result, "| staging | OFF | Available (true) | 1 |")
		assert.Contains(t, result, "- **Kind:** boolean")
		assert.Contains(t, result, "- **Temporary:** yes")
		assert.Contains(t, result, "- **Tags:** test-tag")
	})

	t.Run("flags without environments omits env table", func(t *testing.T) {
		r := resource{
			"key":       "simple-flag",
			"kind":      "boolean",
			"temporary": false,
		}

		result := MarkdownSingularOutput(r, "flags")

		assert.Contains(t, result, "## simple-flag")
		assert.NotContains(t, result, "| Environment")
		assert.Contains(t, result, "- **Kind:** boolean")
		assert.Contains(t, result, "- **Temporary:** no")
	})

	t.Run("flags without description omits description", func(t *testing.T) {
		r := resource{
			"key":       "no-desc-flag",
			"kind":      "boolean",
			"temporary": false,
		}

		result := MarkdownSingularOutput(r, "flags")

		assert.Contains(t, result, "## no-desc-flag")
		assert.NotContains(t, result, "Example description")
		assert.Contains(t, result, "- **Kind:** boolean")
	})

	t.Run("flags with maintainer shows maintainer", func(t *testing.T) {
		r := resource{
			"key":         "maint-flag",
			"kind":        "boolean",
			"temporary":   false,
			"_maintainer": map[string]interface{}{"name": "John Doe", "email": "john@example.com"},
		}

		result := MarkdownSingularOutput(r, "flags")

		assert.Contains(t, result, "- **Maintainer:** John Doe")
	})

	t.Run("non-flags resource uses column registry", func(t *testing.T) {
		r := resource{
			"key":   "production",
			"name":  "Production",
			"color": "FF0000",
		}

		result := MarkdownSingularOutput(r, "environments")

		assert.Contains(t, result, "## Production (production)")
		assert.Contains(t, result, "- **Key:** production")
		assert.Contains(t, result, "- **Name:** Production")
		assert.Contains(t, result, "- **Color:** FF0000")
	})

	t.Run("unknown resource uses generic heading", func(t *testing.T) {
		r := resource{
			"key":  "test-key",
			"name": "test-name",
		}

		result := MarkdownSingularOutput(r, "unknown-resource")

		assert.Equal(t, "## test-name (test-key)", result)
	})
}

func TestMarkdownMultipleOutput(t *testing.T) {
	t.Run("with registered columns uses markdown table", func(t *testing.T) {
		items := []resource{
			{"key": "flag-1", "name": "Flag 1", "kind": "boolean", "temporary": true, "tags": []interface{}{}},
		}

		result := MarkdownMultipleOutput(items, "flags")

		assert.Contains(t, result, "| KEY | NAME | KIND | TEMPORARY | TAGS |")
		assert.Contains(t, result, "flag-1")
	})

	t.Run("without registered columns uses bullet list", func(t *testing.T) {
		items := []resource{
			{"key": "item-1", "name": "Item 1"},
			{"key": "item-2", "name": "Item 2"},
		}

		result := MarkdownMultipleOutput(items, "unknown-resource")

		assert.Contains(t, result, "- Item 1 (item-1)")
		assert.Contains(t, result, "- Item 2 (item-2)")
	})
}

func TestResolveFallthrough(t *testing.T) {
	variations := []variation{
		{Name: "Available", Value: true},
		{Name: "Unavailable", Value: false},
	}

	t.Run("resolves named variation", func(t *testing.T) {
		envData := map[string]interface{}{
			"fallthrough": map[string]interface{}{"variation": float64(0)},
		}
		result := resolveFallthrough(envData, variations)
		assert.Equal(t, "Available (true)", result)
	})

	t.Run("out of range index", func(t *testing.T) {
		envData := map[string]interface{}{
			"fallthrough": map[string]interface{}{"variation": float64(5)},
		}
		result := resolveFallthrough(envData, variations)
		assert.Equal(t, "variation 5", result)
	})

	t.Run("missing fallthrough", func(t *testing.T) {
		envData := map[string]interface{}{}
		result := resolveFallthrough(envData, variations)
		assert.Equal(t, "", result)
	})

	t.Run("unnamed variation shows value only", func(t *testing.T) {
		vars := []variation{{Name: "", Value: "red"}}
		envData := map[string]interface{}{
			"fallthrough": map[string]interface{}{"variation": float64(0)},
		}
		result := resolveFallthrough(envData, vars)
		assert.Equal(t, "red", result)
	})
}

func TestExtractMaintainer(t *testing.T) {
	t.Run("name preferred over email", func(t *testing.T) {
		r := resource{
			"_maintainer": map[string]interface{}{"name": "Alice", "email": "alice@test.com"},
		}
		assert.Equal(t, "Alice", extractMaintainer(r))
	})

	t.Run("falls back to email", func(t *testing.T) {
		r := resource{
			"_maintainer": map[string]interface{}{"email": "bob@test.com"},
		}
		assert.Equal(t, "bob@test.com", extractMaintainer(r))
	})

	t.Run("no maintainer returns empty", func(t *testing.T) {
		r := resource{}
		assert.Equal(t, "", extractMaintainer(r))
	})
}

func TestEscapeMDPipe(t *testing.T) {
	assert.Equal(t, `a\|b`, escapeMDPipe("a|b"))
	assert.Equal(t, "no pipes", escapeMDPipe("no pipes"))
	assert.Equal(t, "", escapeMDPipe(""))
}
