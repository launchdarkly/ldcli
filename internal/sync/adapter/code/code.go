// Package code holds the helpers that the tree-sitter adapters share: parsing
// with a time limit, string codecs, message lists, and placeholder
// translation. No format package imports another format package.
package code

import (
	"bytes"
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
		Items: collection.Items, Collection: true, Start: collection.Start, End: collection.End, File: collection.File,
		Capability: adapter.CapabilityDirect, Reason: "message structure is read-only",
	}
	// A read-only collection, such as a mutated import, keeps its reason,
	// so that no publish writes its pre-mutation messages.
	if collection.Capability == adapter.CapabilityReadOnly {
		result.Capability, result.Reason = adapter.CapabilityReadOnly, collection.Reason
		return result
	}
	var messages []syncdomain.Message
	for _, item := range collection.Items {
		fields := item.Fields
		if fields == nil && len(item.Items) == 2 {
			fields = map[string]*adapter.Node{"role": item.Items[0], "content": item.Items[1]}
			item.Fields = fields
		}
		role, roleOK := StringValue(fields["role"])
		content, contentOK := StringValue(fields["content"])
		// The role of a message is structure, and a constructor such as
		// HumanMessage gives it. The content must be a writable value.
		if !roleOK || !contentOK || !Writable(fields["content"]) {
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
	return NormalizeLineEndings(value), quote, err
}

// NormalizeLineEndings gives a string literal one line ending, so a prompt
// in a file with Windows or classic Mac line endings reads as the same text
// everywhere, as the prompt normalization of a variation does.
func NormalizeLineEndings(value string) string {
	return strings.ReplaceAll(strings.ReplaceAll(value, "\r\n", "\n"), "\r", "\n")
}

// MatchLineEndings writes an encoded literal with the line endings of the
// literal that it replaces, so a file keeps one line ending throughout.
func MatchLineEndings(encoded, previous []byte) []byte {
	if !bytes.Contains(previous, []byte("\r\n")) {
		return encoded
	}
	normalized := bytes.ReplaceAll(encoded, []byte("\r\n"), []byte("\n"))
	return bytes.ReplaceAll(normalized, []byte("\n"), []byte("\r\n"))
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
	return NormalizeLineEndings(value), quote, err
}

// EncodeJSString writes a value as a JavaScript string with the given quote.
// A template literal holds its line breaks as they are, so a multi-line
// prompt keeps its layout.
func EncodeJSString(value string, quote byte) ([]byte, error) {
	if quote == '`' && strings.Contains(value, "${") {
		return nil, fmt.Errorf("template interpolation is read-only")
	}
	return []byte(string(quote) + encodeEscapes(value, quote, quote == '`') + string(quote)), nil
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
			// A backslash before a line break joins the lines.
		case '\r':
			// A Windows line break is two bytes, and the continuation
			// takes both.
			if offset < len(text) && text[offset] == '\n' {
				offset++
			}
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
		case 'a':
			if !python {
				return "", fmt.Errorf("unsupported TypeScript escape \\a")
			}
			result.WriteByte('\a')
		case 'N':
			if !python {
				return "", fmt.Errorf("unsupported TypeScript escape \\N")
			}
			// A named character needs the Unicode name table, which the
			// adapter does not carry, so the string is read-only.
			return "", fmt.Errorf("Python escape \\N{...} is not supported")
		case '0', '1', '2', '3', '4', '5', '6', '7':
			if !python {
				return "", fmt.Errorf("unsupported TypeScript escape \\%c", escape)
			}
			// An octal escape has one to three digits.
			digits := string(escape)
			for len(digits) < 3 && offset < len(text) && text[offset] >= '0' && text[offset] <= '7' {
				digits += string(text[offset])
				offset++
			}
			value, _ := strconv.ParseUint(digits, 8, 32)
			if value > 0o377 {
				return "", fmt.Errorf("Python octal escape \\%s is out of range", digits)
			}
			result.WriteRune(rune(value))
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
		items, err := pythonItems(typed)
		if err != nil {
			return nil, err
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

// AmbiguousImport returns the read-only value of a name that two imports of
// one scope bind, as in a try/except fallback, because no one module is its
// value. file is the file of the first import, so that the name still counts
// as an import.
func AmbiguousImport(name, file string) *adapter.Node {
	node := adapter.ReadOnly(0, 0, fmt.Sprintf("%s is imported more than once, so its value is not static", name))
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
	// The id of the resource is the variable name. An object entry named id
	// is not reachable through the resource, and a map of the id says so
	// instead of reading the variable name as a value.
	fields["id"] = &adapter.Node{
		Value: id, Capability: adapter.CapabilityReadOnly,
		Reason: "the id of a constant is its variable name, not a value of the object",
	}
	return &adapter.Node{
		Value: value.Value, Fields: fields, Items: value.Items, Collection: value.Collection,
		Start: value.Start, End: value.End, File: value.File, Capability: value.Capability,
		Reason: value.Reason, Code: value.Code, Encode: value.Encode,
	}
}

// Receiver finds the instance that a dotted callable is called on, and the
// method after it. A receiver can be a name or an attribute chain, such as
// self.client or this.client, so the longest prefix that names an instance
// wins. kindOf returns the kind of a receiver, or an empty string.
func Receiver(callable string, kindOf func(string) string) (kind, method string) {
	for index := strings.LastIndex(callable, "."); index > 0; index = strings.LastIndex(callable[:index], ".") {
		if kind := kindOf(callable[:index]); kind != "" {
			return kind, callable[index+1:]
		}
	}
	return "", ""
}

// SplitMethod splits a callable at its last dot, so that the owner of
// p.ChatPromptTemplate.fromMessages is p.ChatPromptTemplate. A namespace
// import names a class through the namespace, so the owner can have dots.
func SplitMethod(callable string) (owner, method string, qualified bool) {
	index := strings.LastIndex(callable, ".")
	if index < 0 {
		return callable, "", false
	}
	return callable[:index], callable[index+1:], true
}

// PythonTuple writes a list of values as a Python tuple, so that a tuple
// constant stays a tuple on a write. A tuple of one item keeps its comma.
func PythonTuple(value any) ([]byte, error) {
	typed, ok := value.([]any)
	if !ok {
		return nil, adapter.TypeMismatch("source", "a list")
	}
	items, err := pythonItems(typed)
	if err != nil {
		return nil, err
	}
	if len(items) == 1 {
		return []byte("(" + items[0] + ",)"), nil
	}
	return []byte("(" + strings.Join(items, ", ") + ")"), nil
}

func pythonItems(values []any) ([]string, error) {
	items := make([]string, len(values))
	for index, item := range values {
		encoded, err := PythonLiteral(item)
		if err != nil {
			return nil, err
		}
		items[index] = string(encoded)
	}
	return items, nil
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

// FallbackIDs gives resources that have no name of their own distinct ids.
// The first resource that falls back to an id keeps it, and each later one
// gets an ordinal suffix, so that a keyed selector matches one resource.
type FallbackIDs struct {
	seen map[string]int
}

// NewFallbackIDs returns an empty allocator.
func NewFallbackIDs() *FallbackIDs { return &FallbackIDs{seen: map[string]int{}} }

// Distinguish marks the id of a resource that fell back to its position as
// positional, and renames it when an earlier resource already used it.
func (ids *FallbackIDs) Distinguish(resource *adapter.Node, fallback string) {
	if id, _ := StringValue(resource.Fields["id"]); id != fallback {
		return
	}
	ids.seen[fallback]++
	id := fallback
	if count := ids.seen[fallback]; count > 1 {
		id = fmt.Sprintf("%s#%d", fallback, count)
	}
	resource.Fields["id"] = adapter.PositionalID(id)
}

// DistinguishDuplicates gives each of the resources that share an id an
// ordinal, in order, as a positional id, so that no two resources of one
// collection share one id and a selector names one of them.
func DistinguishDuplicates(resources []*adapter.Node) {
	counts := map[string]int{}
	for _, resource := range resources {
		if id, ok := StringValue(resource.Fields["id"]); ok {
			counts[id]++
		}
	}
	ordinals := map[string]int{}
	for _, resource := range resources {
		id, ok := StringValue(resource.Fields["id"])
		if !ok || counts[id] < 2 {
			continue
		}
		ordinals[id]++
		resource.Fields["id"] = adapter.PositionalID(fmt.Sprintf("%s#%d", id, ordinals[id]))
	}
}

// SameNode reports whether two handles name the same syntax node. Tree-sitter
// returns a new handle on each access, so the handles themselves differ.
func SameNode(left, right *sitter.Node) bool {
	return left != nil && right != nil && left.Kind() == right.Kind() &&
		left.StartByte() == right.StartByte() && left.EndByte() == right.EndByte()
}

// StringValue returns the string value of a node. A nil node has none.
func StringValue(node *adapter.Node) (string, bool) {
	if node == nil {
		return "", false
	}
	text, ok := node.Value.(string)
	return text, ok
}

// HasValue reports whether a node has a static value. A null leaf has a nil
// Value and is still static, so a collection that holds one stays writable.
func HasValue(node *adapter.Node) bool {
	return node != nil && (node.Value != nil || node.Capability == adapter.CapabilityDirect)
}

// Writable reports whether a node has a static value that a collection can
// hold and write back: a direct leaf, a null leaf included, or a direct
// object or collection. A read-only node, such as a mutated import, makes the
// collection around it read-only.
func Writable(node *adapter.Node) bool {
	return node != nil && node.Capability == adapter.CapabilityDirect
}

// IsLiteral reports whether a node is a literal that a constant can hold: a
// static value, an object, or a collection.
func IsLiteral(node *adapter.Node) bool {
	return HasValue(node) || node != nil && (node.Fields != nil || node.Items != nil)
}

// ShadowKey returns the key under which a constants map keeps an imported
// value that a local declaration shadows. The space keeps it apart from
// every qualified id.
func ShadowKey(name string) string { return "import " + name }

// RecordShadowedImport keeps an imported value that a module-level
// declaration shadows, with the start of the first such declaration, so that
// a use before it still resolves the import.
func RecordShadowedImport(constants map[string]*adapter.Node, name string, value *adapter.Node, declarationStart int) {
	if value == nil {
		return
	}
	if _, recorded := constants[ShadowKey(name)]; recorded {
		return
	}
	constants[ShadowKey(name)] = &adapter.Node{
		Start: declarationStart, Fields: map[string]*adapter.Node{"import": value},
	}
}

// ShadowedImport returns a copy of the import that a use of a module-level
// name sees before the local declaration that shadows it, or nil.
func ShadowedImport(constants map[string]*adapter.Node, name string, use int) *adapter.Node {
	shadow := constants[ShadowKey(name)]
	if shadow == nil || use >= shadow.Start {
		return nil
	}
	return shadow.Fields["import"].Clone()
}

// ImportSiteKey is the key under which the constants of a file record where
// an import statement binds a name.
func ImportSiteKey(name string) string { return "import site " + name }

// RecordImportSite records the end of the import statement that binds a name
// in the primary file, so that a use above it in the same scope does not see
// the imported value. The first statement of a name wins.
func RecordImportSite(constants map[string]*adapter.Node, name string, end int) {
	if _, recorded := constants[ImportSiteKey(name)]; !recorded {
		constants[ImportSiteKey(name)] = &adapter.Node{End: end}
	}
}

// ImportBound reports whether an imported name is bound at a byte offset of
// the primary file. A name without a recorded site is bound everywhere.
func ImportBound(constants map[string]*adapter.Node, name string, use int) bool {
	site := constants[ImportSiteKey(name)]
	return site == nil || site.End <= use
}

// DeclarationKey is the key under which the constants of a file record where
// a declaration of the primary file ends.
func DeclarationKey(id string) string { return "declared " + id }

// RecordDeclaration records where a declaration of the primary file ends, so
// that a value copied from an import is in scope after the declaration only.
func RecordDeclaration(constants map[string]*adapter.Node, id string, end int) {
	constants[DeclarationKey(id)] = &adapter.Node{End: end}
}

// DeclaredAt reports whether a constant is in scope at a byte offset of the
// primary file. A declaration of the primary file must end before the use, a
// value copied from an import included. An import without a declaration here
// is in scope everywhere, and its statement position has its own check.
func DeclaredAt(constants map[string]*adapter.Node, id string, constant *adapter.Node, use int) bool {
	if constant == nil {
		return false
	}
	if declaration := constants[DeclarationKey(id)]; declaration != nil {
		return declaration.End < use
	}
	return Declared(constant, use)
}

// Declared reports whether a constant is in scope at a byte offset of the
// primary file. A constant from another file is in scope everywhere. A
// constant of the primary file must end before the use.
func Declared(constant *adapter.Node, use int) bool {
	return constant != nil && (constant.File != "" || constant.End < use)
}

// StaticString returns the string value of a node, or an empty string.
func StaticString(node *adapter.Node) string {
	if node == nil {
		return ""
	}
	value, _ := node.Value.(string)
	return value
}
