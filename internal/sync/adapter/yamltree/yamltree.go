// Package yamltree builds logical nodes from a YAML document, with the exact
// byte span of each scalar. The YAML adapter and Markdown front matter use it.
package yamltree

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
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
		var closed bool
		end, closed := yamlQuotedEnd(rest)
		raw, exact = string(rest[:end]), closed
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

// yamlQuotedEnd returns the length of the quoted scalar at the start of text,
// and whether its closing quote is on this line. A quoted scalar that
// continues on the next line has no exact span.
func yamlQuotedEnd(text []byte) (int, bool) {
	quote := text[0]
	for index := 1; index < len(text); index++ {
		switch {
		case quote == '"' && text[index] == '\\':
			index++
		case text[index] == quote && quote == '\'' && index+1 < len(text) && text[index+1] == '\'':
			index++
		case text[index] == quote:
			return index + 1, true
		}
	}
	return len(text), false
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
	return build(data, node, offset, flow, map[*yaml.Node]bool{})
}

// build is Build with the set of anchored nodes that are still being built.
// An alias to one of those nodes is a cycle, which has no finite value.
func build(data []byte, node *yaml.Node, offset int, flow bool, open map[*yaml.Node]bool) (*adapter.Node, error) {
	if node.Anchor != "" {
		if open[node] {
			return nil, syncdomain.NewProblem(
				syncdomain.ErrorInvalidSource,
				fmt.Sprintf("YAML alias *%s refers to a node that contains it", node.Anchor),
				"Remove the circular alias.",
			)
		}
		open[node] = true
		defer delete(open, node)
	}
	flow = flow || node.Style&yaml.FlowStyle != 0
	switch node.Kind {
	case yaml.MappingNode:
		return yamlMappingNode(data, node, offset, flow, open)
	case yaml.SequenceNode:
		return yamlSequenceNode(data, node, offset, flow, open)
	case yaml.ScalarNode:
		return yamlValueNode(data, node, offset, flow)
	case yaml.AliasNode:
		value, err := build(data, node.Alias, offset, flow, open)
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

func yamlMappingNode(data []byte, node *yaml.Node, offset int, flow bool, open map[*yaml.Node]bool) (*adapter.Node, error) {
	fields := make(map[string]*adapter.Node, len(node.Content)/2)
	value := make(map[string]any, len(node.Content)/2)
	unsafe := node.Anchor != "" || customYAMLTag(node.Tag)
	reason := "YAML anchors and tags are read-only"
	var merges []*adapter.Node
	merged := false
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
		if _, duplicate := fields[key]; duplicate || key == "<<" && merged {
			return nil, syncdomain.NewProblem(
				syncdomain.ErrorDuplicateKey,
				fmt.Sprintf("YAML mapping contains duplicate key %q", key),
				"Remove the duplicate key.",
			)
		}
		child, err := build(data, valueNode, offset, flow, open)
		if err != nil {
			return nil, err
		}
		if key == "<<" {
			unsafe, reason, merged = true, "YAML merge keys are read-only", true
			switch {
			case child.Fields != nil:
				merges = []*adapter.Node{child}
			case child.Collection:
				merges = child.Items
			default:
				return nil, invalidMerge()
			}
			continue
		}
		fields[key], value[key] = child, child.Value
	}
	// Merged keys come last: an explicit key wins over a merged key, and an
	// earlier mapping in a merge list wins over a later one.
	for _, merged := range merges {
		if merged.Fields == nil {
			return nil, invalidMerge()
		}
		for mergedKey, mergedValue := range merged.Fields {
			if _, exists := fields[mergedKey]; !exists {
				fields[mergedKey] = mergedValue
				value[mergedKey] = mergedValue.Value
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

func invalidMerge() error {
	return syncdomain.NewProblem(
		syncdomain.ErrorInvalidSource,
		"YAML merge key value is not a mapping or a list of mappings",
		"Merge one mapping or a list of mappings.",
	)
}

func yamlSequenceNode(data []byte, node *yaml.Node, offset int, flow bool, open map[*yaml.Node]bool) (*adapter.Node, error) {
	items := make([]*adapter.Node, 0, len(node.Content))
	values := make([]any, 0, len(node.Content))
	for _, childNode := range node.Content {
		child, err := build(data, childNode, offset, flow, open)
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
		var value bool
		if err := node.Decode(&value); err != nil {
			result = unreadableScalar(node, start, end)
			break
		}
		result = adapter.Bool(value, start, end, nil)
	case "!!int", "!!float":
		var number float64
		if err := node.Decode(&number); err != nil || math.IsInf(number, 0) || math.IsNaN(number) {
			result = unreadableScalar(node, start, end)
			break
		}
		result = adapter.Number(number, start, end, func(next float64) ([]byte, error) {
			return []byte(strconv.FormatFloat(next, 'g', -1, 64)), nil
		})
	case "!!null":
		result = adapter.ReadOnly(start, end, "YAML null values are read-only")
	default:
		result = adapter.ReadOnly(start, end, "YAML scalar type is read-only")
		result.Value = node.Value
	}
	if readOnly {
		result.Capability, result.Reason, result.Encode = adapter.CapabilityReadOnly, reason, nil
	}
	return result, nil
}

// unreadableScalar keeps a scalar that the YAML library cannot decode, or
// that has no JSON form such as infinity, as a read-only value, so that one
// odd spelling does not reject the document.
func unreadableScalar(node *yaml.Node, start, end int) *adapter.Node {
	result := adapter.ReadOnly(start, end, "YAML scalar spelling is read-only")
	result.Value = node.Value
	return result
}

func customYAMLTag(tag string) bool {
	return tag != "" && !strings.HasPrefix(tag, "!!")
}
