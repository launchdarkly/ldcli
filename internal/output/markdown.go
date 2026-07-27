package output

import (
	"fmt"
	"sort"
	"strings"
)

// MarkdownTableOutput formats a slice of resources as a GitHub-flavored markdown table.
func MarkdownTableOutput(items []resource, cols []ColumnDef) string {
	headers := make([]string, len(cols))
	separators := make([]string, len(cols))
	for i, col := range cols {
		headers[i] = col.Header
		separators[i] = "---"
	}

	var sb strings.Builder
	sb.WriteString("| ")
	sb.WriteString(strings.Join(headers, " | "))
	sb.WriteString(" |\n| ")
	sb.WriteString(strings.Join(separators, " | "))
	sb.WriteString(" |")

	for _, item := range items {
		vals := make([]string, len(cols))
		for i, col := range cols {
			vals[i] = escapeMDPipe(colValue(item, col))
		}
		sb.WriteString("\n| ")
		sb.WriteString(strings.Join(vals, " | "))
		sb.WriteString(" |")
	}

	return sb.String()
}

// MarkdownKeyValueOutput formats a single resource as a markdown bullet list of key-value pairs.
func MarkdownKeyValueOutput(r resource, cols []ColumnDef) string {
	lines := make([]string, 0, len(cols))
	for _, col := range cols {
		val := colValue(r, col)
		lines = append(lines, fmt.Sprintf("- **%s:** %s", col.Header, val))
	}
	return strings.Join(lines, "\n")
}

// MarkdownSingularOutput renders a single resource in markdown with a heading and metadata.
// For flags it produces a rich view with environment table; for other resources it uses
// the column registry or a generic fallback.
func MarkdownSingularOutput(r resource, resourceName string) string {
	if resourceName == "flags" {
		return markdownFlagOutput(r)
	}

	heading := markdownHeading(r)
	if cols := GetSingularColumns(resourceName); cols != nil {
		return heading + "\n\n" + MarkdownKeyValueOutput(r, cols)
	}
	return heading
}

// MarkdownMultipleOutput renders a list of resources as a markdown table (if columns are
// registered) or a bullet list.
func MarkdownMultipleOutput(items []resource, resourceName string) string {
	if cols := GetListColumns(resourceName); cols != nil {
		return MarkdownTableOutput(items, cols)
	}

	lines := make([]string, 0, len(items))
	for _, item := range items {
		lines = append(lines, fmt.Sprintf("- %s", SingularPlaintextOutputFn(item)))
	}
	return strings.Join(lines, "\n")
}

func markdownFlagOutput(r resource) string {
	var sb strings.Builder

	key := defaultFormat(r["key"])
	sb.WriteString("## ")
	sb.WriteString(key)

	if desc, ok := r["description"]; ok && desc != nil && fmt.Sprint(desc) != "" {
		sb.WriteString("\n\n")
		sb.WriteString(fmt.Sprint(desc))
	}

	envTable := markdownEnvTable(r)
	if envTable != "" {
		sb.WriteString("\n\n")
		sb.WriteString(envTable)
	}

	if meta := markdownFlagMetadata(r); meta != "" {
		sb.WriteString("\n\n")
		sb.WriteString(meta)
	}

	return sb.String()
}

func markdownEnvTable(r resource) string {
	envMap, ok := r["environments"].(map[string]interface{})
	if !ok || len(envMap) == 0 {
		return ""
	}

	variations := extractVariations(r)

	keys := make([]string, 0, len(envMap))
	for k := range envMap {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var sb strings.Builder
	sb.WriteString("| Environment | Status | Fallthrough | Rules |\n")
	sb.WriteString("| --- | --- | --- | --- |")

	for _, envKey := range keys {
		envData, ok := envMap[envKey].(map[string]interface{})
		if !ok {
			continue
		}

		status := "OFF"
		if on, ok := envData["on"].(bool); ok && on {
			status = "ON"
		}

		fallthrough_ := resolveFallthrough(envData, variations)

		rulesCount := 0
		if rules, ok := envData["rules"].([]interface{}); ok {
			rulesCount = len(rules)
		}

		sb.WriteString(fmt.Sprintf("\n| %s | %s | %s | %d |",
			escapeMDPipe(envKey), status, escapeMDPipe(fallthrough_), rulesCount))
	}

	return sb.String()
}

func markdownFlagMetadata(r resource) string {
	var lines []string

	if kind := r["kind"]; kind != nil {
		lines = append(lines, fmt.Sprintf("- **Kind:** %s", kind))
	}
	if temp, ok := r["temporary"].(bool); ok {
		lines = append(lines, fmt.Sprintf("- **Temporary:** %s", boolYesNo(temp)))
	}
	if tags, ok := r["tags"].([]interface{}); ok && len(tags) > 0 {
		strs := make([]string, len(tags))
		for i, t := range tags {
			strs[i] = fmt.Sprint(t)
		}
		lines = append(lines, fmt.Sprintf("- **Tags:** %s", strings.Join(strs, ", ")))
	}
	if maintainer := extractMaintainer(r); maintainer != "" {
		lines = append(lines, fmt.Sprintf("- **Maintainer:** %s", maintainer))
	}

	return strings.Join(lines, "\n")
}

func extractVariations(r resource) []variation {
	raw, ok := r["variations"].([]interface{})
	if !ok {
		return nil
	}
	vars := make([]variation, 0, len(raw))
	for _, v := range raw {
		m, ok := v.(map[string]interface{})
		if !ok {
			continue
		}
		name := ""
		if n, ok := m["name"].(string); ok {
			name = n
		}
		vars = append(vars, variation{
			Name:  name,
			Value: m["value"],
		})
	}
	return vars
}

type variation struct {
	Name  string
	Value interface{}
}

func resolveFallthrough(envData map[string]interface{}, variations []variation) string {
	ft, ok := envData["fallthrough"].(map[string]interface{})
	if !ok {
		return ""
	}
	varIdx, ok := ft["variation"].(float64)
	if !ok {
		return ""
	}
	idx := int(varIdx)
	if idx < 0 || idx >= len(variations) {
		return fmt.Sprintf("variation %d", idx)
	}
	v := variations[idx]
	if v.Name != "" {
		return fmt.Sprintf("%s (%v)", v.Name, v.Value)
	}
	return fmt.Sprintf("%v", v.Value)
}

func extractMaintainer(r resource) string {
	m, ok := r["_maintainer"].(map[string]interface{})
	if !ok {
		return ""
	}
	if name, ok := m["name"].(string); ok && name != "" {
		return name
	}
	if email, ok := m["email"].(string); ok && email != "" {
		return email
	}
	return ""
}

func markdownHeading(r resource) string {
	key := r["key"]
	name := r["name"]
	switch {
	case name != nil && key != nil:
		return fmt.Sprintf("## %s (%s)", fmt.Sprint(name), fmt.Sprint(key))
	case name != nil:
		return fmt.Sprintf("## %s", fmt.Sprint(name))
	case key != nil:
		return fmt.Sprintf("## %s", fmt.Sprint(key))
	default:
		return "## (unknown)"
	}
}

func escapeMDPipe(s string) string {
	return strings.ReplaceAll(s, "|", "\\|")
}
