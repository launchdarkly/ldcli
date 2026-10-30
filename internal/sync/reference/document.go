package reference

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"gopkg.in/yaml.v3"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
)

type markdownAdapter struct{}

func (markdownAdapter) Format() string { return "markdown" }
func (markdownAdapter) Inspect(bundle SourceBundle) (Inspection, error) {
	root, err := parseMarkdown(bundle.Primary)
	return Inspection{root: root}, err
}
func (markdownAdapter) Defaults(kind syncdomain.Kind) (string, map[string]string) {
	if kind == syncdomain.KindVariation {
		return "$", map[string]string{string(syncdomain.FieldPrompt): "$"}
	}
	return "$", nil
}

type agentSkillAdapter struct{}

func (agentSkillAdapter) Format() string { return "agent-skill" }
func (agentSkillAdapter) Inspect(bundle SourceBundle) (Inspection, error) {
	root, err := parseAgentSkill(bundle.Primary)
	return Inspection{root: root}, err
}
func (agentSkillAdapter) Defaults(kind syncdomain.Kind) (string, map[string]string) {
	if kind == syncdomain.KindSkill {
		return "$", map[string]string{
			string(syncdomain.FieldDescription): "frontmatter.description",
			string(syncdomain.FieldMarkdown):    "body",
		}
	}
	return "$", nil
}

func init() {
	mustRegister(markdownAdapter{})
	mustRegister(agentSkillAdapter{})
}

func parseMarkdown(document Document) (*sourceNode, error) {
	if !utf8.Valid(document.Data) {
		return nil, fmt.Errorf("%s: source is not valid UTF-8", document.Path)
	}
	data := document.Data
	root := markdownTextNode(data, 0, len(data))
	root.fields = map[string]*sourceNode{"body": root}
	root.fields["sections"] = &sourceNode{items: markdownSections(data), collection: true}
	return root, nil
}

func markdownSections(data []byte) []*sourceNode {
	type heading struct {
		level     int
		text      string
		lineStart int
		bodyStart int
	}
	var headings []heading
	var fenceCharacter byte
	fenceLength := 0
	for offset := 0; offset < len(data); {
		end := bytes.IndexByte(data[offset:], '\n')
		if end < 0 {
			end = len(data)
		} else {
			end += offset + 1
		}
		line := strings.TrimSuffix(strings.TrimSuffix(string(data[offset:end]), "\n"), "\r")
		trimmed := strings.TrimLeft(line, " ")
		indent := len(line) - len(trimmed)
		if indent > 3 || strings.HasPrefix(trimmed, "\t") {
			offset = end
			continue
		}
		marker, markerLength := markdownFence(trimmed)
		switch {
		case fenceLength == 0 && markerLength >= 3:
			fenceCharacter, fenceLength = marker, markerLength
		case fenceLength != 0 && marker == fenceCharacter && markerLength >= fenceLength:
			fenceCharacter, fenceLength = 0, 0
		case fenceLength == 0:
			level := 0
			for level < len(trimmed) && trimmed[level] == '#' {
				level++
			}
			if level > 0 && level <= 6 && level < len(trimmed) && trimmed[level] == ' ' {
				text := markdownHeadingText(trimmed[level:])
				headings = append(headings, heading{level: level, text: text, lineStart: offset, bodyStart: end})
			}
		}
		offset = end
	}

	sections := make([]*sourceNode, 0, len(headings))
	for index, current := range headings {
		bodyEnd := len(data)
		for next := index + 1; next < len(headings); next++ {
			if headings[next].level <= current.level {
				bodyEnd = headings[next].lineStart
				break
			}
		}
		body := markdownTextNode(data[current.bodyStart:bodyEnd], current.bodyStart, bodyEnd)
		sections = append(sections, &sourceNode{fields: map[string]*sourceNode{
			"heading": {value: current.text},
			"body":    body,
		}})
	}
	return sections
}

func markdownFence(line string) (byte, int) {
	if line == "" || line[0] != '`' && line[0] != '~' {
		return 0, 0
	}
	length := 1
	for length < len(line) && line[length] == line[0] {
		length++
	}
	return line[0], length
}

func markdownHeadingText(text string) string {
	text = strings.TrimSpace(text)
	lastSpace := strings.LastIndexByte(text, ' ')
	if lastSpace < 0 || lastSpace == len(text)-1 {
		return text
	}
	for _, character := range text[lastSpace+1:] {
		if character != '#' {
			return text
		}
	}
	return strings.TrimSpace(text[:lastSpace])
}

func markdownTextNode(data []byte, start, end int) *sourceNode {
	value := normalizeMarkdown(string(data))
	lineEnding := "\n"
	if bytes.Contains(data, []byte("\r\n")) {
		lineEnding = "\r\n"
	}
	contentStart, contentEnd := 0, len(data)
	for contentStart < len(data) && isMarkdownSpace(data[contentStart]) {
		contentStart++
	}
	for contentEnd > contentStart && isMarkdownSpace(data[contentEnd-1]) {
		contentEnd--
	}
	prefix := bytes.Clone(data[:contentStart])
	suffix := bytes.Clone(data[contentEnd:])
	return stringNode(value, start, end, func(text string) ([]byte, error) {
		text = normalizeMarkdown(text)
		text = strings.ReplaceAll(text, "\n", lineEnding)
		result := append(bytes.Clone(prefix), []byte(text)...)
		return append(result, suffix...), nil
	})
}

func isMarkdownSpace(value byte) bool {
	return value == ' ' || value == '\t' || value == '\r' || value == '\n'
}

func parseAgentSkill(document Document) (*sourceNode, error) {
	if !utf8.Valid(document.Data) {
		return nil, fmt.Errorf("%s: source is not valid UTF-8", document.Path)
	}
	frontMatter, bodyStart, err := splitFrontMatter(document.Data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", document.Path, err)
	}
	var yamlNode yaml.Node
	decoder := yaml.NewDecoder(bytes.NewReader(frontMatter.data))
	if err := decoder.Decode(&yamlNode); err != nil {
		return nil, fmt.Errorf("%s: invalid front matter: %w", document.Path, err)
	}
	description, err := yamlScalarNode(frontMatter, &yamlNode, "description")
	if err != nil {
		return nil, fmt.Errorf("%s: %w", document.Path, err)
	}
	body := document.Data[bodyStart:]
	if len(body) == 0 || strings.TrimSpace(string(body)) == "" {
		return nil, fmt.Errorf("%s: skill Markdown is required", document.Path)
	}
	bodyNode := exactMarkdownNode(body, bodyStart, len(document.Data))
	return &sourceNode{
		fields: map[string]*sourceNode{
			"frontmatter": {fields: map[string]*sourceNode{"description": description}},
			"body":        bodyNode,
		},
	}, nil
}

type frontMatterRegion struct {
	data        []byte
	startOffset int
}

func splitFrontMatter(data []byte) (frontMatterRegion, int, error) {
	offset := 0
	if bytes.HasPrefix(data, []byte("\ufeff")) {
		offset = len([]byte("\ufeff"))
	}
	firstEnd := nextLineEnd(data, offset)
	if strings.TrimSpace(string(data[offset:firstEnd])) != "---" {
		return frontMatterRegion{}, 0, fmt.Errorf("missing YAML front matter")
	}
	contentStart := firstEnd
	for lineStart := firstEnd; lineStart < len(data); {
		lineEnd := nextLineEnd(data, lineStart)
		if strings.TrimSpace(string(data[lineStart:lineEnd])) == "---" {
			bodyStart := lineEnd
			if bytes.HasPrefix(data[bodyStart:], []byte("\r\n")) {
				bodyStart += 2
			} else if bytes.HasPrefix(data[bodyStart:], []byte("\n")) {
				bodyStart++
			}
			return frontMatterRegion{data: data[contentStart:lineStart], startOffset: contentStart}, bodyStart, nil
		}
		lineStart = lineEnd
	}
	return frontMatterRegion{}, 0, fmt.Errorf("unclosed YAML front matter")
}

func nextLineEnd(data []byte, start int) int {
	if index := bytes.IndexByte(data[start:], '\n'); index >= 0 {
		return start + index + 1
	}
	return len(data)
}

func yamlScalarNode(region frontMatterRegion, document *yaml.Node, key string) (*sourceNode, error) {
	if len(document.Content) == 0 || document.Content[0].Kind != yaml.MappingNode {
		return nil, fmt.Errorf("front matter must be a mapping")
	}
	mapping := document.Content[0]
	for index := 0; index+1 < len(mapping.Content); index += 2 {
		name, value := mapping.Content[index], mapping.Content[index+1]
		if name.Value != key {
			continue
		}
		if value.Kind != yaml.ScalarNode || value.Tag != "!!str" {
			return nil, fmt.Errorf("front matter %s must be a string", key)
		}
		start, end, raw, err := yamlScalarSpan(region, value)
		if err != nil {
			return nil, err
		}
		if value.Style == yaml.LiteralStyle || value.Style == yaml.FoldedStyle {
			return &sourceNode{
				value: value.Value, start: start, end: end, capability: CapabilityReadOnly,
				reason: "YAML block scalars are read-only",
			}, nil
		}
		return stringNode(value.Value, start, end, func(text string) ([]byte, error) {
			return encodeYAMLString(text, raw)
		}), nil
	}
	return nil, syncdomain.NewProblem(
		syncdomain.ErrorPathNotFound,
		fmt.Sprintf("front matter field %q was not found", key),
		"Add the field or update the map path.",
	)
}

func yamlScalarSpan(region frontMatterRegion, node *yaml.Node) (int, int, string, error) {
	lines := bytes.SplitAfter(region.data, []byte("\n"))
	if node.Line < 1 || node.Line > len(lines) || node.Column < 1 {
		return 0, 0, "", fmt.Errorf("cannot locate YAML scalar")
	}
	start := region.startOffset
	for index := 0; index < node.Line-1; index++ {
		start += len(lines[index])
	}
	line := lines[node.Line-1]
	line = bytes.TrimRight(line, "\r\n")
	localStart := byteOffsetForColumn(line, node.Column)
	start += localStart
	localEnd := yamlTokenEnd(line, localStart)
	if localStart > localEnd {
		return 0, 0, "", fmt.Errorf("cannot locate YAML scalar")
	}
	raw := string(line[localStart:localEnd])
	return start, start + len(raw), raw, nil
}

func byteOffsetForColumn(line []byte, column int) int {
	offset := 0
	for current := 1; current < column && offset < len(line); current++ {
		_, width := utf8.DecodeRune(line[offset:])
		offset += width
	}
	return offset
}

func yamlTokenEnd(line []byte, start int) int {
	if start >= len(line) {
		return start
	}
	quote := line[start]
	if quote == '\'' || quote == '"' {
		escaped := false
		for index := start + 1; index < len(line); index++ {
			if quote == '"' && line[index] == '\\' && !escaped {
				escaped = true
				continue
			}
			if line[index] == quote && !escaped {
				if quote == '\'' && index+1 < len(line) && line[index+1] == '\'' {
					index++
					continue
				}
				return index + 1
			}
			escaped = false
		}
		return len(line)
	}
	end := len(line)
	if comment := bytes.Index(line[start:], []byte(" #")); comment >= 0 {
		end = start + comment
	}
	for _, delimiter := range []byte{',', ']', '}'} {
		if index := bytes.IndexByte(line[start:end], delimiter); index >= 0 {
			end = start + index
		}
	}
	for end > start && (line[end-1] == ' ' || line[end-1] == '\t') {
		end--
	}
	return end
}

func quoteYAML(value, previous string) string {
	if strings.HasPrefix(previous, "'") {
		if strings.ContainsAny(value, "\r\n") {
			encoded, _ := json.Marshal(value)
			return string(encoded)
		}
		return "'" + strings.ReplaceAll(value, "'", "''") + "'"
	}
	if strings.HasPrefix(previous, `"`) {
		encoded, _ := json.Marshal(value)
		return string(encoded)
	}
	if value != "" && !strings.ContainsAny(value, ":\n\r#{}[],&*!|>'\"%@`") &&
		value == strings.TrimSpace(value) {
		var decoded any
		if yaml.Unmarshal([]byte(value), &decoded) == nil {
			if _, isString := decoded.(string); isString {
				return value
			}
		}
	}
	encoded, _ := json.Marshal(value)
	return string(encoded)
}

func encodeYAMLString(value, previous string) ([]byte, error) {
	for _, character := range value {
		valid := character == '\n' || character == '\r' || character == '\t' ||
			character >= 0x20 && character <= 0xd7ff ||
			character >= 0xe000 && character <= 0xfffd ||
			character >= 0x10000 && character <= utf8.MaxRune
		if !valid || unicode.IsControl(character) && character != '\n' && character != '\r' && character != '\t' {
			return nil, fmt.Errorf("YAML string contains unsupported character U+%04X", character)
		}
	}
	return []byte(quoteYAML(value, previous)), nil
}

func exactMarkdownNode(data []byte, start, end int) *sourceNode {
	value := string(data)
	finalNewline := bytes.HasSuffix(data, []byte("\n"))
	return stringNode(value, start, end, func(text string) ([]byte, error) {
		if finalNewline && text != "" && !strings.HasSuffix(text, "\n") {
			text += "\n"
		}
		return []byte(text), nil
	})
}
