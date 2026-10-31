package reference

import (
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	sitter "github.com/tree-sitter/go-tree-sitter"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
)

const maxSourceBytes = 4 << 20

func parseTree(document Document, language *sitter.Language) (*sitter.Tree, error) {
	if !utf8.Valid(document.Data) {
		return nil, fmt.Errorf("%s: source is not valid UTF-8", document.Path)
	}
	if len(document.Data) > maxSourceBytes {
		return nil, fmt.Errorf("%s: source exceeds %d bytes", document.Path, maxSourceBytes)
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

func logicalRoot(collections map[string][]*sourceNode) *sourceNode {
	fields := make(map[string]*sourceNode, len(collections))
	for name, items := range collections {
		fields[name] = &sourceNode{items: items, collection: true}
	}
	return &sourceNode{fields: fields}
}

func logicalResource(id string, fields map[string]*sourceNode) *sourceNode {
	fields["id"] = &sourceNode{value: id}
	return &sourceNode{fields: fields}
}

func walkTree(root *sitter.Node, visit func(*sitter.Node)) {
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

func namedChildren(node *sitter.Node) []*sitter.Node {
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

func nodeText(node *sitter.Node, source []byte) string {
	if node == nil {
		return ""
	}
	return string(source[node.StartByte():node.EndByte()])
}

func messageListFromCollection(collection *sourceNode) *sourceNode {
	if collection == nil || collection.items == nil {
		return nil
	}
	result := &sourceNode{
		items: collection.items, collection: true, start: collection.start, end: collection.end,
		capability: CapabilityDirect, reason: "message structure is read-only",
	}
	var messages []syncdomain.Message
	for _, item := range collection.items {
		fields := item.fields
		if fields == nil && len(item.items) == 2 {
			fields = map[string]*sourceNode{"role": item.items[0], "content": item.items[1]}
			item.fields = fields
		}
		role, roleOK := fields["role"].value.(string)
		content, contentOK := fields["content"].value.(string)
		if !roleOK || !contentOK {
			result.capability = CapabilityReadOnly
			result.reason = "dynamic message content is read-only"
			return result
		}
		messages = append(messages, syncdomain.Message{
			Role: normalizeMessageRole(role), Content: content,
		})
	}
	result.value = messages
	return result
}

func decodePythonString(raw []byte) (string, string, error) {
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

func encodePythonString(value, quote string) []byte {
	body := encodeEscapes(value, quote[0], len(quote) == 3)
	if len(quote) == 3 {
		body = strings.ReplaceAll(body, quote, `\`+quote)
	}
	return []byte(quote + body + quote)
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
