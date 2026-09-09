package sync

import (
	"bytes"
	"errors"
	"fmt"
	"path"
	"strings"

	"gopkg.in/yaml.v3"
)

const configsDir = "configs"

type variationParser struct{}

func (variationParser) Dir() string {
	return configsDir
}

func (variationParser) Accept(relPath string) bool {
	if path.Ext(relPath) != ".prompt" {
		return false
	}

	dir, file := path.Split(relPath)
	dir = strings.TrimSuffix(dir, "/")

	return dir != "" && !strings.Contains(dir, "/") && file != ""
}

type variationFrontMatter struct {
	FormatVersion  int            `yaml:"formatVersion"`
	Upsert         bool           `yaml:"upsert"`
	Key            string         `yaml:"key"`
	Name           string         `yaml:"name"`
	ModelConfigKey string         `yaml:"modelConfigKey"`
	Model          map[string]any `yaml:"model"`
	OutputFormat   map[string]any `yaml:"outputFormat"`
	Tools          []toolRef      `yaml:"tools"`
}

type toolRef struct {
	Key     string `json:"key" yaml:"key"`
	Version int    `json:"version" yaml:"version"`
}

type message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type variationPayload struct {
	Key            string         `json:"key"`
	Name           string         `json:"name"`
	ModelConfigKey string         `json:"modelConfigKey,omitempty"`
	Model          map[string]any `json:"model,omitempty"`
	OutputFormat   map[string]any `json:"outputFormat,omitempty"`
	Tools          []toolRef      `json:"tools,omitempty"`
	Messages       []message      `json:"messages,omitempty"`
}

func (variationParser) Parse(file File) (SyncedResource, error) {
	front, body, err := splitFrontMatter(file.Data)
	if err != nil {
		return SyncedResource{}, err
	}

	var meta variationFrontMatter
	dec := yaml.NewDecoder(bytes.NewReader(front))
	dec.KnownFields(true)

	if err := dec.Decode(&meta); err != nil {
		return SyncedResource{}, fmt.Errorf("invalid front matter: %w", err)
	}

	if err := validateVariation(file.RelPath, meta); err != nil {
		return SyncedResource{}, err
	}

	messages, err := parseMessages(string(body))
	if err != nil {
		return SyncedResource{}, err
	}

	payload, err := marshalPayload(variationPayload{
		Key:            meta.Key,
		Name:           meta.Name,
		ModelConfigKey: meta.ModelConfigKey,
		Model:          meta.Model,
		OutputFormat:   meta.OutputFormat,
		Tools:          meta.Tools,
		Messages:       messages,
	})
	if err != nil {
		return SyncedResource{}, err
	}

	configKey := path.Dir(file.RelPath)

	return SyncedResource{
		Kind:        KindVariation,
		ProjectKey:  file.ProjectKey,
		LookupKey:   configKey + "/" + meta.Key,
		Payload:     payload,
		Fingerprint: Hash(payload),
		Upsert:      meta.Upsert,
	}, nil
}

func validateVariation(relPath string, meta variationFrontMatter) error {
	switch {
	case meta.FormatVersion == 0:
		return errors.New("formatVersion is required")
	case meta.FormatVersion != 1:
		return fmt.Errorf("unsupported formatVersion %d", meta.FormatVersion)
	case meta.Key == "":
		return errors.New("key is required")
	case meta.Name == "":
		return errors.New("name is required")
	}

	stem := strings.TrimSuffix(path.Base(relPath), ".prompt")
	if stem != meta.Key {
		return fmt.Errorf("key %q does not match filename %q", meta.Key, stem)
	}

	return nil
}

func splitFrontMatter(data []byte) (front, body []byte, err error) {
	// Drop a leading BOM and blank lines so --- is the first real token.
	s := bytes.TrimPrefix(data, []byte("\ufeff"))
	s = bytes.TrimLeft(s, "\r\n")

	// Opening fence must be --- on its own line, not ---key: value.
	if !bytes.HasPrefix(s, []byte("---")) {
		return nil, nil, errors.New("missing YAML front matter")
	}

	rest, ok := consumeLineEnding(s[3:])
	if !ok {
		return nil, nil, errors.New("missing YAML front matter")
	}

	// Closing fence is the first \n--- after the YAML block.
	idx := bytes.Index(rest, []byte("\n---"))
	if idx < 0 {
		return nil, nil, errors.New("unclosed YAML front matter")
	}

	front = bytes.TrimSpace(rest[:idx])
	// Skip the line ending after the closing ---; leftover bytes are the prompt body.
	after, ok := consumeLineEnding(rest[idx+4:])
	if !ok {
		after = nil
	}

	return front, bytes.TrimSpace(after), nil
}

func consumeLineEnding(s []byte) ([]byte, bool) {
	// EOF after --- is a valid end of line (file ends on the fence).
	if len(s) == 0 {
		return s, true
	}
	if s[0] == '\n' {
		return s[1:], true
	}
	// Accept \r and \r\n so Windows and old Mac files parse the same way.
	if s[0] == '\r' {
		s = s[1:]
		if len(s) > 0 && s[0] == '\n' {
			s = s[1:]
		}

		return s, true
	}

	// Next byte is content, so --- was not a fence on its own line.
	return s, false
}

var messageRoles = []string{"system", "user", "assistant"}

func parseMessages(body string) ([]message, error) {
	if strings.TrimSpace(body) == "" {
		return nil, nil
	}

	if _, _, _, ok := nextOpenTag(body, 0); !ok {
		return []message{{Role: "system", Content: strings.TrimSpace(body)}}, nil
	}

	var messages []message
	cursor := 0

	for cursor < len(body) {
		start, role, contentStart, ok := nextOpenTag(body, cursor)
		if !ok {
			if strings.TrimSpace(body[cursor:]) != "" {
				return nil, errors.New("unexpected text outside message tags")
			}
			break
		}
		if strings.TrimSpace(body[cursor:start]) != "" {
			return nil, errors.New("unexpected text outside message tags")
		}

		contentEnd, closeEnd, found := matchingClose(body, contentStart, role)
		if !found {
			return nil, fmt.Errorf("unclosed <%s> tag", role)
		}

		messages = append(messages, message{
			Role:    role,
			Content: strings.TrimSpace(body[contentStart:contentEnd]),
		})
		cursor = closeEnd
	}

	return messages, nil
}

func nextOpenTag(body string, from int) (start int, role string, contentStart int, ok bool) {
	start = -1

	for _, candidate := range messageRoles {
		tag := "<" + candidate + ">"
		i := strings.Index(body[from:], tag)
		if i < 0 {
			continue
		}

		abs := from + i
		if start < 0 || abs < start {
			start = abs
			role = candidate
			contentStart = abs + len(tag)
			ok = true
		}
	}

	return start, role, contentStart, ok
}

func matchingClose(body string, from int, role string) (contentEnd, closeEnd int, ok bool) {
	open := "<" + role + ">"
	close := "</" + role + ">"
	depth := 1
	i := from

	for i < len(body) {
		relOpen := strings.Index(body[i:], open)
		relClose := strings.Index(body[i:], close)
		if relClose < 0 {
			return 0, 0, false
		}

		if relOpen >= 0 && relOpen < relClose {
			depth++
			i += relOpen + len(open)
			continue
		}

		depth--
		closeAt := i + relClose
		if depth == 0 {
			return closeAt, closeAt + len(close), true
		}

		i = closeAt + len(close)
	}

	return 0, 0, false
}
