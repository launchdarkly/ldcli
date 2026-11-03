package structured

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	toml "github.com/pelletier/go-toml/v2"
	"github.com/pelletier/go-toml/v2/unstable"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
	"github.com/launchdarkly/ldcli/internal/sync/adapter"
)

type tomlAdapter struct{}

func (tomlAdapter) Format() string   { return "toml" }
func (tomlAdapter) Behavior() string { return "toml/1" }

func (tomlAdapter) Inspect(bundle adapter.SourceBundle) (*adapter.Node, error) {
	return parseTOMLDocument(bundle.Primary)
}

func (tomlAdapter) Defaults(syncdomain.Kind) (string, map[string]string) { return "$", nil }

func init() { adapter.MustRegister(tomlAdapter{}) }

func parseTOMLDocument(document adapter.Document) (*adapter.Node, error) {
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
	root := &adapter.Node{Value: map[string]any{}, Fields: map[string]*adapter.Node{}}
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

func addTOMLValue(data []byte, offset int, table *adapter.Node, keys []string, valueNode *unstable.Node) error {
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
	if _, duplicate := parent.Fields[key]; duplicate {
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
	if table.Capability == adapter.CapabilityReadOnly {
		value.MarkReadOnly(table.Reason)
	}
	parent.Fields[key] = value
	parent.Value.(map[string]any)[key] = value.Value
	return nil
}

func tomlTable(root *adapter.Node, keys []string, array bool) (*adapter.Node, error) {
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
	collection := parent.Fields[key]
	if collection == nil {
		collection = &adapter.Node{
			Value: []any{}, Capability: adapter.CapabilityReadOnly,
			Reason: "TOML arrays of tables are read-only",
		}
		parent.Fields[key] = collection
		parent.Value.(map[string]any)[key] = collection.Value
	}
	if collection.Items == nil && len(collection.Value.([]any)) != 0 {
		return nil, syncdomain.NewProblem(
			syncdomain.ErrorDuplicateKey,
			fmt.Sprintf("TOML key %q has incompatible definitions", strings.Join(keys, ".")),
			"Keep one table definition.",
		)
	}
	item := &adapter.Node{
		Value: map[string]any{}, Fields: map[string]*adapter.Node{},
		Capability: adapter.CapabilityReadOnly, Reason: "TOML arrays of tables are read-only",
	}
	collection.Items = append(collection.Items, item)
	collection.Value = append(collection.Value.([]any), item.Value)
	parent.Value.(map[string]any)[key] = collection.Value
	return item, nil
}

func tomlObjectPath(root *adapter.Node, keys []string) (*adapter.Node, error) {
	current := root
	for _, key := range keys {
		child := current.Fields[key]
		if child == nil {
			child = &adapter.Node{Value: map[string]any{}, Fields: map[string]*adapter.Node{}}
			current.Fields[key] = child
			current.Value.(map[string]any)[key] = child.Value
		}
		if child.Fields == nil {
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

func tomlValueNode(data []byte, offset int, node *unstable.Node) (*adapter.Node, error) {
	start := offset + int(node.Raw.Offset)
	end := start + int(node.Raw.Length)
	raw := data[node.Raw.Offset : node.Raw.Offset+node.Raw.Length]
	switch node.Kind {
	case unstable.String:
		return adapter.String(string(node.Data), start, end, func(value string) ([]byte, error) {
			return encodeTOMLString(value, string(raw))
		}), nil
	case unstable.Bool:
		return adapter.Bool(string(node.Data) == "true", start, end, nil), nil
	case unstable.Integer, unstable.Float:
		var decoded map[string]any
		if err := toml.Unmarshal(append([]byte("value = "), raw...), &decoded); err != nil {
			return nil, err
		}
		number, ok := adapter.AsNumber(decoded["value"])
		if !ok {
			return adapter.ReadOnly(start, end, "TOML number is not representable"), nil
		}
		return adapter.Number(number, start, end, func(next float64) ([]byte, error) {
			return []byte(strconv.FormatFloat(next, 'g', -1, 64)), nil
		}), nil
	case unstable.Array:
		return tomlArrayNode(data, offset, node)
	case unstable.InlineTable:
		return tomlInlineTableNode(data, offset, node)
	default:
		result := adapter.ReadOnly(start, end, "TOML date and time values are read-only")
		result.Value = string(node.Data)
		return result, nil
	}
}

func tomlArrayNode(data []byte, offset int, node *unstable.Node) (*adapter.Node, error) {
	var items []*adapter.Node
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
		items, values = append(items, item), append(values, item.Value)
	}
	return &adapter.Node{
		Value: values, Items: items, Collection: true, Capability: adapter.CapabilityReadOnly,
		Reason: "TOML structural changes are read-only",
	}, nil
}

func tomlInlineTableNode(data []byte, offset int, node *unstable.Node) (*adapter.Node, error) {
	result := &adapter.Node{
		Value: map[string]any{}, Fields: map[string]*adapter.Node{},
		Capability: adapter.CapabilityDirect,
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
	result.Capability = adapter.CapabilityReadOnly
	result.Reason = "TOML structural changes are read-only"
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
