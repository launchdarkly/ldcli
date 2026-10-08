package local

import (
	"errors"
	"fmt"
	"strings"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
)

// parseCompletionMessages reads the role blocks of a completion body, for
// example "<user>Hi</user>". A body without role blocks is one system message.
func parseCompletionMessages(body string) ([]syncdomain.Message, error) {
	body = syncdomain.NormalizePromptText(body)
	if body == "" {
		return nil, nil
	}
	if _, _, _, found := nextOpenTag(body, 0); !found {
		return []syncdomain.Message{{Role: syncdomain.RoleSystem, Content: body}}, nil
	}

	var messages []syncdomain.Message
	for cursor := 0; cursor < len(body); {
		start, role, contentStart, found := nextOpenTag(body, cursor)
		if !found {
			start = len(body)
		}
		if strings.TrimSpace(body[cursor:start]) != "" {
			return nil, errors.New("unexpected text outside message tags")
		}
		if !found {
			break
		}

		contentEnd, closeEnd, closed := matchingClose(body, contentStart, role)
		if !closed {
			return nil, fmt.Errorf("unclosed <%s> tag", role)
		}
		content := syncdomain.NormalizePromptText(body[contentStart:contentEnd])
		messages = append(messages, syncdomain.Message{Role: role, Content: unescapeMessageContent(content, role)})
		cursor = closeEnd
	}
	return messages, nil
}

// escapeMessageContent escapes the role tags and backslashes in a message,
// so that the rendered body parses back to the same message.
func escapeMessageContent(content, role string) string {
	content = strings.ReplaceAll(content, `\`, `\\`)
	content = strings.ReplaceAll(content, "<"+role+">", `<\`+role+">")
	return strings.ReplaceAll(content, "</"+role+">", `<\/`+role+">")
}

// unescapeMessageContent reverses escapeMessageContent.
func unescapeMessageContent(content, role string) string {
	content = strings.ReplaceAll(content, `<\/`+role+">", "</"+role+">")
	content = strings.ReplaceAll(content, `<\`+role+">", "<"+role+">")
	return strings.ReplaceAll(content, `\\`, `\`)
}

// nextOpenTag finds the first role tag at or after from.
func nextOpenTag(body string, from int) (start int, role string, contentStart int, found bool) {
	start = -1
	for _, candidate := range syncdomain.MessageRoles {
		tag := "<" + candidate + ">"
		index := strings.Index(body[from:], tag)
		if index < 0 {
			continue
		}
		if absolute := from + index; start < 0 || absolute < start {
			start, role, contentStart, found = absolute, candidate, absolute+len(tag), true
		}
	}
	return start, role, contentStart, found
}

// matchingClose finds the closing tag that balances the role block that
// starts at from. A nested block with the same role increases the depth.
func matchingClose(body string, from int, role string) (contentEnd, closeEnd int, found bool) {
	openTag, closeTag := "<"+role+">", "</"+role+">"
	depth := 1
	for index := from; index < len(body); {
		nextClose := strings.Index(body[index:], closeTag)
		if nextClose < 0 {
			return 0, 0, false
		}
		if nextOpen := strings.Index(body[index:], openTag); nextOpen >= 0 && nextOpen < nextClose {
			depth++
			index += nextOpen + len(openTag)
			continue
		}

		closeAt := index + nextClose
		depth--
		if depth == 0 {
			return closeAt, closeAt + len(closeTag), true
		}
		index = closeAt + len(closeTag)
	}
	return 0, 0, false
}
