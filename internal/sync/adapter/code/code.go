// Package code holds the helpers that the tree-sitter adapters share: parsing
// with a time limit, string codecs, message lists, and placeholder
// translation. No format package imports another format package.
package code

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	sitter "github.com/tree-sitter/go-tree-sitter"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
	"github.com/launchdarkly/ldcli/internal/sync/adapter"
)

// MaxSourceBytes is the largest source that a code adapter parses.
const MaxSourceBytes = 4 << 20

// ParseTree parses a source with a time limit. It rejects a source that is
// not UTF-8, that is too large, or that has syntax errors.
func ParseTree(document adapter.Document, language *sitter.Language) (*sitter.Tree, error) {
	if !utf8.Valid(document.Data) {
		return nil, fmt.Errorf("%s: source is not valid UTF-8", document.Path)
	}
	if len(document.Data) > MaxSourceBytes {
		return nil, fmt.Errorf("%s: source exceeds %d bytes", document.Path, MaxSourceBytes)
	}
	parser := sitter.NewParser()
	defer parser.Close()
	if err := parser.SetLanguage(language); err != nil {
		return nil, fmt.Errorf("%s: set parser language: %w", document.Path, err)
	}
	deadline := time.Now().Add(2 * time.Second)
	tree := parser.ParseWithOptions(func(offset int, _ sitter.Point) []byte {
		if offset >= len(document.Data) {
			return nil
		}
		return document.Data[offset:]
	}, nil, &sitter.ParseOptions{
		ProgressCallback: func(sitter.ParseState) bool {
			return time.Now().After(deadline)
		},
	})
	if tree == nil {
		return nil, fmt.Errorf("%s: parser timed out", document.Path)
	}
	if tree.RootNode().HasError() {
		tree.Close()
		return nil, fmt.Errorf("%s: source contains syntax errors", document.Path)
	}
	return tree, nil
}

// Walk visits every named node of a tree, parents before children.
func Walk(root *sitter.Node, visit func(*sitter.Node)) {
	stack := []*sitter.Node{root}
	for len(stack) != 0 {
		node := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		visit(node)
		for index := node.NamedChildCount(); index > 0; index-- {
			stack = append(stack, node.NamedChild(index-1))
		}
	}
}

// NamedChildren returns the named children of a node without comments.
func NamedChildren(node *sitter.Node) []*sitter.Node {
	if node == nil {
		return nil
	}
	children := make([]*sitter.Node, 0, node.NamedChildCount())
	for index := uint(0); index < node.NamedChildCount(); index++ {
		child := node.NamedChild(index)
		if child.Kind() != "comment" {
			children = append(children, child)
		}
	}
	return children
}

// FindNode returns the first node of kind whose text is text, or nil.
func FindNode(root *sitter.Node, source []byte, kind, text string) *sitter.Node {
	var found *sitter.Node
	Walk(root, func(node *sitter.Node) {
		if found == nil && node.Kind() == kind && Text(node, source) == text {
			found = node
		}
	})
	return found
}

// Text returns the source text of a node.
func Text(node *sitter.Node, source []byte) string {
	if node == nil {
		return ""
	}
	return string(source[node.StartByte():node.EndByte()])
}

// MessageList reads a collection of role and content pairs as a message
// list. A pair can be a two-item list or an object. A message with a dynamic
// role or content makes the list read-only.
func MessageList(collection *adapter.Node) *adapter.Node {
	if collection == nil || collection.Items == nil {
		return nil
	}
	result := &adapter.Node{
		Items: collection.Items, Collection: true, Start: collection.Start, End: collection.End,
		Capability: adapter.CapabilityDirect, Reason: "message structure is read-only",
	}
	var messages []syncdomain.Message
	for _, item := range collection.Items {
		fields := item.Fields
		if fields == nil && len(item.Items) == 2 {
			fields = map[string]*adapter.Node{"role": item.Items[0], "content": item.Items[1]}
			item.Fields = fields
		}
		role, roleOK := fields["role"].Value.(string)
		content, contentOK := fields["content"].Value.(string)
		if !roleOK || !contentOK {
			result.Capability = adapter.CapabilityReadOnly
			result.Reason = "dynamic message content is read-only"
			return result
		}
		messages = append(messages, syncdomain.Message{
			Role: NormalizeRole(role), Content: content,
		})
	}
	result.Value = messages
	return result
}

// DecodePythonString decodes a Python string literal and returns its value
// and its quote. Formatted, byte, and raw strings are read-only.
func DecodePythonString(raw []byte) (string, string, error) {
	text := string(raw)
	quoteAt := strings.IndexAny(text, `"'`)
	if quoteAt < 0 {
		return "", "", fmt.Errorf("unsupported Python string")
	}
	prefix := strings.ToLower(text[:quoteAt])
	if strings.ContainsAny(prefix, "fbr") || strings.Trim(prefix, "u") != "" {
		return "", "", fmt.Errorf("Python string prefix %q is read-only", prefix)
	}
	quote := text[quoteAt : quoteAt+1]
	if strings.HasPrefix(text[quoteAt:], quote+quote+quote) {
		quote = quote + quote + quote
	}
	if len(text) < quoteAt+2*len(quote) || !strings.HasSuffix(text, quote) {
		return "", "", fmt.Errorf("invalid Python string")
	}
	value, err := decodeEscapes(text[quoteAt+len(quote):len(text)-len(quote)], true)
	return value, quote, err
}

// EncodePythonString writes a value as a Python string with the given quote.
func EncodePythonString(value, quote string) []byte {
	body := encodeEscapes(value, quote[0], len(quote) == 3)
	if len(quote) == 3 {
		body = strings.ReplaceAll(body, quote, `\`+quote)
	}
	return []byte(quote + body + quote)
}

// DecodeJSString decodes a JavaScript string literal and returns its value
// and its quote. A template with interpolation is read-only.
func DecodeJSString(raw []byte) (string, byte, error) {
	if len(raw) < 2 || raw[0] != raw[len(raw)-1] {
		return "", 0, fmt.Errorf("invalid TypeScript string")
	}
	quote := raw[0]
	if quote != '\'' && quote != '"' && quote != '`' {
		return "", 0, fmt.Errorf("unsupported TypeScript string")
	}
	body := string(raw[1 : len(raw)-1])
	if quote == '`' && strings.Contains(body, "${") {
		return "", 0, fmt.Errorf("template interpolation is read-only")
	}
	value, err := decodeEscapes(body, false)
	return value, quote, err
}

// EncodeJSString writes a value as a JavaScript string with the given quote.
func EncodeJSString(value string, quote byte) ([]byte, error) {
	if quote == '`' && strings.Contains(value, "${") {
		return nil, fmt.Errorf("template interpolation is read-only")
	}
	return []byte(string(quote) + encodeEscapes(value, quote, false) + string(quote)), nil
}

func decodeEscapes(text string, python bool) (string, error) {
	var result strings.Builder
	for offset := 0; offset < len(text); {
		if text[offset] != '\\' {
			r, width := utf8.DecodeRuneInString(text[offset:])
			result.WriteRune(r)
			offset += width
			continue
		}
		offset++
		if offset == len(text) {
			return "", fmt.Errorf("string ends with a backslash")
		}
		escape := text[offset]
		offset++
		switch escape {
		case '\\', '\'', '"', '`':
			result.WriteByte(escape)
		case 'n':
			result.WriteByte('\n')
		case 'r':
			result.WriteByte('\r')
		case 't':
			result.WriteByte('\t')
		case 'b':
			result.WriteByte('\b')
		case 'f':
			result.WriteByte('\f')
		case 'v':
			result.WriteByte('\v')
		case '\n':
		case 'x', 'u', 'U':
			digits := map[byte]int{'x': 2, 'u': 4, 'U': 8}[escape]
			if !python && escape == 'U' {
				return "", fmt.Errorf("TypeScript does not support \\U escapes")
			}
			if offset+digits > len(text) {
				return "", fmt.Errorf("short hexadecimal escape")
			}
			value, err := strconv.ParseUint(text[offset:offset+digits], 16, 32)
			if err != nil || value > utf8.MaxRune || value >= 0xd800 && value <= 0xdfff {
				return "", fmt.Errorf("invalid Unicode escape")
			}
			result.WriteRune(rune(value))
			offset += digits
		default:
			if !python {
				return "", fmt.Errorf("unsupported TypeScript escape \\%c", escape)
			}
			result.WriteByte('\\')
			result.WriteByte(escape)
		}
	}
	return result.String(), nil
}

func encodeEscapes(value string, quote byte, multiline bool) string {
	var result strings.Builder
	for _, character := range value {
		switch character {
		case '\\':
			result.WriteString(`\\`)
		case '\n':
			if multiline {
				result.WriteByte('\n')
			} else {
				result.WriteString(`\n`)
			}
		case '\r':
			result.WriteString(`\r`)
		case '\t':
			result.WriteString(`\t`)
		case rune(quote):
			result.WriteByte('\\')
			result.WriteRune(character)
		default:
			if character < 0x20 {
				fmt.Fprintf(&result, `\u%04x`, character)
			} else {
				result.WriteRune(character)
			}
		}
	}
	return result.String()
}

// PythonLiteral writes a JSON-compatible value as a Python literal.
// Strings use double quotes. Objects keep their key order sorted, as the
// JSON encoding does.
func PythonLiteral(value any) ([]byte, error) {
	switch typed := value.(type) {
	case nil:
		return []byte("None"), nil
	case bool:
		if typed {
			return []byte("True"), nil
		}
		return []byte("False"), nil
	case string:
		return EncodePythonString(typed, `"`), nil
	case float64, int, int64:
		return json.Marshal(typed)
	case []any:
		items := make([]string, len(typed))
		for index, item := range typed {
			encoded, err := PythonLiteral(item)
			if err != nil {
				return nil, err
			}
			items[index] = string(encoded)
		}
		return []byte("[" + strings.Join(items, ", ") + "]"), nil
	case map[string]any:
		pairs := make([]string, 0, len(typed))
		for _, key := range slices.Sorted(maps.Keys(typed)) {
			encoded, err := PythonLiteral(typed[key])
			if err != nil {
				return nil, err
			}
			pairs = append(pairs, string(EncodePythonString(key, `"`))+": "+string(encoded))
		}
		return []byte("{" + strings.Join(pairs, ", ") + "}"), nil
	default:
		return nil, fmt.Errorf("value of type %T has no Python literal", value)
	}
}

// UnresolvedImport is the read-only node of an imported value that static
// inspection cannot follow. file names the module that holds it, so that a
// hint can tell the user to link that file.
func UnresolvedImport(module, name, file string) *adapter.Node {
	node := adapter.ReadOnly(0, 0, fmt.Sprintf("imported value %s.%s cannot be resolved statically", module, name))
	node.Code = syncdomain.ErrorImportUnresolved
	node.File = file
	return node
}

// Constant returns a top-level literal as a logical resource with an id, so
// that constants[id="NAME"] can select it.
func Constant(id string, value *adapter.Node) *adapter.Node {
	fields := make(map[string]*adapter.Node, len(value.Fields)+1)
	for key, child := range value.Fields {
		fields[key] = child
	}
	fields["id"] = &adapter.Node{Value: id}
	return &adapter.Node{
		Value: value.Value, Fields: fields, Items: value.Items, Collection: value.Collection,
		Start: value.Start, End: value.End, File: value.File, Capability: value.Capability,
		Reason: value.Reason, Code: value.Code, Encode: value.Encode,
	}
}

// NormalizeRole maps framework role names to the LaunchDarkly roles.
func NormalizeRole(role string) string {
	switch role {
	case "human":
		return syncdomain.RoleUser
	case "ai":
		return syncdomain.RoleAssistant
	default:
		return role
	}
}

// StaticString returns the string value of a node, or an empty string.
func StaticString(node *adapter.Node) string {
	if node == nil {
		return ""
	}
	value, _ := node.Value.(string)
	return value
}
