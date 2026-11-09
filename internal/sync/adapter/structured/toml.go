package structured

import (
	"bytes"
	"fmt"
	"math"
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
	root := newTOMLTable(nil)
	table := root
	tree := newTOMLTree()
	for parser.NextExpression() {
		expression := parser.Expression()
		switch expression.Kind {
		case unstable.KeyValue:
			keys := tomlKeys(expression)
			if err := addTOMLValue(data, offset, table, keys, expression.Value(), tree); err != nil {
				return nil, nameDocument(err, document.Path)
			}
		case unstable.Table:
			var err error
			table, err = tree.table(root, tomlKeys(expression))
			if err != nil {
				return nil, nameDocument(err, document.Path)
			}
		case unstable.ArrayTable:
			var err error
			table, err = tree.arrayTable(root, tomlKeys(expression))
			if err != nil {
				return nil, nameDocument(err, document.Path)
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

// tomlTree records how each table of a document was defined, because TOML
// limits what can extend it. A [header] defines a table once, and a later
// header can add a sub-table to it. A dotted key cannot add to a table that a
// header defined. Nothing can add to an inline table.
type tomlTree struct {
	// arrayTables are the arrays that [[header]] lines build.
	arrayTables map[*adapter.Node]bool
	// headers are the tables that a [header] or a [[header]] item defined.
	headers map[*adapter.Node]bool
	// implicit are the tables that a header path created on its way, as
	// [a.b.c] creates a and a.b. A later header can define one of them once,
	// and a dotted key cannot extend it.
	implicit map[*adapter.Node]bool
	// dotted are the tables that a dotted key created.
	dotted map[*adapter.Node]bool
	// inline are the inline tables.
	inline map[*adapter.Node]bool
}

func newTOMLTree() *tomlTree {
	return &tomlTree{
		arrayTables: map[*adapter.Node]bool{}, headers: map[*adapter.Node]bool{},
		dotted: map[*adapter.Node]bool{}, inline: map[*adapter.Node]bool{}, implicit: map[*adapter.Node]bool{},
	}
}

// addTOMLValue adds one key and value to a table. An inline table has a tree
// of its own, because no header can open it again and its own keys follow
// the same extension rules.
func addTOMLValue(
	data []byte,
	offset int,
	table *adapter.Node,
	keys []string,
	valueNode *unstable.Node,
	tree *tomlTree,
) error {
	if len(keys) == 0 {
		return syncdomain.NewProblem(
			syncdomain.ErrorInvalidSource, "TOML key is missing", "Add a key to the value.",
		)
	}
	parent, err := tree.objectPath(table, keys[:len(keys)-1], true)
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
	if tomlFrozen(table) {
		value.MarkReadOnly(table.Reason)
	}
	if tree != nil && valueNode.Kind == unstable.InlineTable {
		tree.inline[value] = true
	}
	parent.Fields[key] = value
	parent.Value.(map[string]any)[key] = value.Value
	return nil
}

const (
	tomlArrayTableReason = "TOML arrays of tables are read-only"
	tomlStructureReason  = "TOML structural changes are read-only"
)

// tomlFrozen reports whether a table and every value in it are read-only,
// as an item of an array of tables is. A table that is only structurally
// read-only keeps its scalar values writable.
func tomlFrozen(table *adapter.Node) bool {
	return table.Capability == adapter.CapabilityReadOnly && table.Reason == tomlArrayTableReason
}

// table returns the table that a [table] header opens. A header can define a
// table once, and cannot open an array of tables, an inline table, or a
// table that a dotted key defined.
func (tree *tomlTree) table(root *adapter.Node, keys []string) (*adapter.Node, error) {
	if len(keys) == 0 {
		return nil, syncdomain.NewProblem(
			syncdomain.ErrorInvalidSource, "TOML table key is missing", "Add a table key.",
		)
	}
	parent, err := tree.objectPath(root, keys[:len(keys)-1], false)
	if err != nil {
		return nil, err
	}
	key := keys[len(keys)-1]
	table := parent.Fields[key]
	switch {
	case table == nil:
		table = newTOMLTable(parent)
		parent.Fields[key] = table
		parent.Value.(map[string]any)[key] = table.Value
	case table.Fields == nil || tree.arrayTables[table] || tree.headers[table] || tree.dotted[table] || tree.inline[table]:
		return nil, syncdomain.NewProblem(
			syncdomain.ErrorDuplicateKey,
			fmt.Sprintf("TOML table %q is defined more than once", strings.Join(keys, ".")),
			"Keep one table definition.",
		)
	}
	tree.headers[table] = true
	delete(tree.implicit, table)
	return table, nil
}

// arrayTable returns the new item that a [[table]] header appends to its
// array of tables. A key that already holds a value, a table, or a plain
// array cannot become an array of tables.
func (tree *tomlTree) arrayTable(root *adapter.Node, keys []string) (*adapter.Node, error) {
	if len(keys) == 0 {
		return nil, syncdomain.NewProblem(
			syncdomain.ErrorInvalidSource, "TOML array table key is missing", "Add a table key.",
		)
	}
	parent, err := tree.objectPath(root, keys[:len(keys)-1], false)
	if err != nil {
		return nil, err
	}
	key := keys[len(keys)-1]
	collection := parent.Fields[key]
	if collection == nil {
		collection = &adapter.Node{
			Value: []any{}, Collection: true, Capability: adapter.CapabilityReadOnly,
			Reason: tomlArrayTableReason,
		}
		tree.arrayTables[collection] = true
		parent.Fields[key] = collection
		parent.Value.(map[string]any)[key] = collection.Value
	}
	if !tree.arrayTables[collection] {
		return nil, syncdomain.NewProblem(
			syncdomain.ErrorDuplicateKey,
			fmt.Sprintf("TOML key %q has incompatible definitions", strings.Join(keys, ".")),
			"Keep one table definition.",
		)
	}
	item := &adapter.Node{
		Value: map[string]any{}, Fields: map[string]*adapter.Node{},
		Capability: adapter.CapabilityReadOnly, Reason: tomlArrayTableReason,
	}
	tree.headers[item] = true
	collection.Items = append(collection.Items, item)
	collection.Value = append(collection.Value.([]any), item.Value)
	parent.Value.(map[string]any)[key] = collection.Value
	return item, nil
}

// newTOMLTable returns an empty table under parent, or the root table when
// parent is nil. Sync edits the values of a table and never rewrites the
// table as a whole, so the table is structurally read-only. A table inside
// an item of an array of tables is read-only with its values, as the item is.
func newTOMLTable(parent *adapter.Node) *adapter.Node {
	table := &adapter.Node{
		Value: map[string]any{}, Fields: map[string]*adapter.Node{},
		Capability: adapter.CapabilityReadOnly, Reason: tomlStructureReason,
	}
	if parent != nil && tomlFrozen(parent) {
		table.Reason = parent.Reason
	}
	return table
}

// objectPath returns the table at a dotted key path and creates the tables
// that do not exist yet. A header path through an array of tables continues
// in its last item, as [servers.limits] after [[servers]] does. A dotted key
// cannot extend a table that a header defined or an array of tables, and no
// path can extend an inline table.
func (tree *tomlTree) objectPath(root *adapter.Node, keys []string, dotted bool) (*adapter.Node, error) {
	current := root
	for index, key := range keys {
		child := current.Fields[key]
		if child == nil {
			child = newTOMLTable(current)
			current.Fields[key] = child
			current.Value.(map[string]any)[key] = child.Value
			if tree != nil {
				if dotted {
					tree.dotted[child] = true
				} else {
					tree.implicit[child] = true
				}
			}
		}
		if tree != nil && (tree.inline[child] ||
			dotted && (tree.headers[child] || tree.arrayTables[child] || tree.implicit[child])) {
			return nil, syncdomain.NewProblem(
				syncdomain.ErrorDuplicateKey,
				fmt.Sprintf("TOML table %q cannot be extended here", strings.Join(keys[:index+1], ".")),
				"Define the keys where the table is defined.",
			)
		}
		if tree != nil && tree.arrayTables[child] && len(child.Items) != 0 {
			child = child.Items[len(child.Items)-1]
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
		// An integer beyond int64 fails to decode, so it stays read-only
		// text, as an integer beyond 53 bits does, and the rest of the
		// document stays usable.
		if node.Kind == unstable.Integer {
			if _, err := strconv.ParseInt(strings.ReplaceAll(string(raw), "_", ""), 0, 64); err != nil {
				result := adapter.ReadOnly(start, end, "TOML integer is too large to represent exactly")
				result.Value = string(raw)
				return result, nil
			}
		}
		var decoded map[string]any
		if err := toml.Unmarshal(append([]byte("value = "), raw...), &decoded); err != nil {
			return nil, err
		}
		// An integer beyond 53 bits has no exact float64, so it stays
		// read-only text instead of a rounded number.
		if integer, isInteger := decoded["value"].(int64); isInteger && !adapter.ExactInteger(integer) {
			result := adapter.ReadOnly(start, end, "TOML integer is too large to represent exactly")
			result.Value = string(raw)
			return result, nil
		}
		number, ok := adapter.AsNumber(decoded["value"])
		if !ok {
			return adapter.ReadOnly(start, end, "TOML number is not representable"), nil
		}
		// Infinity and NaN have no JSON form, so they stay read-only text.
		if math.IsInf(number, 0) || math.IsNaN(number) {
			result := adapter.ReadOnly(start, end, "TOML infinity and NaN values are read-only")
			result.Value = string(raw)
			return result, nil
		}
		return adapter.Number(number, start, end, tomlNumberEncoder(node.Kind == unstable.Integer)), nil
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
	items, values := []*adapter.Node{}, []any{}
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
		Reason: tomlStructureReason,
	}, nil
}

func tomlInlineTableNode(data []byte, offset int, node *unstable.Node) (*adapter.Node, error) {
	result := newTOMLTable(nil)
	tree := newTOMLTree()
	iterator := node.Children()
	for iterator.Next() {
		child := iterator.Node()
		if child.Kind != unstable.KeyValue {
			continue
		}
		if err := addTOMLValue(data, offset, result, tomlKeys(child), child.Value(), tree); err != nil {
			return nil, err
		}
	}
	return result, nil
}

// tomlNumberEncoder writes a number in the type of the value that it
// replaces. TOML tells an integer from a float, so an integer slot takes a
// whole number only and a float slot keeps a fraction or a trailing .0.
func tomlNumberEncoder(integer bool) func(float64) ([]byte, error) {
	return func(next float64) ([]byte, error) {
		if integer {
			if next != math.Trunc(next) || next > float64(adapter.MaxExactInteger) || next < -float64(adapter.MaxExactInteger) {
				return nil, fmt.Errorf("TOML integer cannot hold %v", next)
			}
			return []byte(strconv.FormatInt(int64(next), 10)), nil
		}
		text := strconv.FormatFloat(next, 'g', -1, 64)
		if !strings.ContainsAny(text, ".eE") {
			text += ".0"
		}
		return []byte(text), nil
	}
}

// encodeTOMLString writes a value in the string form of the text that it
// replaces. A literal string has no escapes, so it cannot hold every value.
func encodeTOMLString(value, previous string) ([]byte, error) {
	switch {
	case strings.HasPrefix(previous, `"""`):
		return []byte(tomlMultilineOpening(`"""`, previous, value) + encodeTOMLBasicBody(value, true) + `"""`), nil
	case strings.HasPrefix(previous, `'''`):
		if strings.Contains(value, `'''`) || !tomlLiteralCanHold(value, true) {
			return nil, fmt.Errorf("multiline literal string cannot represent the new value")
		}
		return []byte(tomlMultilineOpening(`'''`, previous, value) + value + `'''`), nil
	case strings.HasPrefix(previous, `'`):
		if strings.Contains(value, `'`) || !tomlLiteralCanHold(value, false) {
			return nil, fmt.Errorf("literal string cannot represent the new value")
		}
		return []byte("'" + value + "'"), nil
	default:
		return []byte(`"` + encodeTOMLBasicBody(value, false) + `"`), nil
	}
}

// tomlMultilineOpening returns the opening delimiter of a multiline string.
// TOML trims a newline right after the delimiter, so a value that starts with
// a newline needs one more. A source that had one keeps its layout.
func tomlMultilineOpening(delimiter, previous, value string) string {
	rest := strings.TrimPrefix(previous, delimiter)
	if strings.HasPrefix(value, "\n") || strings.HasPrefix(value, "\r\n") ||
		strings.HasPrefix(rest, "\n") || strings.HasPrefix(rest, "\r\n") {
		return delimiter + "\n"
	}
	return delimiter
}

// tomlLiteralCanHold reports whether a literal string can hold a value. A
// literal string allows a tab but no other control character. A multiline
// literal string also allows a newline.
func tomlLiteralCanHold(value string, multiline bool) bool {
	for index := 0; index < len(value); index++ {
		switch character := value[index]; {
		case character == '\t' || character >= 0x20 && character != 0x7f:
		case multiline && character == '\n':
		case multiline && character == '\r' && index+1 < len(value) && value[index+1] == '\n':
		default:
			return false
		}
	}
	return true
}

// encodeTOMLBasicBody escapes a value for a basic string. A multiline basic
// string keeps a newline and a tab as they are.
func encodeTOMLBasicBody(value string, multiline bool) string {
	var encoded strings.Builder
	for _, character := range value {
		switch {
		case character == '"' || character == '\\':
			encoded.WriteByte('\\')
			encoded.WriteRune(character)
		case multiline && (character == '\n' || character == '\t'):
			encoded.WriteRune(character)
		case character == '\b':
			encoded.WriteString(`\b`)
		case character == '\t':
			encoded.WriteString(`\t`)
		case character == '\n':
			encoded.WriteString(`\n`)
		case character == '\f':
			encoded.WriteString(`\f`)
		case character == '\r':
			encoded.WriteString(`\r`)
		case character < 0x20 || character >= 0x7f && character <= 0x9f:
			fmt.Fprintf(&encoded, `\u%04X`, character)
		default:
			encoded.WriteRune(character)
		}
	}
	return encoded.String()
}
