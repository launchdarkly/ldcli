package reference

import (
	"fmt"
	"strings"
	"unicode"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
)

type placeholderStyle string

const (
	bracePlaceholder  placeholderStyle = "brace"
	dollarPlaceholder placeholderStyle = "dollar"
)

func translatePlaceholderNode(node *sourceNode, style placeholderStyle) *sourceNode {
	if node == nil {
		return nil
	}
	value, ok := node.value.(string)
	if !ok {
		return node
	}
	translated, err := placeholdersToLaunchDarkly(value, style)
	if err != nil {
		result := *node
		result.value = nil
		result.capability = CapabilityReadOnly
		result.reason = err.Error()
		result.code = syncdomain.ErrorPlaceholderAmbiguous
		result.encode = nil
		return &result
	}
	result := *node
	result.value = translated
	encode := node.encode
	result.encode = func(value any) ([]byte, error) {
		text, ok := value.(string)
		if !ok {
			return nil, mapTypeError("source", "a string")
		}
		sourceText, err := placeholdersFromLaunchDarkly(text, style)
		if err != nil {
			return nil, err
		}
		return encode(sourceText)
	}
	return &result
}

func translateMessagePlaceholders(node *sourceNode, style placeholderStyle) *sourceNode {
	if node == nil || node.items == nil {
		return node
	}
	result := *node
	result.items = make([]*sourceNode, len(node.items))
	var messages []syncdomain.Message
	for index, item := range node.items {
		copied := *item
		copied.fields = make(map[string]*sourceNode, len(item.fields))
		for key, value := range item.fields {
			copied.fields[key] = value
		}
		copied.fields["content"] = translatePlaceholderNode(item.fields["content"], style)
		result.items[index] = &copied
		role, roleOK := copied.fields["role"].value.(string)
		content, contentOK := copied.fields["content"].value.(string)
		if !roleOK || !contentOK {
			result.value = nil
			result.capability = CapabilityReadOnly
			result.reason = copied.fields["content"].reason
			result.code = copied.fields["content"].code
			return &result
		}
		messages = append(messages, syncdomain.Message{
			Role: normalizeMessageRole(role), Content: content,
		})
	}
	result.value = messages
	return &result
}

func translateFieldPlaceholders(
	fields map[string]*sourceNode,
	style placeholderStyle,
	names ...string,
) map[string]*sourceNode {
	for _, name := range names {
		if node := fields[name]; node != nil {
			if name == "messages" {
				fields[name] = translateMessagePlaceholders(node, style)
			} else {
				fields[name] = translatePlaceholderNode(node, style)
			}
		}
	}
	return fields
}

func placeholdersToLaunchDarkly(value string, style placeholderStyle) (string, error) {
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

func nextSourcePlaceholder(value string, offset int, style placeholderStyle) (int, int, string, bool, error) {
	token := "{"
	if style == dollarPlaceholder {
		token = "${"
	}
	index := strings.Index(value[offset:], token)
	if index < 0 {
		if style == bracePlaceholder && strings.ContainsAny(value[offset:], "{}") {
			return 0, 0, "", false, fmt.Errorf("literal or escaped braces make placeholder translation ambiguous")
		}
		return 0, 0, "", false, nil
	}
	index += offset
	if style == bracePlaceholder && (strings.HasPrefix(value[index:], "{{") || index > 0 && value[index-1] == '{') {
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

func placeholdersFromLaunchDarkly(value string, style placeholderStyle) (string, error) {
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
		if style == dollarPlaceholder {
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
