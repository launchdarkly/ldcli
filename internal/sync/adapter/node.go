package adapter

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
)

// Node is one logical value that an adapter exposes to select and map paths.
//
// A leaf has a Value, the byte span that holds it, and an Encode function that
// writes a new value in the syntax of the source. An object has Fields. A
// collection has Items. A node whose Value is nil is unknown: the adapter saw
// it but cannot read it statically. A read-only node has a Reason, and an
// optional Code, that explain why sync cannot write it.
type Node struct {
	Value      any
	Fields     map[string]*Node
	Items      []*Node
	Collection bool
	Start      int
	End        int
	File       string
	Capability Capability
	Reason     string
	Code       syncdomain.ErrorCode
	Encode     func(any) ([]byte, error)
}

// PositionalIDReason marks the id of a resource that has no name of its own,
// such as the second unnamed agent of a file. The id is the position of the
// resource, so a selection by it is as fragile as an index.
const PositionalIDReason = "the id is the position of a resource without a name"

// MaxExactInteger is the largest integer that a float64 holds exactly. A
// number value is a float64, so a larger integer in a source stays read-only
// text instead of a rounded number.
const MaxExactInteger = int64(1) << 53

// ExactInteger reports whether a float64 holds an integer exactly.
func ExactInteger(integer int64) bool {
	return integer >= -MaxExactInteger && integer <= MaxExactInteger
}

// PositionalID returns an id node that marks a positional id.
func PositionalID(id string) *Node {
	return &Node{Value: id, Capability: CapabilityReadOnly, Reason: PositionalIDReason}
}

// IsPositionalID reports whether a resource has a positional id.
func IsPositionalID(resource *Node) bool {
	if resource == nil || resource.Fields == nil {
		return false
	}
	id := resource.Fields["id"]
	if id == nil {
		return false
	}
	return id.Reason == PositionalIDReason
}

// String returns a writable string leaf. encode writes a new string in the
// syntax of the source.
func String(value string, start, end int, encode func(string) ([]byte, error)) *Node {
	return &Node{
		Value: value, Start: start, End: end, Capability: CapabilityDirect,
		Encode: func(value any) ([]byte, error) {
			text, ok := value.(string)
			if !ok {
				return nil, TypeMismatch("source", "a string")
			}
			return encode(text)
		},
	}
}

// Number returns a writable number leaf.
func Number(value float64, start, end int, encode func(float64) ([]byte, error)) *Node {
	return &Node{
		Value: value, Start: start, End: end, Capability: CapabilityDirect,
		Encode: func(value any) ([]byte, error) {
			number, ok := AsNumber(value)
			if !ok {
				return nil, TypeMismatch("source", "a number")
			}
			return encode(number)
		},
	}
}

// Bool returns a writable boolean leaf. encode writes a new boolean in the
// syntax of the source. The default writes true or false.
func Bool(value bool, start, end int, encode func(bool) ([]byte, error)) *Node {
	if encode == nil {
		encode = func(value bool) ([]byte, error) { return []byte(strconv.FormatBool(value)), nil }
	}
	return &Node{
		Value: value, Start: start, End: end, Capability: CapabilityDirect,
		Encode: func(value any) ([]byte, error) {
			boolean, ok := value.(bool)
			if !ok {
				return nil, TypeMismatch("source", "a boolean")
			}
			return encode(boolean)
		},
	}
}

// ReadOnly returns a node that sync can see but cannot write.
func ReadOnly(start, end int, reason string) *Node {
	return &Node{Start: start, End: end, Capability: CapabilityReadOnly, Reason: reason}
}

// Collections returns the root of a code source: one named collection of
// resources for each logical kind, such as agents, prompts, tools, and
// constants.
func Collections(collections map[string][]*Node) *Node {
	fields := make(map[string]*Node, len(collections))
	for name, items := range collections {
		fields[name] = &Node{Items: items, Collection: true}
	}
	return &Node{Fields: fields}
}

// Resource returns one logical resource with its id field set, so that a
// keyed selector such as agents[id="support"] can find it. A resource whose
// definition is not a literal object has only the id.
func Resource(id string, fields map[string]*Node) *Node {
	if fields == nil {
		fields = make(map[string]*Node, 1)
	}
	fields["id"] = &Node{Value: id}
	return &Node{Fields: fields}
}

// Clone returns a deep copy of the node tree, so that a change to one use of
// a constant does not change another. The encoders are shared.
func (node *Node) Clone() *Node {
	if node == nil {
		return nil
	}
	copied := *node
	if node.Fields != nil {
		copied.Fields = make(map[string]*Node, len(node.Fields))
		for key, child := range node.Fields {
			copied.Fields[key] = child.Clone()
		}
	}
	if node.Items != nil {
		copied.Items = make([]*Node, len(node.Items))
		for index, child := range node.Items {
			copied.Items[index] = child.Clone()
		}
	}
	return &copied
}

// IsCollection reports whether a path can index the node.
func (node *Node) IsCollection() bool {
	if node == nil {
		return false
	}
	if node.Collection || node.Items != nil {
		return true
	}
	value := reflect.ValueOf(node.Value)
	return value.IsValid() && (value.Kind() == reflect.Array || value.Kind() == reflect.Slice)
}

// MarkReadOnly makes the node and every node below it read-only.
func (node *Node) MarkReadOnly(reason string) {
	if node == nil {
		return
	}
	node.Capability, node.Reason, node.Encode = CapabilityReadOnly, reason, nil
	for _, child := range node.Fields {
		child.MarkReadOnly(reason)
	}
	for _, child := range node.Items {
		child.MarkReadOnly(reason)
	}
}

// SetFile records the file that holds the node and every node below it that
// has no file yet. An imported value keeps the file that it came from.
func (node *Node) SetFile(file string) {
	node.setFile(file, map[*Node]struct{}{})
}

func (node *Node) setFile(file string, visited map[*Node]struct{}) {
	if node == nil {
		return
	}
	if _, seen := visited[node]; seen {
		return
	}
	visited[node] = struct{}{}
	if node.File == "" {
		node.File = file
	}
	for _, child := range node.Fields {
		child.setFile(file, visited)
	}
	for _, child := range node.Items {
		child.setFile(file, visited)
	}
}

// AsNumber returns the float64 value of a number from any parser.
func AsNumber(value any) (float64, bool) {
	switch value := value.(type) {
	case float64:
		return value, true
	case int:
		return float64(value), true
	case int64:
		return float64(value), true
	default:
		return 0, false
	}
}

// TypeMismatch is the problem of a mapped field whose value has the wrong type.
func TypeMismatch(field, expected string) error {
	return syncdomain.NewProblem(
		syncdomain.ErrorMapTypeMismatch,
		fmt.Sprintf("mapped field %q needs %s", field, expected),
		"Map the field to a compatible static value.",
	)
}

// JSONEncoder encodes a value as JSON in the layout of the raw text that it
// replaces. A value that spanned lines keeps its indentation unit and the
// indentation of its closing line. A value on one line stays on one line.
func JSONEncoder(raw []byte) func(any) ([]byte, error) {
	return func(next any) ([]byte, error) {
		unit, prefix, multiline := jsonIndentation(raw)
		if !multiline {
			return EncodeJSON(next, "", "")
		}
		return EncodeJSON(next, prefix, unit)
	}
}

// EncodeJSON writes a value as JSON without the HTML escapes of the standard
// encoder, so that <, >, and & in prompt text stay as they are. A non-empty
// indent writes the value over several lines.
func EncodeJSON(value any, prefix, indent string) ([]byte, error) {
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent(prefix, indent)
	if err := encoder.Encode(value); err != nil {
		return nil, err
	}
	return bytes.TrimRight(buffer.Bytes(), "\n"), nil
}

// jsonIndentation returns the indentation unit and the indentation of the
// closing line of a multi-line JSON value.
func jsonIndentation(raw []byte) (unit, prefix string, multiline bool) {
	lines := bytes.Split(raw, []byte("\n"))
	if len(lines) < 2 {
		return "", "", false
	}
	last := lines[len(lines)-1]
	prefix = string(last[:len(last)-len(bytes.TrimLeft(last, " \t"))])
	for _, line := range lines[1 : len(lines)-1] {
		indent := line[:len(line)-len(bytes.TrimLeft(line, " \t"))]
		if len(indent) > len(prefix) {
			return string(indent[len(prefix):]), prefix, true
		}
	}
	return "  ", prefix, true
}
