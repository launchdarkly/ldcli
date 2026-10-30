package reference

import (
	"bytes"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
)

type markdownProfileAdapter struct {
	format      string
	frontMatter bool
}

func (adapter markdownProfileAdapter) Format() string { return adapter.format }
func (adapter markdownProfileAdapter) Inspect(bundle SourceBundle) (Inspection, error) {
	root, err := parseMarkdownProfile(bundle.Primary, adapter.frontMatter)
	return Inspection{root: root}, err
}
func (markdownProfileAdapter) Defaults(kind syncdomain.Kind) (string, map[string]string) {
	if kind == syncdomain.KindVariation {
		return "$", map[string]string{string(syncdomain.FieldPrompt): "body"}
	}
	return "$", nil
}

func init() {
	mustRegister(markdownProfileAdapter{format: "cursor", frontMatter: true})
	mustRegister(markdownProfileAdapter{format: "copilot", frontMatter: true})
	mustRegister(markdownProfileAdapter{format: "agents-markdown"})
	mustRegister(markdownProfileAdapter{format: "claude-markdown", frontMatter: true})
}

func parseMarkdownProfile(document Document, supportsFrontMatter bool) (*sourceNode, error) {
	data := document.Data
	bodyStart := 0
	frontmatter := &sourceNode{value: map[string]any{}, fields: map[string]*sourceNode{}}
	trimmed := bytes.TrimPrefix(data, []byte("\ufeff"))
	bomLength := len(data) - len(trimmed)
	if supportsFrontMatter && bytes.HasPrefix(trimmed, []byte("---")) {
		region, start, err := splitFrontMatter(data)
		if err != nil {
			return nil, err
		}
		bodyStart = start
		var documentNode yaml.Node
		if err := yaml.Unmarshal(region.data, &documentNode); err != nil {
			return nil, fmt.Errorf("invalid profile front matter: %w", err)
		}
		if len(documentNode.Content) != 0 {
			frontmatter, err = yamlSourceNode(region.data, documentNode.Content[0], region.startOffset)
			if err != nil {
				return nil, err
			}
		}
	} else {
		bodyStart = bomLength
	}
	bodyData := data[bodyStart:]
	body := markdownTextNode(bodyData, bodyStart, len(data))
	sections := &sourceNode{items: markdownSections(bodyData), collection: true}
	offsetSourceSpans(sections, bodyStart)
	markers, err := markdownMarkers(bodyData, bodyStart)
	if err != nil {
		return nil, err
	}
	return &sourceNode{fields: map[string]*sourceNode{
		"frontmatter": frontmatter,
		"body":        body,
		"sections":    sections,
		"markers":     markers,
	}}, nil
}

func markdownMarkers(data []byte, base int) (*sourceNode, error) {
	const beginPrefix = "<!-- launchdarkly:begin "
	const endPrefix = "<!-- launchdarkly:end "
	var items []*sourceNode
	openID, bodyStart := "", 0
	for offset := 0; offset < len(data); {
		lineEnd := nextLineEnd(data, offset)
		line := strings.TrimSpace(string(data[offset:lineEnd]))
		switch {
		case strings.HasPrefix(line, beginPrefix) && strings.HasSuffix(line, "-->"):
			if openID != "" {
				return nil, fmt.Errorf("nested LaunchDarkly marker %q", openID)
			}
			openID = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(line, beginPrefix), "-->"))
			bodyStart = lineEnd
		case strings.HasPrefix(line, endPrefix) && strings.HasSuffix(line, "-->"):
			id := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(line, endPrefix), "-->"))
			if openID == "" || id != openID {
				return nil, fmt.Errorf("LaunchDarkly marker %q has no matching begin marker", id)
			}
			body := markdownTextNode(data[bodyStart:offset], base+bodyStart, base+offset)
			items = append(items, &sourceNode{fields: map[string]*sourceNode{
				"id": {value: id}, "body": body,
			}})
			openID = ""
		}
		offset = lineEnd
	}
	if openID != "" {
		return nil, fmt.Errorf("LaunchDarkly marker %q is not closed", openID)
	}
	return &sourceNode{items: items, collection: true}, nil
}

func offsetSourceSpans(node *sourceNode, offset int) {
	if node == nil {
		return
	}
	if node.start != 0 || node.end != 0 {
		node.start += offset
		node.end += offset
	}
	for _, child := range node.fields {
		offsetSourceSpans(child, offset)
	}
	for _, child := range node.items {
		offsetSourceSpans(child, offset)
	}
}
