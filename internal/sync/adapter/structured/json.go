// Package structured inspects JSON, YAML, and TOML documents with exact byte
// spans for each scalar value.
package structured

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"unicode/utf8"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
	"github.com/launchdarkly/ldcli/internal/sync/adapter"
)

type jsonAdapter struct{}

func (jsonAdapter) Format() string   { return "json" }
func (jsonAdapter) Behavior() string { return "json/1" }

func (jsonAdapter) Inspect(bundle adapter.SourceBundle) (*adapter.Node, error) {
	return parseJSONDocument(bundle.Primary)
}

func (jsonAdapter) Defaults(syncdomain.Kind) (string, map[string]string) { return "$", nil }

func init() { adapter.MustRegister(jsonAdapter{}) }

type jsonParser struct {
	data   []byte
	offset int
}

func parseJSONDocument(document adapter.Document) (*adapter.Node, error) {
	if !utf8.Valid(document.Data) {
		return nil, syncdomain.NewProblem(
			syncdomain.ErrorInvalidSource,
			"JSON source is not valid UTF-8",
			"Save the file as UTF-8.",
		).WithFile(document.Path, 0, 0)
	}
	parser := jsonParser{data: document.Data}
	if bytes.HasPrefix(parser.data, []byte("\ufeff")) {
		parser.offset = len([]byte("\ufeff"))
	}
	parser.skipSpace()
	root, err := parser.value()
	if err != nil {
		return nil, err
	}
	parser.skipSpace()
	if parser.offset != len(parser.data) {
		return nil, parser.problem("JSON source has trailing input")
	}
	return root, nil
}

func (parser *jsonParser) value() (*adapter.Node, error) {
	parser.skipSpace()
	if parser.offset >= len(parser.data) {
		return nil, parser.problem("JSON value is missing")
	}
	switch parser.data[parser.offset] {
	case '{':
		return parser.object()
	case '[':
		return parser.array()
	case '"':
		return parser.string()
	case 't':
		return parser.boolean("true")
	case 'f':
		return parser.boolean("false")
	case 'n':
		return parser.keyword("null", nil)
	default:
		return parser.number()
	}
}

func (parser *jsonParser) object() (*adapter.Node, error) {
	start := parser.offset
	parser.offset++
	parser.skipSpace()
	fields := map[string]*adapter.Node{}
	value := map[string]any{}
	if parser.take('}') {
		return parser.objectNode(value, fields, start, parser.offset), nil
	}
	for {
		keyNode, err := parser.string()
		if err != nil {
			return nil, err
		}
		key := keyNode.Value.(string)
		if _, duplicate := fields[key]; duplicate {
			return nil, syncdomain.NewProblem(
				syncdomain.ErrorDuplicateKey,
				fmt.Sprintf("JSON object contains duplicate key %q", key),
				"Remove the duplicate key.",
			)
		}
		parser.skipSpace()
		if !parser.take(':') {
			return nil, parser.problem("JSON object key needs a colon")
		}
		item, err := parser.value()
		if err != nil {
			return nil, err
		}
		fields[key], value[key] = item, item.Value
		parser.skipSpace()
		if parser.take('}') {
			return parser.objectNode(value, fields, start, parser.offset), nil
		}
		if !parser.take(',') {
			return nil, parser.problem("JSON object needs a comma or closing brace")
		}
		parser.skipSpace()
	}
}

func (parser *jsonParser) array() (*adapter.Node, error) {
	start := parser.offset
	parser.offset++
	parser.skipSpace()
	// An empty array is a value of its own, so it never reads as null.
	items, values := []*adapter.Node{}, []any{}
	if parser.take(']') {
		return parser.arrayNode(values, items, start, parser.offset), nil
	}
	for {
		item, err := parser.value()
		if err != nil {
			return nil, err
		}
		items, values = append(items, item), append(values, item.Value)
		parser.skipSpace()
		if parser.take(']') {
			return parser.arrayNode(values, items, start, parser.offset), nil
		}
		if !parser.take(',') {
			return nil, parser.problem("JSON array needs a comma or closing bracket")
		}
		parser.skipSpace()
	}
}

func (parser *jsonParser) string() (*adapter.Node, error) {
	start := parser.offset
	if !parser.take('"') {
		return nil, parser.problem("JSON string must start with a quote")
	}
	escaped := false
	for parser.offset < len(parser.data) {
		next := parser.data[parser.offset]
		parser.offset++
		switch {
		case escaped:
			escaped = false
		case next == '\\':
			escaped = true
		case next == '"':
			raw := parser.data[start:parser.offset]
			var value string
			if err := json.Unmarshal(raw, &value); err != nil {
				return nil, parser.problem("JSON string is invalid")
			}
			return adapter.String(value, start, parser.offset, func(text string) ([]byte, error) {
				return adapter.EncodeJSON(text, "", "")
			}), nil
		case next < 0x20:
			return nil, parser.problem("JSON string contains a control character")
		}
	}
	return nil, parser.problem("JSON string is not closed")
}

func (parser *jsonParser) number() (*adapter.Node, error) {
	start := parser.offset
	for parser.offset < len(parser.data) && bytes.ContainsRune([]byte("-+0123456789.eE"), rune(parser.data[parser.offset])) {
		parser.offset++
	}
	if start == parser.offset {
		return nil, parser.problem("JSON value is invalid")
	}
	raw := parser.data[start:parser.offset]
	var value float64
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, parser.problem("JSON number is invalid")
	}
	// An integer beyond 53 bits has no exact float64, so it stays read-only
	// text instead of a rounded number.
	if !bytes.ContainsAny(raw, ".eE") {
		if integer, err := strconv.ParseInt(string(raw), 10, 64); err != nil || !adapter.ExactInteger(integer) {
			result := adapter.ReadOnly(start, parser.offset, "JSON integer is too large to represent exactly")
			result.Value = string(raw)
			return result, nil
		}
	}
	return adapter.Number(value, start, parser.offset, func(number float64) ([]byte, error) {
		return []byte(strconv.FormatFloat(number, 'g', -1, 64)), nil
	}), nil
}

// boolean reads true or false. The leaf accepts only a boolean on a write,
// as a string or a number leaf accepts only its own type.
func (parser *jsonParser) boolean(keyword string) (*adapter.Node, error) {
	start := parser.offset
	if !bytes.HasPrefix(parser.data[start:], []byte(keyword)) {
		return nil, parser.problem("JSON value is invalid")
	}
	parser.offset += len(keyword)
	return adapter.Bool(keyword == "true", start, parser.offset, nil), nil
}

// keyword reads null. A null slot has no type of its own, so a write of any
// JSON value fills it.
func (parser *jsonParser) keyword(keyword string, value any) (*adapter.Node, error) {
	start := parser.offset
	if !bytes.HasPrefix(parser.data[start:], []byte(keyword)) {
		return nil, parser.problem("JSON value is invalid")
	}
	parser.offset += len(keyword)
	return &adapter.Node{
		Value: value, Start: start, End: parser.offset, Capability: adapter.CapabilityDirect,
		Encode: func(next any) ([]byte, error) { return adapter.EncodeJSON(next, "", "") },
	}, nil
}

func (parser *jsonParser) skipSpace() {
	for parser.offset < len(parser.data) && bytes.ContainsRune([]byte(" \t\r\n"), rune(parser.data[parser.offset])) {
		parser.offset++
	}
}

func (parser *jsonParser) take(expected byte) bool {
	if parser.offset >= len(parser.data) || parser.data[parser.offset] != expected {
		return false
	}
	parser.offset++
	return true
}

func (parser *jsonParser) problem(message string) error {
	return syncdomain.NewProblem(
		syncdomain.ErrorInvalidSource,
		fmt.Sprintf("%s at byte %d", message, parser.offset),
		"Fix the JSON syntax.",
	)
}

func (parser *jsonParser) objectNode(value map[string]any, fields map[string]*adapter.Node, start, end int) *adapter.Node {
	result := &adapter.Node{
		Value: value, Fields: fields, Start: start, End: end, Capability: adapter.CapabilityDirect,
		Encode: adapter.JSONEncoder(parser.data[start:end]),
	}
	for _, field := range fields {
		jsonContainerHolds(result, field)
	}
	return result
}

func (parser *jsonParser) arrayNode(value []any, items []*adapter.Node, start, end int) *adapter.Node {
	result := &adapter.Node{
		Value: value, Items: items, Collection: true, Start: start, End: end, Capability: adapter.CapabilityDirect,
		Encode: adapter.JSONEncoder(parser.data[start:end]),
	}
	for _, item := range items {
		jsonContainerHolds(result, item)
	}
	return result
}

// jsonContainerHolds makes a container read-only when a value in it is, such
// as an integer that a float64 cannot hold exactly. A write of the whole
// container would replace that value with a rounded one.
func jsonContainerHolds(container, child *adapter.Node) {
	if child == nil || child.Capability == adapter.CapabilityDirect || container.Capability != adapter.CapabilityDirect {
		return
	}
	container.Capability = adapter.CapabilityReadOnly
	container.Reason = "JSON container holds a read-only value: " + child.Reason
	container.Encode = nil
}
