// Package markdown inspects Markdown documents and Agent Skill files.
//
// A Markdown document exposes four logical fields. frontmatter is the YAML
// front matter, or an empty object. body is the text after the front matter.
// sections has one item for each heading, with the heading text and the body
// below it. markers has one item for each LaunchDarkly marker region. The
// same adapter serves plain prompt files, AGENTS.md, CLAUDE.md, Cursor rules,
// and Copilot instructions, because only the mapped value changes.
package markdown

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"gopkg.in/yaml.v3"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
	"github.com/launchdarkly/ldcli/internal/sync/adapter"
	"github.com/launchdarkly/ldcli/internal/sync/adapter/yamltree"
)

type markdownAdapter struct{}

func (markdownAdapter) Format() string   { return "markdown" }
func (markdownAdapter) Behavior() string { return "markdown/1" }

func (markdownAdapter) Inspect(bundle adapter.SourceBundle) (*adapter.Node, error) {
	return parse(bundle.Primary, false)
}

func (markdownAdapter) Defaults(kind syncdomain.Kind) (string, map[string]string) {
	if kind == syncdomain.KindVariation {
		return "$", map[string]string{string(syncdomain.FieldPrompt): "body"}
	}
	return "$", nil
}

type agentSkillAdapter struct{}

func (agentSkillAdapter) Format() string   { return "agent-skill" }
func (agentSkillAdapter) Behavior() string { return "agent-skill/1" }

func (agentSkillAdapter) Inspect(bundle adapter.SourceBundle) (*adapter.Node, error) {
	return parseAgentSkill(bundle.Primary)
}

func (agentSkillAdapter) Defaults(kind syncdomain.Kind) (string, map[string]string) {
	if kind == syncdomain.KindSkill {
		return "$", map[string]string{
			string(syncdomain.FieldDescription): "frontmatter.description",
			string(syncdomain.FieldMarkdown):    "body",
		}
	}
	return "$", nil
}

func init() {
	adapter.MustRegister(markdownAdapter{})
	adapter.MustRegister(agentSkillAdapter{})
}

// parse builds the logical document. With exactBody, the body keeps every
// byte, as a skill needs. Otherwise the body is normalized prompt text.
func parse(document adapter.Document, exactBody bool) (*adapter.Node, error) {
	data := document.Data
	if !utf8.Valid(data) {
		return nil, fmt.Errorf("%s: source is not valid UTF-8", document.Path)
	}
	bodyStart := len(data) - len(bytes.TrimPrefix(data, []byte("\ufeff")))
	frontMatter := &adapter.Node{Value: map[string]any{}, Fields: map[string]*adapter.Node{}}
	if hasFrontMatter(data[bodyStart:]) {
		region, end, err := splitFrontMatter(data)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", document.Path, err)
		}
		if frontMatter, err = parseFrontMatter(region); err != nil {
			return nil, fmt.Errorf("%s: %w", document.Path, err)
		}
		bodyStart = end
	}
	body := data[bodyStart:]
	bodyNode := textNode(body, bodyStart, len(data))
	if exactBody {
		bodyNode = exactNode(body, bodyStart, len(data))
	}
	markers, err := markerRegions(body, bodyStart)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", document.Path, err)
	}
	return &adapter.Node{Fields: map[string]*adapter.Node{
		"frontmatter": frontMatter,
		"body":        bodyNode,
		"sections":    {Items: sections(body, bodyStart), Collection: true},
		"markers":     markers,
	}}, nil
}

// parseAgentSkill builds a SKILL.md document and checks the fields that a
// skill needs: a string description in the front matter and a body.
func parseAgentSkill(document adapter.Document) (*adapter.Node, error) {
	if !hasFrontMatter(bytes.TrimPrefix(document.Data, []byte("\ufeff"))) {
		return nil, fmt.Errorf("%s: missing YAML front matter", document.Path)
	}
	root, err := parse(document, true)
	if err != nil {
		return nil, err
	}
	description := root.Fields["frontmatter"].Fields["description"]
	switch {
	case root.Fields["frontmatter"].Fields == nil:
		return nil, fmt.Errorf("%s: front matter must be a mapping", document.Path)
	case description == nil:
		return nil, syncdomain.NewProblem(
			syncdomain.ErrorPathNotFound,
			"front matter field \"description\" was not found",
			"Add the field or update the map path.",
		).WithFile(document.Path, 0, 0)
	case description.Value == nil:
		return nil, fmt.Errorf("%s: front matter description must be a string", document.Path)
	}
	if _, ok := description.Value.(string); !ok {
		return nil, fmt.Errorf("%s: front matter description must be a string", document.Path)
	}
	if strings.TrimSpace(root.Fields["body"].Value.(string)) == "" {
		return nil, fmt.Errorf("%s: skill Markdown is required", document.Path)
	}
	return root, nil
}

// hasFrontMatter reports whether data starts with a front matter fence. A
// fence is three dashes at the start of a line and nothing else.
func hasFrontMatter(data []byte) bool {
	return isFence(string(data[:nextLineEnd(data, 0)]))
}

func isFence(line string) bool {
	return strings.TrimRight(line, "\r\n") == "---"
}

// splitFrontMatter returns the YAML region between the front matter fences
// and the offset where the body starts.
func splitFrontMatter(data []byte) (yamltree.Region, int, error) {
	offset := len(data) - len(bytes.TrimPrefix(data, []byte("\ufeff")))
	contentStart := nextLineEnd(data, offset)
	for lineStart := contentStart; lineStart < len(data); {
		lineEnd := nextLineEnd(data, lineStart)
		if isFence(string(data[lineStart:lineEnd])) {
			bodyStart := lineEnd
			if bytes.HasPrefix(data[bodyStart:], []byte("\r\n")) {
				bodyStart += 2
			} else if bytes.HasPrefix(data[bodyStart:], []byte("\n")) {
				bodyStart++
			}
			return yamltree.Region{Data: data[contentStart:lineStart], StartOffset: contentStart}, bodyStart, nil
		}
		lineStart = lineEnd
	}
	return yamltree.Region{}, 0, errors.New("unclosed YAML front matter")
}

// parseFrontMatter builds the logical tree of the front matter. Empty front
// matter is an empty object.
func parseFrontMatter(region yamltree.Region) (*adapter.Node, error) {
	var document yaml.Node
	if err := yaml.Unmarshal(region.Data, &document); err != nil {
		return nil, fmt.Errorf("invalid front matter: %w", err)
	}
	if len(document.Content) == 0 {
		return &adapter.Node{Value: map[string]any{}, Fields: map[string]*adapter.Node{}}, nil
	}
	return yamltree.Build(region.Data, document.Content[0], region.StartOffset, false)
}

func nextLineEnd(data []byte, start int) int {
	if index := bytes.IndexByte(data[start:], '\n'); index >= 0 {
		return start + index + 1
	}
	return len(data)
}

// sections returns one node for each heading. The body of a section runs to
// the next heading of the same or a higher level. Headings inside fenced code
// and indented lines do not count. base is the offset of data in the file.
func sections(data []byte, base int) []*adapter.Node {
	type heading struct {
		level     int
		text      string
		lineStart int
		bodyStart int
	}
	var headings []heading
	forEachLine(data, func(line markdownLine) {
		if line.fenced || line.indented {
			return
		}
		trimmed := strings.TrimLeft(line.text, " ")
		level := 0
		for level < len(trimmed) && trimmed[level] == '#' {
			level++
		}
		if level > 0 && level <= 6 && level < len(trimmed) && trimmed[level] == ' ' {
			headings = append(headings, heading{
				level: level, text: headingText(trimmed[level:]), lineStart: line.start, bodyStart: line.end,
			})
		}
	})

	result := make([]*adapter.Node, 0, len(headings))
	for index, current := range headings {
		bodyEnd := len(data)
		for next := index + 1; next < len(headings); next++ {
			if headings[next].level <= current.level {
				bodyEnd = headings[next].lineStart
				break
			}
		}
		result = append(result, &adapter.Node{Fields: map[string]*adapter.Node{
			"heading": {Value: current.text},
			"body":    textNode(data[current.bodyStart:bodyEnd], base+current.bodyStart, base+bodyEnd),
		}})
	}
	return result
}

// markdownLine is one line of a document with its byte range, whether it is
// inside or part of a fenced code block, and whether it is indented code.
type markdownLine struct {
	start, end int
	text       string
	fenced     bool
	indented   bool
}

// forEachLine visits each line of data in order and tracks fenced code
// blocks, so that headings and markers inside code do not count. As in
// CommonMark, a closing fence has nothing but spaces after it, and the info
// string of a backtick fence has no backtick.
func forEachLine(data []byte, visit func(markdownLine)) {
	var fenceCharacter byte
	fenceLength := 0
	for offset := 0; offset < len(data); {
		end := nextLineEnd(data, offset)
		text := strings.TrimRight(string(data[offset:end]), "\r\n")
		trimmed := strings.TrimLeft(text, " ")
		line := markdownLine{start: offset, end: end, text: text, fenced: fenceLength != 0}
		line.indented = len(text)-len(trimmed) > 3 || strings.HasPrefix(trimmed, "\t")
		if !line.indented {
			marker, markerLength := fence(trimmed)
			rest := trimmed[markerLength:]
			switch {
			case fenceLength == 0 && markerLength >= 3 && (marker == '~' || !strings.Contains(rest, "`")):
				fenceCharacter, fenceLength, line.fenced = marker, markerLength, true
			case fenceLength != 0 && marker == fenceCharacter && markerLength >= fenceLength && strings.TrimSpace(rest) == "":
				fenceCharacter, fenceLength = 0, 0
			}
		}
		visit(line)
		offset = end
	}
}

func fence(line string) (byte, int) {
	if line == "" || line[0] != '`' && line[0] != '~' {
		return 0, 0
	}
	length := 1
	for length < len(line) && line[length] == line[0] {
		length++
	}
	return line[0], length
}

// headingText returns the heading without its closing hashes.
func headingText(text string) string {
	text = strings.TrimSpace(text)
	lastSpace := strings.LastIndexByte(text, ' ')
	if lastSpace < 0 || lastSpace == len(text)-1 || strings.Trim(text[lastSpace+1:], "#") != "" {
		return text
	}
	return strings.TrimSpace(text[:lastSpace])
}

// markerRegions returns one node for each region between a begin and an end
// marker. A marker inside fenced or indented code does not count. Nested,
// reversed, and unclosed markers are errors.
func markerRegions(data []byte, base int) (*adapter.Node, error) {
	const beginPrefix = "<!-- launchdarkly:begin "
	const endPrefix = "<!-- launchdarkly:end "
	var items []*adapter.Node
	var err error
	openID, bodyStart := "", 0
	forEachLine(data, func(line markdownLine) {
		text := strings.TrimSpace(line.text)
		switch {
		case err != nil || line.fenced || line.indented:
		case strings.HasPrefix(text, beginPrefix) && strings.HasSuffix(text, "-->"):
			if openID != "" {
				err = fmt.Errorf("nested LaunchDarkly marker %q", openID)
				return
			}
			openID = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(text, beginPrefix), "-->"))
			bodyStart = line.end
		case strings.HasPrefix(text, endPrefix) && strings.HasSuffix(text, "-->"):
			id := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(text, endPrefix), "-->"))
			if openID == "" || id != openID {
				err = fmt.Errorf("LaunchDarkly marker %q has no matching begin marker", id)
				return
			}
			items = append(items, &adapter.Node{Fields: map[string]*adapter.Node{
				"id":   {Value: id},
				"body": textNode(data[bodyStart:line.start], base+bodyStart, base+line.start),
			}})
			openID = ""
		}
	})
	if err != nil {
		return nil, err
	}
	if openID != "" {
		return nil, fmt.Errorf("LaunchDarkly marker %q is not closed", openID)
	}
	return &adapter.Node{Items: items, Collection: true}, nil
}

// textNode is prompt text: normalized line endings and no surrounding white
// space. An update keeps the white space around the text and the line ending
// style of the file.
func textNode(data []byte, start, end int) *adapter.Node {
	lineEnding := "\n"
	if bytes.Contains(data, []byte("\r\n")) {
		lineEnding = "\r\n"
	}
	trimmed := bytes.TrimFunc(data, isSpace)
	prefix := bytes.Clone(data[:bytes.Index(data, trimmed)])
	suffix := bytes.Clone(data[len(prefix)+len(trimmed):])
	return adapter.String(normalize(string(data)), start, end, func(text string) ([]byte, error) {
		text = strings.ReplaceAll(normalize(text), "\n", lineEnding)
		return bytes.Join([][]byte{prefix, []byte(text), suffix}, nil), nil
	})
}

// exactNode is text that keeps every byte, as skill Markdown does. An update
// keeps the final newline of the file.
func exactNode(data []byte, start, end int) *adapter.Node {
	finalNewline := bytes.HasSuffix(data, []byte("\n"))
	return adapter.String(string(data), start, end, func(text string) ([]byte, error) {
		if finalNewline && text != "" && !strings.HasSuffix(text, "\n") {
			text += "\n"
		}
		return []byte(text), nil
	})
}

func normalize(text string) string {
	return syncdomain.NormalizePromptText(text)
}

func isSpace(character rune) bool {
	return character == ' ' || character == '\t' || character == '\r' || character == '\n'
}
