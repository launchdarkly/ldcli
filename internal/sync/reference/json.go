package reference

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"unicode/utf8"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
)

type jsonAdapter struct{}

func (jsonAdapter) Format() string { return "json" }
func (jsonAdapter) Inspect(bundle SourceBundle) (Inspection, error) {
	root, err := parseJSONDocument(bundle.Primary)
	return Inspection{root: root}, err
}
func (jsonAdapter) Defaults(syncdomain.Kind) (string, map[string]string) { return "$", nil }

func init() { mustRegister(jsonAdapter{}) }

type jsonParser struct {
	data   []byte
	offset int
}

func parseJSONDocument(document Document) (*sourceNode, error) {
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

func (parser *jsonParser) value() (*sourceNode, error) {
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
		return parser.keyword("true", true)
	case 'f':
		return parser.keyword("false", false)
	case 'n':
		return parser.keyword("null", nil)
	default:
		return parser.number()
	}
}

func (parser *jsonParser) object() (*sourceNode, error) {
	start := parser.offset
	parser.offset++
	parser.skipSpace()
	fields := map[string]*sourceNode{}
	value := map[string]any{}
	if parser.take('}') {
		return jsonObjectNode(value, fields, start, parser.offset), nil
	}
	for {
		keyNode, err := parser.string()
		if err != nil {
			return nil, err
		}
		key := keyNode.value.(string)
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
		fields[key], value[key] = item, item.value
		parser.skipSpace()
		if parser.take('}') {
			return jsonObjectNode(value, fields, start, parser.offset), nil
		}
		if !parser.take(',') {
			return nil, parser.problem("JSON object needs a comma or closing brace")
		}
		parser.skipSpace()
	}
}

func (parser *jsonParser) array() (*sourceNode, error) {
	start := parser.offset
	parser.offset++
	parser.skipSpace()
	var items []*sourceNode
	var values []any
	if parser.take(']') {
		return jsonArrayNode(values, items, start, parser.offset), nil
	}
	for {
		item, err := parser.value()
		if err != nil {
			return nil, err
		}
		items, values = append(items, item), append(values, item.value)
		parser.skipSpace()
		if parser.take(']') {
			return jsonArrayNode(values, items, start, parser.offset), nil
		}
		if !parser.take(',') {
			return nil, parser.problem("JSON array needs a comma or closing bracket")
		}
		parser.skipSpace()
	}
}

func (parser *jsonParser) string() (*sourceNode, error) {
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
			return stringNode(value, start, parser.offset, func(text string) ([]byte, error) {
				return json.Marshal(text)
			}), nil
		case next < 0x20:
			return nil, parser.problem("JSON string contains a control character")
		}
	}
	return nil, parser.problem("JSON string is not closed")
}

func (parser *jsonParser) number() (*sourceNode, error) {
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
	return numberNode(value, start, parser.offset, func(number float64) ([]byte, error) {
		return []byte(strconv.FormatFloat(number, 'g', -1, 64)), nil
	}), nil
}

func (parser *jsonParser) keyword(keyword string, value any) (*sourceNode, error) {
	start := parser.offset
	if !bytes.HasPrefix(parser.data[start:], []byte(keyword)) {
		return nil, parser.problem("JSON value is invalid")
	}
	parser.offset += len(keyword)
	return &sourceNode{
		value: value, start: start, end: parser.offset, capability: CapabilityDirect,
		encode: func(next any) ([]byte, error) { return json.Marshal(next) },
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

func jsonObjectNode(value map[string]any, fields map[string]*sourceNode, start, end int) *sourceNode {
	return &sourceNode{
		value: value, fields: fields, start: start, end: end, capability: CapabilityDirect,
		encode: func(next any) ([]byte, error) { return json.Marshal(next) },
	}
}

func jsonArrayNode(value []any, items []*sourceNode, start, end int) *sourceNode {
	return &sourceNode{
		value: value, items: items, collection: true, start: start, end: end, capability: CapabilityDirect,
		encode: func(next any) ([]byte, error) { return json.Marshal(next) },
	}
}
