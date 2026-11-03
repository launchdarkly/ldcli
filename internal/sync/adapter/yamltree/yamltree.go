// Package yamltree builds logical nodes from a YAML document, with the exact
// byte span of each scalar. The YAML adapter and Markdown front matter use it.
package yamltree

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"gopkg.in/yaml.v3"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
	"github.com/launchdarkly/ldcli/internal/sync/adapter"
)

// Region is a YAML document, or part of one, and the byte offset of its first
// byte in the file that contains it.
type Region struct {
	Data        []byte
	StartOffset int
}

// ScalarSpan returns the byte span and the raw text of one scalar. flow
// is true inside a flow sequence or a flow mapping, where a comma or a closing
// bracket ends a plain scalar. A plain scalar that spans lines has no exact
// span, so the caller treats it as read-only.
func ScalarSpan(region Region, node *yaml.Node, flow bool) (start, end int, raw string, exact bool, err error) {
	lines := bytes.SplitAfter(region.Data, []byte("\n"))
	if node.Line < 1 || node.Line > len(lines) || node.Column < 1 {
		return 0, 0, "", false, fmt.Errorf("cannot locate YAML scalar")
	}
	start = region.StartOffset
	for index := 0; index < node.Line-1; index++ {
		start += len(lines[index])
	}
	line := bytes.TrimRight(lines[node.Line-1], "\r\n")
	localStart := byteOffsetForColumn(line, node.Column)
	if localStart > len(line) {
		return 0, 0, "", false, fmt.Errorf("cannot locate YAML scalar")
	}
	rest := line[localStart:]
	switch {
	case len(rest) != 0 && (rest[0] == '\'' || rest[0] == '"'):
		raw = string(rest[:yamlQuotedEnd(rest)])
		exact = true
	default:
		raw = string(yamlPlainScalar(rest, flow))
		exact = raw == node.Value
	}
	start += localStart
	return start, start + len(raw), raw, exact, nil
}

func byteOffsetForColumn(line []byte, column int) int {
	offset := 0
	for current := 1; current < column && offset < len(line); current++ {
		_, width := utf8.DecodeRune(line[offset:])
		offset += width
	}
	return offset
}

// yamlQuotedEnd returns the length of the quoted scalar at the start of text.
func yamlQuotedEnd(text []byte) int {
	quote := text[0]
	for index := 1; index < len(text); index++ {
		switch {
		case quote == '"' && text[index] == '\\':
			index++
		case text[index] == quote && quote == '\'' && index+1 < len(text) && text[index+1] == '\'':
			index++
		case text[index] == quote:
			return index + 1
		}
	}
	return len(text)
}

// yamlPlainScalar returns the plain scalar at the start of text. A space and
// a hash start a comment. In a flow collection, a comma or a closing bracket
// ends the scalar.
func yamlPlainScalar(text []byte, flow bool) []byte {
	end := len(text)
	if comment := bytes.Index(text, []byte(" #")); comment >= 0 {
		end = comment
	}
	if flow {
		if index := bytes.IndexAny(text[:end], ",]}"); index >= 0 {
			end = index
		}
	}
	return bytes.TrimRight(text[:end], " \t")
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

// EncodeString writes a string in the quoting style of the scalar that it
// replaces. It rejects characters that YAML cannot hold.
func EncodeString(value, previous string) ([]byte, error) {
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

// Build builds the logical node of one YAML node. flow is true
// inside a flow sequence or a flow mapping.
func Build(data []byte, node *yaml.Node, offset int, flow bool) (*adapter.Node, error) {
	flow = flow || node.Style&yaml.FlowStyle != 0
	switch node.Kind {
	case yaml.MappingNode:
		return yamlMappingNode(data, node, offset, flow)
	case yaml.SequenceNode:
		return yamlSequenceNode(data, node, offset, flow)
	case yaml.ScalarNode:
		return yamlValueNode(data, node, offset, flow)
	case yaml.AliasNode:
		value, err := Build(data, node.Alias, offset, flow)
		if value != nil {
			value.MarkReadOnly("YAML aliases are read-only")
		}
		return value, err
	default:
		return nil, syncdomain.NewProblem(
			syncdomain.ErrorInvalidSource, "YAML node is not supported", "Use mappings, lists, or scalar values.",
		)
	}
}

func yamlMappingNode(data []byte, node *yaml.Node, offset int, flow bool) (*adapter.Node, error) {
	fields := make(map[string]*adapter.Node, len(node.Content)/2)
	value := make(map[string]any, len(node.Content)/2)
	unsafe := node.Anchor != "" || customYAMLTag(node.Tag)
	reason := "YAML anchors and tags are read-only"
	for index := 0; index+1 < len(node.Content); index += 2 {
		keyNode, valueNode := node.Content[index], node.Content[index+1]
		if keyNode.Kind != yaml.ScalarNode || keyNode.Tag != "!!str" && keyNode.Tag != "!!merge" {
			return nil, syncdomain.NewProblem(
				syncdomain.ErrorInvalidSource,
				"YAML mapping key is not a string",
				"Use direct string keys.",
			)
		}
		key := keyNode.Value
		if _, duplicate := fields[key]; duplicate {
			return nil, syncdomain.NewProblem(
				syncdomain.ErrorDuplicateKey,
				fmt.Sprintf("YAML mapping contains duplicate key %q", key),
				"Remove the duplicate key.",
			)
		}
		child, err := Build(data, valueNode, offset, flow)
		if err != nil {
			return nil, err
		}
		fields[key], value[key] = child, child.Value
		if key == "<<" {
			unsafe, reason = true, "YAML merge keys are read-only"
			for mergedKey, mergedValue := range child.Fields {
				if _, exists := fields[mergedKey]; !exists {
					fields[mergedKey] = mergedValue
					value[mergedKey] = mergedValue.Value
				}
			}
		}
	}
	result := &adapter.Node{
		Value: value, Fields: fields, Capability: adapter.CapabilityReadOnly,
		Reason: "YAML structural changes are read-only",
	}
	if unsafe {
		result.MarkReadOnly(reason)
	}
	return result, nil
}

func yamlSequenceNode(data []byte, node *yaml.Node, offset int, flow bool) (*adapter.Node, error) {
	items := make([]*adapter.Node, 0, len(node.Content))
	values := make([]any, 0, len(node.Content))
	for _, childNode := range node.Content {
		child, err := Build(data, childNode, offset, flow)
		if err != nil {
			return nil, err
		}
		items, values = append(items, child), append(values, child.Value)
	}
	result := &adapter.Node{
		Value: values, Items: items, Collection: true, Capability: adapter.CapabilityReadOnly,
		Reason: "YAML structural changes are read-only",
	}
	if node.Anchor != "" || customYAMLTag(node.Tag) {
		result.MarkReadOnly("YAML anchors and tags are read-only")
	}
	return result, nil
}

func yamlValueNode(data []byte, node *yaml.Node, offset int, flow bool) (*adapter.Node, error) {
	region := Region{Data: data, StartOffset: offset}
	start, end, raw, exact, err := ScalarSpan(region, node, flow)
	if err != nil {
		return nil, err
	}
	readOnly := !exact || node.Anchor != "" || customYAMLTag(node.Tag) ||
		node.Style == yaml.LiteralStyle || node.Style == yaml.FoldedStyle
	reason := "YAML anchors, tags, block scalars, and multi-line scalars are read-only"
	var result *adapter.Node
	switch node.Tag {
	case "!!str":
		result = adapter.String(node.Value, start, end, func(text string) ([]byte, error) {
			return EncodeString(text, raw)
		})
	case "!!bool":
		value, err := strconv.ParseBool(node.Value)
		if err != nil {
			return nil, err
		}
		result = adapter.Bool(value, start, end, nil)
	case "!!int", "!!float":
		number, err := strconv.ParseFloat(strings.ReplaceAll(node.Value, "_", ""), 64)
		if err != nil {
			return nil, err
		}
		result = adapter.Number(number, start, end, func(next float64) ([]byte, error) {
			return []byte(strconv.FormatFloat(next, 'g', -1, 64)), nil
		})
	default:
		result = adapter.ReadOnly(start, end, "YAML scalar type is read-only")
		result.Value = node.Value
	}
	if readOnly {
		result.Capability, result.Reason, result.Encode = adapter.CapabilityReadOnly, reason, nil
	}
	return result, nil
}

func customYAMLTag(tag string) bool {
	return tag != "" && !strings.HasPrefix(tag, "!!")
}
