package output

import (
	"bytes"
	"fmt"
	"strings"
	"text/tabwriter"
	"time"
)

// ColumnDef describes a single column in table or key-value output.
type ColumnDef struct {
	Header string
	Field  string
	Format func(interface{}) string
}

func defaultFormat(v interface{}) string {
	if v == nil {
		return ""
	}
	return fmt.Sprint(v)
}

func boolYesNo(v interface{}) string {
	switch b := v.(type) {
	case bool:
		if b {
			return "yes"
		}
		return "no"
	default:
		return defaultFormat(v)
	}
}

func truncatedList(max int) func(interface{}) string {
	return func(v interface{}) string {
		items, ok := v.([]interface{})
		if !ok {
			return defaultFormat(v)
		}
		strs := make([]string, 0, len(items))
		for _, item := range items {
			strs = append(strs, fmt.Sprint(item))
		}
		if len(strs) <= max {
			return strings.Join(strs, ", ")
		}
		return strings.Join(strs[:max], ", ") + ", ..."
	}
}

func countList(v interface{}) string {
	items, ok := v.([]interface{})
	if !ok {
		return defaultFormat(v)
	}
	return fmt.Sprintf("%d", len(items))
}

func formatTimestamp(v interface{}) string {
	switch n := v.(type) {
	case float64:
		return time.UnixMilli(int64(n)).UTC().Format(time.RFC3339)
	default:
		return defaultFormat(v)
	}
}

// listColumnRegistry maps resource names to their list-view column definitions.
var listColumnRegistry = map[string][]ColumnDef{
	"flags": {
		{Header: "KEY", Field: "key"},
		{Header: "NAME", Field: "name"},
		{Header: "KIND", Field: "kind"},
		{Header: "TEMPORARY", Field: "temporary", Format: boolYesNo},
		{Header: "TAGS", Field: "tags", Format: truncatedList(3)},
	},
	"projects": {
		{Header: "KEY", Field: "key"},
		{Header: "NAME", Field: "name"},
		{Header: "TAG COUNT", Field: "tags", Format: countList},
	},
	"environments": {
		{Header: "KEY", Field: "key"},
		{Header: "NAME", Field: "name"},
		{Header: "COLOR", Field: "color"},
	},
	"members": {
		{Header: "EMAIL", Field: "email"},
		{Header: "ROLE", Field: "role"},
		{Header: "LAST NAME", Field: "lastName"},
		{Header: "FIRST NAME", Field: "firstName"},
	},
	"segments": {
		{Header: "KEY", Field: "key"},
		{Header: "NAME", Field: "name"},
		{Header: "CREATED", Field: "creationDate", Format: formatTimestamp},
	},
}

// singularColumnRegistry maps resource names to their singular-view column definitions.
var singularColumnRegistry = map[string][]ColumnDef{
	"flags": {
		{Header: "Key", Field: "key"},
		{Header: "Name", Field: "name"},
		{Header: "Kind", Field: "kind"},
		{Header: "Temporary", Field: "temporary", Format: boolYesNo},
		{Header: "Created", Field: "creationDate", Format: formatTimestamp},
		{Header: "Tags", Field: "tags", Format: truncatedList(10)},
	},
	"projects": {
		{Header: "Key", Field: "key"},
		{Header: "Name", Field: "name"},
		{Header: "Tag Count", Field: "tags", Format: countList},
	},
	"environments": {
		{Header: "Key", Field: "key"},
		{Header: "Name", Field: "name"},
		{Header: "Color", Field: "color"},
	},
	"members": {
		{Header: "Email", Field: "email"},
		{Header: "Role", Field: "role"},
		{Header: "Last Name", Field: "lastName"},
		{Header: "First Name", Field: "firstName"},
	},
	"segments": {
		{Header: "Key", Field: "key"},
		{Header: "Name", Field: "name"},
		{Header: "Created", Field: "creationDate", Format: formatTimestamp},
	},
}

// GetListColumns returns the column definitions for a resource's list view, or nil if none are registered.
func GetListColumns(resourceName string) []ColumnDef {
	return listColumnRegistry[resourceName]
}

// GetSingularColumns returns the column definitions for a resource's singular view, or nil if none are registered.
func GetSingularColumns(resourceName string) []ColumnDef {
	return singularColumnRegistry[resourceName]
}

func colValue(r resource, col ColumnDef) string {
	if r == nil {
		return ""
	}
	format := col.Format
	if format == nil {
		format = defaultFormat
	}
	return strings.ReplaceAll(format(r[col.Field]), "\t", " ")
}

// TableOutput formats a slice of resources as an aligned table using tabwriter.
func TableOutput(items []resource, cols []ColumnDef) string {
	var buf bytes.Buffer
	w := tabwriter.NewWriter(&buf, 0, 0, 3, ' ', 0)

	headers := make([]string, len(cols))
	for i, col := range cols {
		headers[i] = col.Header
	}
	fmt.Fprintln(w, strings.Join(headers, "\t"))

	for _, item := range items {
		vals := make([]string, len(cols))
		for i, col := range cols {
			vals[i] = colValue(item, col)
		}
		fmt.Fprintln(w, strings.Join(vals, "\t"))
	}

	w.Flush()

	return strings.TrimRight(buf.String(), "\n")
}

// KeyValueOutput formats a single resource as key-value pairs.
func KeyValueOutput(r resource, cols []ColumnDef) string {
	maxLen := 0
	for _, col := range cols {
		if len(col.Header) > maxLen {
			maxLen = len(col.Header)
		}
	}

	lines := make([]string, 0, len(cols))
	for _, col := range cols {
		val := colValue(r, col)
		padding := strings.Repeat(" ", maxLen-len(col.Header))
		lines = append(lines, fmt.Sprintf("%s:%s  %s", col.Header, padding, val))
	}

	return strings.Join(lines, "\n")
}
