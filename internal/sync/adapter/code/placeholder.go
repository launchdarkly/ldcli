package code

import (
	"fmt"
	"strings"
	"unicode"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
	"github.com/launchdarkly/ldcli/internal/sync/adapter"
)

// PlaceholderStyle is the placeholder syntax of a framework. LaunchDarkly
// uses {{name}}. Translation is exact or the value is read-only.
type PlaceholderStyle string

const (
	BracePlaceholders  PlaceholderStyle = "brace"
	DollarPlaceholders PlaceholderStyle = "dollar"
)

// TranslatePlaceholders returns a copy of a string node whose value uses
// LaunchDarkly placeholders and whose encoder writes them back in style.
func TranslatePlaceholders(node *adapter.Node, style PlaceholderStyle) *adapter.Node {
	if node == nil {
		return nil
	}
	value, ok := node.Value.(string)
	if !ok {
		return node
	}
	translated, err := placeholdersToLaunchDarkly(value, style)
	if err != nil {
		result := *node
		result.Value = nil
		result.Capability = adapter.CapabilityReadOnly
		result.Reason = err.Error()
		result.Code = syncdomain.ErrorPlaceholderAmbiguous
		result.Encode = nil
		return &result
	}
	result := *node
	result.Value = translated
	encode := node.Encode
	result.Encode = func(value any) ([]byte, error) {
		text, ok := value.(string)
		if !ok {
			return nil, adapter.TypeMismatch("source", "a string")
		}
		sourceText, err := placeholdersFromLaunchDarkly(text, style)
		if err != nil {
			return nil, err
		}
		return encode(sourceText)
	}
	return &result
}

func translateMessagePlaceholders(node *adapter.Node, style PlaceholderStyle) *adapter.Node {
	if node == nil || node.Items == nil {
		return node
	}
	result := *node
	result.Items = make([]*adapter.Node, len(node.Items))
	var messages []syncdomain.Message
	for index, item := range node.Items {
		copied := *item
		copied.Fields = make(map[string]*adapter.Node, len(item.Fields))
		for key, value := range item.Fields {
			copied.Fields[key] = value
		}
		copied.Fields["content"] = TranslatePlaceholders(item.Fields["content"], style)
		result.Items[index] = &copied
		role, roleOK := copied.Fields["role"].Value.(string)
		content, contentOK := copied.Fields["content"].Value.(string)
		if !roleOK || !contentOK {
			result.Value = nil
			result.Capability = adapter.CapabilityReadOnly
			result.Reason = copied.Fields["content"].Reason
			result.Code = copied.Fields["content"].Code
			return &result
		}
		messages = append(messages, syncdomain.Message{
			Role: NormalizeRole(role), Content: content,
		})
	}
	result.Value = messages
	return &result
}

// TranslateFields translates the placeholders of the named fields in place.
// A messages field translates the content of each message.
func TranslateFields(
	fields map[string]*adapter.Node,
	style PlaceholderStyle,
	names ...string,
) map[string]*adapter.Node {
	for _, name := range names {
		if node := fields[name]; node != nil {
			if name == "messages" {
				fields[name] = translateMessagePlaceholders(node, style)
			} else {
				fields[name] = TranslatePlaceholders(node, style)
			}
		}
	}
	return fields
}

func placeholdersToLaunchDarkly(value string, style PlaceholderStyle) (string, error) {
	var output strings.Builder
	for index := 0; index < len(value); {
		start, end, name, ok, err := nextSourcePlaceholder(value, index, style)
		if err != nil {
			return "", err
		}
		if !ok {
			output.WriteString(value[index:])
			break
		}
		output.WriteString(value[index:start])
		output.WriteString("{{" + name + "}}")
		index = end
	}
	return output.String(), nil
}

func nextSourcePlaceholder(value string, offset int, style PlaceholderStyle) (int, int, string, bool, error) {
	token := "{"
	if style == DollarPlaceholders {
		token = "${"
	}
	index := strings.Index(value[offset:], token)
	if index < 0 {
		if style == BracePlaceholders && strings.ContainsAny(value[offset:], "{}") {
			return 0, 0, "", false, fmt.Errorf("literal or escaped braces make placeholder translation ambiguous")
		}
		return 0, 0, "", false, nil
	}
	index += offset
	if style == BracePlaceholders && (strings.HasPrefix(value[index:], "{{") || index > 0 && value[index-1] == '{') {
		return 0, 0, "", false, fmt.Errorf("escaped braces make placeholder translation ambiguous")
	}
	nameStart := index + len(token)
	closeAt := strings.IndexByte(value[nameStart:], '}')
	if closeAt < 0 {
		return 0, 0, "", false, fmt.Errorf("placeholder is not closed")
	}
	closeAt += nameStart
	name := value[nameStart:closeAt]
	if !validPlaceholderName(name) {
		return 0, 0, "", false, fmt.Errorf("placeholder %q is ambiguous", name)
	}
	return index, closeAt + 1, name, true, nil
}

func placeholdersFromLaunchDarkly(value string, style PlaceholderStyle) (string, error) {
	var output strings.Builder
	for index := 0; index < len(value); {
		start := strings.Index(value[index:], "{{")
		if start < 0 {
			if strings.ContainsAny(value[index:], "{}") {
				return "", fmt.Errorf("literal braces make placeholder translation ambiguous")
			}
			output.WriteString(value[index:])
			break
		}
		start += index
		output.WriteString(value[index:start])
		closeAt := strings.Index(value[start+2:], "}}")
		if closeAt < 0 {
			return "", fmt.Errorf("LaunchDarkly placeholder is not closed")
		}
		closeAt += start + 2
		name := value[start+2 : closeAt]
		if !validPlaceholderName(name) {
			return "", fmt.Errorf("LaunchDarkly placeholder %q is ambiguous", name)
		}
		if style == DollarPlaceholders {
			output.WriteString("${" + name + "}")
		} else {
			output.WriteString("{" + name + "}")
		}
		index = closeAt + 2
	}
	return output.String(), nil
}

func validPlaceholderName(name string) bool {
	for index, character := range name {
		if character == '_' || unicode.IsLetter(character) || index > 0 && (unicode.IsDigit(character) || character == '.' || character == '-') {
			continue
		}
		return false
	}
	return name != ""
}
