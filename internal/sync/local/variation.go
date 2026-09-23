package local

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"strings"

	"github.com/adrg/frontmatter"
	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
	"gopkg.in/yaml.v3"
)

const (
	configsDir          = "configs"
	variationFileSuffix = ".prompt.md"
)

type localFile struct {
	ProjectKey string
	RelPath    string
	Data       []byte
}

type variationFrontMatter struct {
	FormatVersion        int  `yaml:"formatVersion"`
	Upsert               bool `yaml:"upsert"`
	syncdomain.Variation `yaml:",inline"`
}

var yamlFrontMatter = frontmatter.NewFormat("---", "---", func(data []byte, destination any) error {
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)

	return decoder.Decode(destination)
})

func isVariationFile(relPath string) bool {
	if !strings.HasSuffix(relPath, variationFileSuffix) {
		return false
	}

	dir, file := path.Split(relPath)
	dir = strings.TrimSuffix(dir, "/")

	return dir != "" && !strings.Contains(dir, "/") && file != ""
}

func parseVariation(file localFile) (syncdomain.SyncedResource, error) {
	var meta variationFrontMatter
	body, err := parseYAMLFrontMatter(file.Data, &meta)
	if err != nil {
		return syncdomain.SyncedResource{}, err
	}

	if err := validateVariation(file.RelPath, meta); err != nil {
		return syncdomain.SyncedResource{}, err
	}

	variation := meta.Variation
	switch variation.Mode {
	case syncdomain.VariationModeAgent:
		variation.Instructions = strings.TrimSpace(string(body))
	case syncdomain.VariationModeCompletion:
		messages, err := parseCompletionMessages(string(body))
		if err != nil {
			return syncdomain.SyncedResource{}, err
		}
		variation.Messages = messages
	}

	payload, err := marshalPayload(variation)
	if err != nil {
		return syncdomain.SyncedResource{}, err
	}

	configKey := path.Dir(file.RelPath)

	return syncdomain.SyncedResource{
		Kind:       syncdomain.KindVariation,
		ProjectKey: file.ProjectKey,
		LookupKey:  configKey + "/" + meta.Key,
		Payload:    payload,
		Upsert:     meta.Upsert,
	}, nil
}

func validateVariation(relPath string, meta variationFrontMatter) error {
	switch {
	case meta.FormatVersion == 0:
		return errors.New("formatVersion is required")
	case meta.FormatVersion != 1:
		return fmt.Errorf("unsupported formatVersion %d", meta.FormatVersion)
	case meta.Mode == "":
		return errors.New("mode is required")
	case !meta.Mode.Valid():
		return fmt.Errorf("unsupported mode %q", meta.Mode)
	case meta.Key == "":
		return errors.New("key is required")
	case meta.Name == "":
		return errors.New("name is required")
	}

	stem := strings.TrimSuffix(path.Base(relPath), variationFileSuffix)
	if stem != meta.Key {
		return fmt.Errorf("key %q does not match filename %q", meta.Key, stem)
	}

	return nil
}

func marshalPayload(value any) (json.RawMessage, error) {
	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	encoder.SetEscapeHTML(false)

	if err := encoder.Encode(value); err != nil {
		return nil, fmt.Errorf("marshal payload: %w", err)
	}

	return bytes.TrimSuffix(buf.Bytes(), []byte("\n")), nil
}

func parseYAMLFrontMatter(data []byte, destination any) ([]byte, error) {
	source := bytes.TrimLeft(bytes.TrimPrefix(data, []byte("\ufeff")), "\r\n")
	hasStart := bytes.Equal(source, []byte("---")) ||
		bytes.HasPrefix(source, []byte("---\n")) ||
		bytes.HasPrefix(source, []byte("---\r\n"))
	if !hasStart {
		return nil, errors.New("missing YAML front matter")
	}

	body, err := frontmatter.MustParse(bytes.NewReader(source), destination, yamlFrontMatter)
	if errors.Is(err, frontmatter.ErrNotFound) {
		return nil, errors.New("unclosed YAML front matter")
	}
	if err != nil {
		return nil, fmt.Errorf("invalid front matter: %w", err)
	}

	return body, nil
}

var messageRoles = []string{"system", "user", "assistant"}

func parseCompletionMessages(body string) ([]syncdomain.Message, error) {
	if strings.TrimSpace(body) == "" {
		return nil, nil
	}

	if _, _, _, ok := nextOpenTag(body, 0); !ok {
		return []syncdomain.Message{{
			Role:    "system",
			Content: strings.TrimSpace(body),
		}}, nil
	}

	var messages []syncdomain.Message
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

		messages = append(messages, syncdomain.Message{
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
		index := strings.Index(body[from:], tag)
		if index < 0 {
			continue
		}

		absolute := from + index
		if start < 0 || absolute < start {
			start = absolute
			role = candidate
			contentStart = absolute + len(tag)
			ok = true
		}
	}

	return start, role, contentStart, ok
}

func matchingClose(body string, from int, role string) (contentEnd, closeEnd int, ok bool) {
	open := "<" + role + ">"
	close := "</" + role + ">"
	depth := 1
	index := from

	for index < len(body) {
		relativeOpen := strings.Index(body[index:], open)
		relativeClose := strings.Index(body[index:], close)
		if relativeClose < 0 {
			return 0, 0, false
		}

		if relativeOpen >= 0 && relativeOpen < relativeClose {
			depth++
			index += relativeOpen + len(open)

			continue
		}

		depth--
		closeAt := index + relativeClose
		if depth == 0 {
			return closeAt, closeAt + len(close), true
		}

		index = closeAt + len(close)
	}

	return 0, 0, false
}
