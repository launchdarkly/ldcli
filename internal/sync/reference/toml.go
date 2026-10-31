package reference

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	toml "github.com/pelletier/go-toml/v2"
	"github.com/pelletier/go-toml/v2/unstable"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
)

type tomlAdapter struct{}

func (tomlAdapter) Format() string { return "toml" }
func (tomlAdapter) Inspect(bundle SourceBundle) (Inspection, error) {
	root, err := parseTOMLDocument(bundle.Primary)
	return Inspection{root: root}, err
}
func (tomlAdapter) Defaults(syncdomain.Kind) (string, map[string]string) { return "$", nil }

func init() { mustRegister(tomlAdapter{}) }

func parseTOMLDocument(document Document) (*sourceNode, error) {
	if !utf8.Valid(document.Data) {
		return nil, syncdomain.NewProblem(
			syncdomain.ErrorInvalidSource,
			"TOML source is not valid UTF-8",
			"Save the file as UTF-8.",
		).WithFile(document.Path, 0, 0)
	}
	data := document.Data
	offset := 0
	if bytes.HasPrefix(data, []byte("\ufeff")) {
		offset = len([]byte("\ufeff"))
		data = data[offset:]
	}
	var parser unstable.Parser
	parser.Reset(data)
	root := &sourceNode{value: map[string]any{}, fields: map[string]*sourceNode{}}
	table := root
	for parser.NextExpression() {
		expression := parser.Expression()
		switch expression.Kind {
		case unstable.KeyValue:
			keys := tomlKeys(expression)
			if err := addTOMLValue(data, offset, table, keys, expression.Value()); err != nil {
				return nil, err
			}
		case unstable.Table:
			var err error
			table, err = tomlTable(root, tomlKeys(expression), false)
			if err != nil {
				return nil, err
			}
		case unstable.ArrayTable:
			var err error
			table, err = tomlTable(root, tomlKeys(expression), true)
			if err != nil {
				return nil, err
			}
		}
	}
	if err := parser.Error(); err != nil {
		return nil, syncdomain.WrapProblem(syncdomain.NewProblem(
			syncdomain.ErrorInvalidSource, "TOML source is invalid", "Fix the TOML syntax.",
		).WithFile(document.Path, 0, 0), err)
	}
	return root, nil
}

func tomlKeys(node *unstable.Node) []string {
	var keys []string
	iterator := node.Key()
	for iterator.Next() {
		keys = append(keys, string(iterator.Node().Data))
	}
	return keys
}

func addTOMLValue(data []byte, offset int, table *sourceNode, keys []string, valueNode *unstable.Node) error {
	if len(keys) == 0 {
		return syncdomain.NewProblem(
			syncdomain.ErrorInvalidSource, "TOML key is missing", "Add a key to the value.",
		)
	}
	parent, err := tomlObjectPath(table, keys[:len(keys)-1])
	if err != nil {
		return err
	}
	key := keys[len(keys)-1]
	if _, duplicate := parent.fields[key]; duplicate {
		return syncdomain.NewProblem(
			syncdomain.ErrorDuplicateKey,
			fmt.Sprintf("TOML table contains duplicate key %q", strings.Join(keys, ".")),
			"Remove the duplicate key.",
		)
	}
	value, err := tomlValueNode(data, offset, valueNode)
	if err != nil {
		return err
	}
	if table.capability == CapabilityReadOnly {
		markReadOnly(value, table.reason)
	}
	parent.fields[key] = value
	parent.value.(map[string]any)[key] = value.value
	return nil
}

func tomlTable(root *sourceNode, keys []string, array bool) (*sourceNode, error) {
	if !array {
		return tomlObjectPath(root, keys)
	}
	if len(keys) == 0 {
		return nil, syncdomain.NewProblem(
			syncdomain.ErrorInvalidSource, "TOML array table key is missing", "Add a table key.",
		)
	}
	parent, err := tomlObjectPath(root, keys[:len(keys)-1])
	if err != nil {
		return nil, err
	}
	key := keys[len(keys)-1]
	collection := parent.fields[key]
	if collection == nil {
		collection = &sourceNode{
			value: []any{}, capability: CapabilityReadOnly,
			reason: "TOML arrays of tables are read-only",
		}
		parent.fields[key] = collection
		parent.value.(map[string]any)[key] = collection.value
	}
	if collection.items == nil && len(collection.value.([]any)) != 0 {
		return nil, syncdomain.NewProblem(
			syncdomain.ErrorDuplicateKey,
			fmt.Sprintf("TOML key %q has incompatible definitions", strings.Join(keys, ".")),
			"Keep one table definition.",
		)
	}
	item := &sourceNode{
		value: map[string]any{}, fields: map[string]*sourceNode{},
		capability: CapabilityReadOnly, reason: "TOML arrays of tables are read-only",
	}
	collection.items = append(collection.items, item)
	collection.value = append(collection.value.([]any), item.value)
	parent.value.(map[string]any)[key] = collection.value
	return item, nil
}

func tomlObjectPath(root *sourceNode, keys []string) (*sourceNode, error) {
	current := root
	for _, key := range keys {
		child := current.fields[key]
		if child == nil {
			child = &sourceNode{value: map[string]any{}, fields: map[string]*sourceNode{}}
			current.fields[key] = child
			current.value.(map[string]any)[key] = child.value
		}
		if child.fields == nil {
			return nil, syncdomain.NewProblem(
				syncdomain.ErrorDuplicateKey,
				fmt.Sprintf("TOML key %q is already a value", key),
				"Keep one compatible key definition.",
			)
		}
		current = child
	}
	return current, nil
}

func tomlValueNode(data []byte, offset int, node *unstable.Node) (*sourceNode, error) {
	start := offset + int(node.Raw.Offset)
	end := start + int(node.Raw.Length)
	raw := data[node.Raw.Offset : node.Raw.Offset+node.Raw.Length]
	switch node.Kind {
	case unstable.String:
		return stringNode(string(node.Data), start, end, func(value string) ([]byte, error) {
			return encodeTOMLString(value, string(raw))
		}), nil
	case unstable.Bool:
		value := string(node.Data) == "true"
		return &sourceNode{
			value: value, start: start, end: end, capability: CapabilityDirect,
			encode: func(next any) ([]byte, error) {
				value, ok := next.(bool)
				if !ok {
					return nil, mapTypeError("source", "a boolean")
				}
				return []byte(strconv.FormatBool(value)), nil
			},
		}, nil
	case unstable.Integer, unstable.Float:
		var decoded map[string]any
		if err := toml.Unmarshal(append([]byte("value = "), raw...), &decoded); err != nil {
			return nil, err
		}
		number, ok := sourceNumber(decoded["value"])
		if !ok {
			return readOnlyNode(start, end, "TOML number is not representable"), nil
		}
		return numberNode(number, start, end, func(next float64) ([]byte, error) {
			return []byte(strconv.FormatFloat(next, 'g', -1, 64)), nil
		}), nil
	case unstable.Array:
		return tomlArrayNode(data, offset, node)
	case unstable.InlineTable:
		return tomlInlineTableNode(data, offset, node)
	default:
		result := readOnlyNode(start, end, "TOML date and time values are read-only")
		result.value = string(node.Data)
		return result, nil
	}
}

func tomlArrayNode(data []byte, offset int, node *unstable.Node) (*sourceNode, error) {
	var items []*sourceNode
	var values []any
	iterator := node.Children()
	for iterator.Next() {
		child := iterator.Node()
		if child.Kind == unstable.Comment {
			continue
		}
		item, err := tomlValueNode(data, offset, child)
		if err != nil {
			return nil, err
		}
		items, values = append(items, item), append(values, item.value)
	}
	return &sourceNode{
		value: values, items: items, collection: true, capability: CapabilityReadOnly,
		reason: "TOML structural changes are read-only",
	}, nil
}

func tomlInlineTableNode(data []byte, offset int, node *unstable.Node) (*sourceNode, error) {
	result := &sourceNode{
		value: map[string]any{}, fields: map[string]*sourceNode{},
		capability: CapabilityDirect,
	}
	iterator := node.Children()
	for iterator.Next() {
		child := iterator.Node()
		if child.Kind != unstable.KeyValue {
			continue
		}
		if err := addTOMLValue(data, offset, result, tomlKeys(child), child.Value()); err != nil {
			return nil, err
		}
	}
	result.capability = CapabilityReadOnly
	result.reason = "TOML structural changes are read-only"
	return result, nil
}

func encodeTOMLString(value, previous string) ([]byte, error) {
	switch {
	case strings.HasPrefix(previous, `"""`):
		return []byte(`"""` + strings.ReplaceAll(value, `"""`, `\"""`) + `"""`), nil
	case strings.HasPrefix(previous, `'''`):
		if strings.Contains(value, `'''`) {
			return nil, fmt.Errorf("multiline literal string cannot contain three apostrophes")
		}
		return []byte(`'''` + value + `'''`), nil
	case strings.HasPrefix(previous, `'`):
		if strings.ContainsAny(value, "'\r\n") {
			return nil, fmt.Errorf("literal string cannot represent the new value")
		}
		return []byte("'" + value + "'"), nil
	default:
		return []byte(encodeTOMLBasicString(value)), nil
	}
}

func encodeTOMLBasicString(value string) string {
	var encoded strings.Builder
	encoded.WriteByte('"')
	for _, character := range value {
		switch character {
		case '"', '\\':
			encoded.WriteByte('\\')
			encoded.WriteRune(character)
		case '\b':
			encoded.WriteString(`\b`)
		case '\t':
			encoded.WriteString(`\t`)
		case '\n':
			encoded.WriteString(`\n`)
		case '\f':
			encoded.WriteString(`\f`)
		case '\r':
			encoded.WriteString(`\r`)
		default:
			if character < 0x20 || character >= 0x7f && character <= 0x9f {
				fmt.Fprintf(&encoded, `\u%04X`, character)
			} else {
				encoded.WriteRune(character)
			}
		}
	}
	encoded.WriteByte('"')
	return encoded.String()
}
