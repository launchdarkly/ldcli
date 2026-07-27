package output

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestTableOutput(t *testing.T) {
	t.Run("flags list shows key, name, kind, temporary, tags", func(t *testing.T) {
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

		result := TableOutput(items, cols)
		lines := strings.Split(result, "\n")

		assert.Len(t, lines, 3)
		assert.Contains(t, lines[0], "KEY")
		assert.Contains(t, lines[0], "NAME")
		assert.Contains(t, lines[0], "KIND")
		assert.Contains(t, lines[0], "TEMPORARY")
		assert.Contains(t, lines[0], "TAGS")
		assert.Contains(t, lines[1], "my-flag")
		assert.Contains(t, lines[1], "My Feature Flag")
		assert.Contains(t, lines[1], "boolean")
		assert.Contains(t, lines[1], "yes")
		assert.Contains(t, lines[1], "beta, frontend")
		assert.Contains(t, lines[2], "dark-mode")
		assert.Contains(t, lines[2], "no")
	})

	t.Run("flags list truncates tags beyond 3", func(t *testing.T) {
		cols := GetListColumns("flags")
		items := []resource{
			{
				"key":       "many-tags",
				"name":      "Many Tags",
				"kind":      "boolean",
				"temporary": false,
				"tags":      []interface{}{"a", "b", "c", "d"},
			},
		}

		result := TableOutput(items, cols)

		assert.Contains(t, result, "a, b, c, ...")
	})

	t.Run("projects list shows key, name, tag count", func(t *testing.T) {
		cols := GetListColumns("projects")
		items := []resource{
			{
				"key":  "proj-1",
				"name": "Project One",
				"tags": []interface{}{"tag1", "tag2", "tag3"},
			},
		}

		result := TableOutput(items, cols)
		lines := strings.Split(result, "\n")

		assert.Len(t, lines, 2)
		assert.Contains(t, lines[0], "KEY")
		assert.Contains(t, lines[0], "NAME")
		assert.Contains(t, lines[0], "TAG COUNT")
		assert.Contains(t, lines[1], "proj-1")
		assert.Contains(t, lines[1], "Project One")
		assert.Contains(t, lines[1], "3")
	})

	t.Run("environments list shows key, name, color", func(t *testing.T) {
		cols := GetListColumns("environments")
		items := []resource{
			{
				"key":   "production",
				"name":  "Production",
				"color": "FF0000",
			},
		}

		result := TableOutput(items, cols)
		lines := strings.Split(result, "\n")

		assert.Len(t, lines, 2)
		assert.Contains(t, lines[0], "KEY")
		assert.Contains(t, lines[0], "NAME")
		assert.Contains(t, lines[0], "COLOR")
		assert.Contains(t, lines[1], "production")
		assert.Contains(t, lines[1], "Production")
		assert.Contains(t, lines[1], "FF0000")
	})

	t.Run("members list shows email, role, lastName, firstName", func(t *testing.T) {
		cols := GetListColumns("members")
		items := []resource{
			{
				"email":     "alice@example.com",
				"role":      "admin",
				"lastName":  "Smith",
				"firstName": "Alice",
			},
		}

		result := TableOutput(items, cols)
		lines := strings.Split(result, "\n")

		assert.Len(t, lines, 2)
		assert.Contains(t, lines[0], "EMAIL")
		assert.Contains(t, lines[0], "ROLE")
		assert.Contains(t, lines[0], "LAST NAME")
		assert.Contains(t, lines[0], "FIRST NAME")
		assert.Contains(t, lines[1], "alice@example.com")
		assert.Contains(t, lines[1], "admin")
		assert.Contains(t, lines[1], "Smith")
		assert.Contains(t, lines[1], "Alice")
	})

	t.Run("segments list shows key, name, creationDate", func(t *testing.T) {
		cols := GetListColumns("segments")
		items := []resource{
			{
				"key":          "beta-users",
				"name":         "Beta Users",
				"creationDate": float64(1718438400000),
			},
		}

		result := TableOutput(items, cols)
		lines := strings.Split(result, "\n")

		assert.Len(t, lines, 2)
		expected := time.UnixMilli(1718438400000).UTC().Format(time.RFC3339)
		assert.Contains(t, lines[0], "KEY")
		assert.Contains(t, lines[0], "NAME")
		assert.Contains(t, lines[0], "CREATED")
		assert.Contains(t, lines[1], "beta-users")
		assert.Contains(t, lines[1], "Beta Users")
		assert.Contains(t, lines[1], expected)
	})

	t.Run("handles nil field values gracefully", func(t *testing.T) {
		cols := GetListColumns("flags")
		items := []resource{
			{
				"key":  "no-extras",
				"name": "No Extras",
			},
		}

		result := TableOutput(items, cols)

		assert.Contains(t, result, "no-extras")
		assert.Contains(t, result, "No Extras")
	})

	t.Run("multiple rows are aligned", func(t *testing.T) {
		cols := GetListColumns("environments")
		items := []resource{
			{"key": "short", "name": "S", "color": "000"},
			{"key": "a-very-long-key", "name": "A Very Long Name", "color": "FFF"},
		}

		result := TableOutput(items, cols)
		lines := strings.Split(result, "\n")

		assert.Len(t, lines, 3)
		headerKeyEnd := strings.Index(lines[0], "NAME")
		row1KeyEnd := strings.Index(lines[1], "S")
		row2KeyEnd := strings.Index(lines[2], "A Very Long Name")
		assert.Equal(t, headerKeyEnd, row1KeyEnd)
		assert.Equal(t, headerKeyEnd, row2KeyEnd)
	})
}

func TestKeyValueOutput(t *testing.T) {
	t.Run("flags singular shows key-value pairs", func(t *testing.T) {
		cols := GetSingularColumns("flags")
		r := resource{
			"key":          "my-flag",
			"name":         "My Feature Flag",
			"kind":         "boolean",
			"temporary":    true,
			"creationDate": float64(1718438400000),
			"tags":         []interface{}{"beta", "frontend"},
		}

		result := KeyValueOutput(r, cols)

		expected := time.UnixMilli(1718438400000).UTC().Format(time.RFC3339)
		assert.Contains(t, result, "Key:")
		assert.Contains(t, result, "my-flag")
		assert.Contains(t, result, "Name:")
		assert.Contains(t, result, "My Feature Flag")
		assert.Contains(t, result, "Kind:")
		assert.Contains(t, result, "boolean")
		assert.Contains(t, result, "Temporary:")
		assert.Contains(t, result, "yes")
		assert.Contains(t, result, "Created:")
		assert.Contains(t, result, expected)
		assert.Contains(t, result, "Tags:")
		assert.Contains(t, result, "beta, frontend")
	})

	t.Run("members singular shows email, role, names", func(t *testing.T) {
		cols := GetSingularColumns("members")
		r := resource{
			"email":     "alice@example.com",
			"role":      "admin",
			"lastName":  "Smith",
			"firstName": "Alice",
		}

		result := KeyValueOutput(r, cols)

		assert.Contains(t, result, "Email:")
		assert.Contains(t, result, "alice@example.com")
		assert.Contains(t, result, "Role:")
		assert.Contains(t, result, "admin")
	})

	t.Run("handles nil values", func(t *testing.T) {
		cols := GetSingularColumns("flags")
		r := resource{
			"key":  "minimal",
			"name": "Minimal Flag",
		}

		result := KeyValueOutput(r, cols)

		assert.Contains(t, result, "Key:        minimal")
		assert.Contains(t, result, "Name:       Minimal Flag")
		assert.Contains(t, result, "Kind:")
	})
}

func TestGetListColumns(t *testing.T) {
	t.Run("returns nil for unknown resource", func(t *testing.T) {
		cols := GetListColumns("unknown-resource")

		assert.Nil(t, cols)
	})

	t.Run("returns columns for flags", func(t *testing.T) {
		cols := GetListColumns("flags")

		assert.NotNil(t, cols)
		assert.Equal(t, "KEY", cols[0].Header)
	})
}

func TestGetSingularColumns(t *testing.T) {
	t.Run("returns nil for unknown resource", func(t *testing.T) {
		cols := GetSingularColumns("unknown-resource")

		assert.Nil(t, cols)
	})

	t.Run("returns columns for flags", func(t *testing.T) {
		cols := GetSingularColumns("flags")

		assert.NotNil(t, cols)
		assert.Equal(t, "Key", cols[0].Header)
	})
}

func TestBoolYesNo(t *testing.T) {
	assert.Equal(t, "yes", boolYesNo(true))
	assert.Equal(t, "no", boolYesNo(false))
	assert.Equal(t, "something", boolYesNo("something"))
	assert.Equal(t, "", boolYesNo(nil))
}

func TestTruncatedList(t *testing.T) {
	fn := truncatedList(3)

	t.Run("within limit", func(t *testing.T) {
		result := fn([]interface{}{"a", "b"})
		assert.Equal(t, "a, b", result)
	})

	t.Run("at limit", func(t *testing.T) {
		result := fn([]interface{}{"a", "b", "c"})
		assert.Equal(t, "a, b, c", result)
	})

	t.Run("over limit", func(t *testing.T) {
		result := fn([]interface{}{"a", "b", "c", "d"})
		assert.Equal(t, "a, b, c, ...", result)
	})

	t.Run("empty list", func(t *testing.T) {
		result := fn([]interface{}{})
		assert.Equal(t, "", result)
	})

	t.Run("non-slice value", func(t *testing.T) {
		result := fn("not-a-list")
		assert.Equal(t, "not-a-list", result)
	})

	t.Run("nil value", func(t *testing.T) {
		result := fn(nil)
		assert.Equal(t, "", result)
	})
}

func TestCountList(t *testing.T) {
	t.Run("counts items", func(t *testing.T) {
		result := countList([]interface{}{"a", "b", "c"})
		assert.Equal(t, "3", result)
	})

	t.Run("empty list", func(t *testing.T) {
		result := countList([]interface{}{})
		assert.Equal(t, "0", result)
	})

	t.Run("non-slice", func(t *testing.T) {
		result := countList("not-a-list")
		assert.Equal(t, "not-a-list", result)
	})

	t.Run("nil value", func(t *testing.T) {
		result := countList(nil)
		assert.Equal(t, "", result)
	})
}

func TestFormatTimestamp(t *testing.T) {
	t.Run("formats float64 unix millis to RFC3339", func(t *testing.T) {
		result := formatTimestamp(float64(1718438400000))
		expected := time.UnixMilli(1718438400000).UTC().Format(time.RFC3339)
		assert.Equal(t, expected, result)
	})

	t.Run("nil returns empty string", func(t *testing.T) {
		result := formatTimestamp(nil)
		assert.Equal(t, "", result)
	})

	t.Run("string passes through to defaultFormat", func(t *testing.T) {
		result := formatTimestamp("2024-06-15T00:00:00Z")
		assert.Equal(t, "2024-06-15T00:00:00Z", result)
	})
}

func TestTableOutputEdgeCases(t *testing.T) {
	t.Run("empty items produces header only", func(t *testing.T) {
		cols := GetListColumns("flags")
		result := TableOutput([]resource{}, cols)
		lines := strings.Split(result, "\n")
		assert.Len(t, lines, 1)
		assert.Contains(t, lines[0], "KEY")
	})

	t.Run("nil resource in items does not panic", func(t *testing.T) {
		cols := GetListColumns("environments")
		items := []resource{
			{"key": "good", "name": "Good", "color": "FFF"},
			nil,
		}
		result := TableOutput(items, cols)
		assert.Contains(t, result, "good")
	})

	t.Run("tab in value does not break alignment", func(t *testing.T) {
		cols := []ColumnDef{
			{Header: "KEY", Field: "key"},
			{Header: "NAME", Field: "name"},
		}
		items := []resource{
			{"key": "has\ttab", "name": "normal"},
		}
		result := TableOutput(items, cols)
		assert.NotContains(t, result, "\t")
		assert.Contains(t, result, "has tab")
	})
}

func TestKeyValueOutputEdgeCases(t *testing.T) {
	t.Run("empty cols returns empty string", func(t *testing.T) {
		r := resource{"key": "val"}
		result := KeyValueOutput(r, []ColumnDef{})
		assert.Equal(t, "", result)
	})
}

func TestBoolYesNoEdgeCases(t *testing.T) {
	t.Run("numeric float64 passes through", func(t *testing.T) {
		result := boolYesNo(float64(1))
		assert.Equal(t, "1", result)
	})
}
