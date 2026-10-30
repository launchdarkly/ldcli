package reference

import (
	"encoding/json"
	"fmt"
	"strconv"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
)

type sourcePath struct {
	root  bool
	steps []pathStep
}

type pathStep struct {
	key     string
	filters []pathFilter
	index   *int
}

type pathFilter struct {
	key   string
	value string
}

// ParsePath checks the shared select and map path grammar.
func ParsePath(text string) error {
	_, err := parsePath(text)
	return err
}

func parsePath(text string) (sourcePath, error) {
	if text == "$" {
		return sourcePath{root: true}, nil
	}
	if text == "" {
		return sourcePath{}, pathSyntaxError(text, 0, "path is empty")
	}

	parser := pathParser{text: text}
	key, err := parser.readKey()
	if err != nil {
		return sourcePath{}, err
	}
	result := sourcePath{steps: []pathStep{{key: key}}}
	for parser.offset < len(text) {
		switch text[parser.offset] {
		case '.':
			parser.offset++
			key, err = parser.readIdentifier()
			if err != nil {
				return sourcePath{}, err
			}
			result.steps = append(result.steps, pathStep{key: key})
		case '[':
			step, kind, err := parser.readBracket()
			if err != nil {
				return sourcePath{}, err
			}
			if kind == bracketFilter {
				last := len(result.steps) - 1
				if result.steps[last].index != nil {
					return sourcePath{}, pathSyntaxError(text, parser.offset, "a selector must follow a field")
				}
				if len(result.steps[last].filters) != 0 {
					return sourcePath{}, pathSyntaxError(text, parser.offset, "a path step can have only one selector")
				}
				result.steps[last].filters = step.filters
			} else {
				result.steps = append(result.steps, step)
			}
		default:
			return sourcePath{}, pathSyntaxError(text, parser.offset, "expected a dot or bracket")
		}
	}
	return result, nil
}

type pathParser struct {
	text   string
	offset int
}

type bracketKind int

const (
	bracketField bracketKind = iota
	bracketFilter
	bracketIndex
)

func (parser *pathParser) readKey() (string, error) {
	if parser.offset < len(parser.text) && parser.text[parser.offset] == '[' {
		step, kind, err := parser.readBracket()
		if err != nil {
			return "", err
		}
		if kind != bracketField {
			return "", pathSyntaxError(parser.text, parser.offset, "a path cannot start with a selector or index")
		}
		return step.key, nil
	}
	return parser.readIdentifier()
}

func (parser *pathParser) readIdentifier() (string, error) {
	start := parser.offset
	if start >= len(parser.text) || !identifierStart(parser.text[start]) {
		return "", pathSyntaxError(parser.text, start, "expected an identifier")
	}
	parser.offset++
	for parser.offset < len(parser.text) && identifierContinue(parser.text[parser.offset]) {
		parser.offset++
	}
	return parser.text[start:parser.offset], nil
}

func (parser *pathParser) readBracket() (pathStep, bracketKind, error) {
	start := parser.offset
	parser.offset++
	if parser.offset < len(parser.text) && parser.text[parser.offset] >= '0' && parser.text[parser.offset] <= '9' {
		return parser.readIndex(start)
	}
	key, err := parser.readBracketKey()
	if err != nil {
		return pathStep{}, bracketField, err
	}
	if parser.offset >= len(parser.text) {
		return pathStep{}, bracketField, pathSyntaxError(parser.text, start, "unclosed bracket")
	}
	if parser.text[parser.offset] == ']' {
		parser.offset++
		return pathStep{key: key}, bracketField, nil
	}
	if parser.text[parser.offset] != '=' {
		return pathStep{}, bracketField, pathSyntaxError(parser.text, parser.offset, "expected equals or closing bracket")
	}
	parser.offset++
	if parser.offset >= len(parser.text) || parser.text[parser.offset] != '"' {
		return pathStep{}, bracketFilter, pathSyntaxError(parser.text, parser.offset, "selector value must be a JSON string")
	}
	value, err := parser.readJSONString()
	if err != nil {
		return pathStep{}, bracketFilter, err
	}
	filters := []pathFilter{{key: key, value: value}}
	for parser.offset < len(parser.text) && parser.text[parser.offset] == ',' {
		parser.offset++
		nextKey, err := parser.readBracketKey()
		if err != nil {
			return pathStep{}, bracketFilter, err
		}
		if parser.offset >= len(parser.text) || parser.text[parser.offset] != '=' {
			return pathStep{}, bracketFilter, pathSyntaxError(parser.text, parser.offset, "expected equals")
		}
		parser.offset++
		if parser.offset >= len(parser.text) || parser.text[parser.offset] != '"' {
			return pathStep{}, bracketFilter, pathSyntaxError(parser.text, parser.offset, "selector value must be a JSON string")
		}
		nextValue, err := parser.readJSONString()
		if err != nil {
			return pathStep{}, bracketFilter, err
		}
		filters = append(filters, pathFilter{key: nextKey, value: nextValue})
	}
	if parser.offset >= len(parser.text) || parser.text[parser.offset] != ']' {
		return pathStep{}, bracketFilter, pathSyntaxError(parser.text, parser.offset, "expected a closing bracket")
	}
	parser.offset++
	return pathStep{filters: filters}, bracketFilter, nil
}

func (parser *pathParser) readIndex(start int) (pathStep, bracketKind, error) {
	numberStart := parser.offset
	for parser.offset < len(parser.text) && parser.text[parser.offset] >= '0' && parser.text[parser.offset] <= '9' {
		parser.offset++
	}
	raw := parser.text[numberStart:parser.offset]
	if len(raw) > 1 && raw[0] == '0' {
		return pathStep{}, bracketIndex, pathSyntaxError(parser.text, numberStart, "an index cannot have a leading zero")
	}
	if parser.offset >= len(parser.text) || parser.text[parser.offset] != ']' {
		return pathStep{}, bracketIndex, pathSyntaxError(parser.text, parser.offset, "an index must be a decimal integer")
	}
	value, err := strconv.ParseUint(raw, 10, 31)
	if err != nil {
		return pathStep{}, bracketIndex, pathSyntaxError(parser.text, numberStart, "index exceeds 2147483647")
	}
	parser.offset++
	index := int(value)
	return pathStep{index: &index}, bracketIndex, nil
}

func (parser *pathParser) readBracketKey() (string, error) {
	if parser.offset < len(parser.text) && parser.text[parser.offset] == '"' {
		return parser.readJSONString()
	}
	return parser.readIdentifier()
}

func (parser *pathParser) readJSONString() (string, error) {
	start := parser.offset
	parser.offset++
	for parser.offset < len(parser.text) {
		switch parser.text[parser.offset] {
		case '\\':
			parser.offset += 2
		case '"':
			parser.offset++
			quoted := parser.text[start:parser.offset]
			if err := rejectUnpairedSurrogate(quoted); err != nil {
				return "", pathSyntaxError(parser.text, start, err.Error())
			}
			var value string
			if err := json.Unmarshal([]byte(quoted), &value); err != nil {
				return "", pathSyntaxError(parser.text, start, "invalid JSON string")
			}
			return value, nil
		default:
			parser.offset++
		}
	}
	return "", pathSyntaxError(parser.text, start, "unclosed JSON string")
}

func rejectUnpairedSurrogate(quoted string) error {
	for index := 0; index+5 < len(quoted); index++ {
		if quoted[index:index+2] != `\u` {
			continue
		}
		code, err := strconv.ParseUint(quoted[index+2:index+6], 16, 16)
		if err != nil {
			continue
		}
		switch {
		case code >= 0xd800 && code <= 0xdbff:
			if index+11 >= len(quoted) || quoted[index+6:index+8] != `\u` {
				return fmt.Errorf("unpaired Unicode surrogate")
			}
			low, err := strconv.ParseUint(quoted[index+8:index+12], 16, 16)
			if err != nil || low < 0xdc00 || low > 0xdfff {
				return fmt.Errorf("unpaired Unicode surrogate")
			}
			index += 6
		case code >= 0xdc00 && code <= 0xdfff:
			return fmt.Errorf("unpaired Unicode surrogate")
		}
	}
	return nil
}

func pathSyntaxError(path string, offset int, reason string) error {
	if offset > len(path) {
		offset = len(path)
	}
	return syncdomain.NewProblem(
		syncdomain.ErrorInvalidPathSyntax,
		fmt.Sprintf("invalid path at byte %d: %s", offset, reason),
		"Use $, named fields, zero-based indexes, keyed selectors, or quoted literal keys.",
	)
}

func identifierStart(value byte) bool {
	return value == '_' || value >= 'A' && value <= 'Z' || value >= 'a' && value <= 'z'
}

func identifierContinue(value byte) bool {
	return identifierStart(value) || value >= '0' && value <= '9'
}
