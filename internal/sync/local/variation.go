package local

import (
	"bytes"
	"errors"
	"fmt"
	"path"
	"slices"
	"strings"

	"github.com/adrg/frontmatter"
	"gopkg.in/yaml.v3"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
	syncreference "github.com/launchdarkly/ldcli/internal/sync/reference"
)

// A variation file has YAML front matter and a prompt body:
//
//	---
//	formatVersion: 1
//	mode: completion
//	key: default
//	name: Default
//	---
//
//	<system>
//	You are helpful.
//	</system>
//
// An agent body is the instructions. A completion body is a list of role
// blocks. A completion body without role blocks is one system message. A
// linked variation has a "ref" field and no body.
const variationFormatVersion = 1

type variationFrontMatter struct {
	FormatVersion        int        `yaml:"formatVersion"`
	Upsert               bool       `yaml:"upsert"`
	Ref                  *Reference `yaml:"ref,omitempty"`
	syncdomain.Variation `yaml:",inline"`
}

// localFile is one variation file and the readers for the files that it references.
type localFile struct {
	ProjectKey     string
	RelPath        string
	Data           []byte
	ReadReference  func(Reference) ([]byte, error)
	ReadAttachment func(syncdomain.AttachmentKind, string) (syncdomain.Attachment, error)
}

// parseVariation reads one variation file, including the content of a linked
// file and every attachment that it references. RelPath is relative to the
// configs directory, for example "support/default.prompt.md".
func parseVariation(file localFile) (syncdomain.SyncedResource, error) {
	meta, body, err := parseVariationFrontMatter(file.RelPath, file.Data)
	if err != nil {
		return syncdomain.SyncedResource{}, err
	}

	variation := meta.Variation
	if meta.Ref != nil {
		// The variation file owns the identity and the model settings. The
		// linked file owns only the prompt content.
		if strings.TrimSpace(string(body)) != "" {
			return syncdomain.SyncedResource{}, errors.New("referenced variation cannot also contain an inline prompt body")
		}
		content, err := file.ReadReference(*meta.Ref)
		if err != nil {
			return syncdomain.SyncedResource{}, err
		}
		if err := syncreference.ApplyToVariation(meta.Ref.Format, content, &variation); err != nil {
			return syncdomain.SyncedResource{}, err
		}
		if stem := variationStem(file.RelPath); variation.Key != stem {
			return syncdomain.SyncedResource{}, fmt.Errorf("referenced prompt key %q does not match filename %q", variation.Key, stem)
		}
	} else if err := parsePromptBody(string(body), &variation); err != nil {
		return syncdomain.SyncedResource{}, err
	}

	if err := variation.HydrateAttachments(file.ReadAttachment); err != nil {
		return syncdomain.SyncedResource{}, err
	}

	configKey := path.Dir(file.RelPath)
	return syncdomain.SyncedResource{
		Kind:       syncdomain.KindVariation,
		ProjectKey: file.ProjectKey,
		LookupKey:  configKey + "/" + variation.Key,
		Upsert:     meta.Upsert,
		Ref:        meta.Ref,
		Variation:  variation,
	}, nil
}

// parseVariationFrontMatter decodes and validates the front matter, and
// returns the remaining body.
func parseVariationFrontMatter(relPath string, data []byte) (variationFrontMatter, []byte, error) {
	var meta variationFrontMatter
	body, err := parseYAMLFrontMatter(data, &meta)
	if err != nil {
		return variationFrontMatter{}, nil, err
	}

	switch {
	case meta.FormatVersion == 0:
		return variationFrontMatter{}, nil, errors.New("formatVersion is required")
	case meta.FormatVersion != variationFormatVersion:
		return variationFrontMatter{}, nil, fmt.Errorf("unsupported formatVersion %d", meta.FormatVersion)
	}
	if err := meta.Variation.Validate(); err != nil {
		return variationFrontMatter{}, nil, err
	}
	if stem := variationStem(relPath); meta.Key != stem {
		return variationFrontMatter{}, nil, fmt.Errorf("key %q does not match filename %q", meta.Key, stem)
	}
	return meta, body, nil
}

// parsePromptBody stores an inline body as agent instructions or as
// completion messages.
func parsePromptBody(body string, variation *syncdomain.Variation) error {
	if variation.Mode == syncdomain.VariationModeAgent {
		variation.Instructions = syncdomain.NormalizePromptText(body)
		return nil
	}
	messages, err := parseCompletionMessages(body)
	if err != nil {
		return err
	}
	variation.Messages = messages
	return nil
}

// renderVariationFile renders the front matter and the prompt body of one
// variation file. The result parses back to the same variation.
func renderVariationFile(file VariationFile) ([]byte, error) {
	variation := file.Variation
	if err := variation.Validate(); err != nil {
		return nil, fmt.Errorf("variation %q: %w", variation.Key, err)
	}
	variation.Tools = slices.Clone(variation.Tools)
	variation.Skills = slices.Clone(variation.Skills)
	variation.SortAttachments()
	switch {
	case variation.Mode == syncdomain.VariationModeAgent && len(variation.Messages) != 0:
		return nil, fmt.Errorf("agent variation %q cannot contain messages", variation.Key)
	case variation.Mode == syncdomain.VariationModeCompletion && variation.Instructions != "":
		return nil, fmt.Errorf("completion variation %q cannot contain instructions", variation.Key)
	}
	if file.Ref != nil {
		if err := validateReference(*file.Ref); err != nil {
			return nil, err
		}
	}

	frontMatter, err := marshalYAML(variationFrontMatter{
		FormatVersion: variationFormatVersion,
		Upsert:        file.Upsert,
		Ref:           file.Ref,
		Variation:     variation,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal variation %q: %w", variation.Key, err)
	}

	var content bytes.Buffer
	content.WriteString("---\n")
	content.Write(frontMatter)
	content.WriteString("---\n")

	// A linked variation has no body, so that only one file holds the prompt.
	switch {
	case file.Ref != nil:
	case variation.Mode == syncdomain.VariationModeAgent:
		if instructions := syncdomain.NormalizePromptText(variation.Instructions); instructions != "" {
			fmt.Fprintf(&content, "\n%s\n", instructions)
		}
	default:
		for _, message := range variation.Messages {
			if !syncdomain.ValidMessageRole(message.Role) {
				return nil, fmt.Errorf("variation %q has unsupported message role %q", variation.Key, message.Role)
			}
			body := escapeMessageContent(syncdomain.NormalizePromptText(message.Content), message.Role)
			fmt.Fprintf(&content, "\n<%s>\n%s\n</%s>\n", message.Role, body, message.Role)
		}
	}
	return content.Bytes(), nil
}

// variationStem returns the variation key that a file name declares.
func variationStem(relPath string) string {
	return strings.TrimSuffix(path.Base(relPath), variationFileSuffix)
}

var yamlFrontMatter = frontmatter.NewFormat("---", "---", func(data []byte, destination any) error {
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	return decoder.Decode(destination)
})

// parseYAMLFrontMatter decodes strict YAML front matter and returns the body
// that follows it. It accepts a byte order mark and Windows line endings.
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

// marshalYAML encodes front matter with a two-space indent.
func marshalYAML(value any) ([]byte, error) {
	var buffer bytes.Buffer
	encoder := yaml.NewEncoder(&buffer)
	encoder.SetIndent(2)
	if err := encoder.Encode(value); err != nil {
		return nil, err
	}
	if err := encoder.Close(); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}
