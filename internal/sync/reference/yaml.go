package reference

import (
	"bytes"
	"fmt"
	"io"
	"strconv"
	"strings"
	"unicode/utf8"

	"gopkg.in/yaml.v3"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
)

type yamlAdapter struct{}

func (yamlAdapter) Format() string { return "yaml" }
func (yamlAdapter) Inspect(bundle SourceBundle) (Inspection, error) {
	root, err := parseYAMLDocument(bundle.Primary)
	return Inspection{root: root}, err
}
func (yamlAdapter) Defaults(syncdomain.Kind) (string, map[string]string) { return "$", nil }

func init() { mustRegister(yamlAdapter{}) }

func parseYAMLDocument(document Document) (*sourceNode, error) {
	if !utf8.Valid(document.Data) {
		return nil, syncdomain.NewProblem(
			syncdomain.ErrorInvalidSource,
			"YAML source is not valid UTF-8",
			"Save the file as UTF-8.",
		).WithFile(document.Path, 0, 0)
	}
	data := bytes.TrimPrefix(document.Data, []byte("\ufeff"))
	offset := len(document.Data) - len(data)
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	var first yaml.Node
	if err := decoder.Decode(&first); err != nil {
		return nil, syncdomain.WrapProblem(syncdomain.NewProblem(
			syncdomain.ErrorInvalidSource, "YAML source is invalid", "Fix the YAML syntax.",
		).WithFile(document.Path, 0, 0), err)
	}
	var extra yaml.Node
	extraErr := decoder.Decode(&extra)
	multiple := extraErr == nil
	if extraErr != nil && extraErr != io.EOF {
		return nil, syncdomain.WrapProblem(syncdomain.NewProblem(
			syncdomain.ErrorInvalidSource, "YAML source is invalid", "Fix the YAML syntax.",
		).WithFile(document.Path, 0, 0), extraErr)
	}
	if len(first.Content) == 0 {
		return nil, syncdomain.NewProblem(
			syncdomain.ErrorInvalidSource, "YAML document is empty", "Add one YAML value.",
		)
	}
	root, err := yamlSourceNode(data, first.Content[0], offset)
	if err != nil {
		return nil, err
	}
	if multiple {
		markReadOnly(root, "multi-document YAML is read-only")
	}
	return root, nil
}

func yamlSourceNode(data []byte, node *yaml.Node, offset int) (*sourceNode, error) {
	switch node.Kind {
	case yaml.MappingNode:
		return yamlMappingNode(data, node, offset)
	case yaml.SequenceNode:
		return yamlSequenceNode(data, node, offset)
	case yaml.ScalarNode:
		return yamlValueNode(data, node, offset)
	case yaml.AliasNode:
		value, err := yamlSourceNode(data, node.Alias, offset)
		if value != nil {
			markReadOnly(value, "YAML aliases are read-only")
		}
		return value, err
	default:
		return nil, syncdomain.NewProblem(
			syncdomain.ErrorInvalidSource, "YAML node is not supported", "Use mappings, lists, or scalar values.",
		)
	}
}

func yamlMappingNode(data []byte, node *yaml.Node, offset int) (*sourceNode, error) {
	fields := make(map[string]*sourceNode, len(node.Content)/2)
	value := make(map[string]any, len(node.Content)/2)
	unsafe := node.Anchor != "" || customYAMLTag(node.Tag)
	reason := "YAML anchors and tags are read-only"
	for index := 0; index+1 < len(node.Content); index += 2 {
		keyNode, valueNode := node.Content[index], node.Content[index+1]
		if keyNode.Kind != yaml.ScalarNode || keyNode.Tag != "!!str" && keyNode.Tag != "!!merge" {
			return nil, syncdomain.NewProblem(
				syncdomain.ErrorInvalidSource,
				"YAML mapping key is not a string",
				"Use direct string keys.",
			)
		}
		key := keyNode.Value
		if _, duplicate := fields[key]; duplicate {
			return nil, syncdomain.NewProblem(
				syncdomain.ErrorDuplicateKey,
				fmt.Sprintf("YAML mapping contains duplicate key %q", key),
				"Remove the duplicate key.",
			)
		}
		child, err := yamlSourceNode(data, valueNode, offset)
		if err != nil {
			return nil, err
		}
		fields[key], value[key] = child, child.value
		if key == "<<" {
			unsafe, reason = true, "YAML merge keys are read-only"
			for mergedKey, mergedValue := range child.fields {
				if _, exists := fields[mergedKey]; !exists {
					fields[mergedKey] = mergedValue
					value[mergedKey] = mergedValue.value
				}
			}
		}
	}
	result := &sourceNode{
		value: value, fields: fields, capability: CapabilityReadOnly,
		reason: "YAML structural changes are read-only",
	}
	if unsafe {
		markReadOnly(result, reason)
	}
	return result, nil
}

func yamlSequenceNode(data []byte, node *yaml.Node, offset int) (*sourceNode, error) {
	items := make([]*sourceNode, 0, len(node.Content))
	values := make([]any, 0, len(node.Content))
	for _, childNode := range node.Content {
		child, err := yamlSourceNode(data, childNode, offset)
		if err != nil {
			return nil, err
		}
		items, values = append(items, child), append(values, child.value)
	}
	result := &sourceNode{
		value: values, items: items, collection: true, capability: CapabilityReadOnly,
		reason: "YAML structural changes are read-only",
	}
	if node.Anchor != "" || customYAMLTag(node.Tag) {
		markReadOnly(result, "YAML anchors and tags are read-only")
	}
	return result, nil
}

func yamlValueNode(data []byte, node *yaml.Node, offset int) (*sourceNode, error) {
	region := frontMatterRegion{data: data, startOffset: offset}
	start, end, raw, err := yamlScalarSpan(region, node)
	if err != nil {
		return nil, err
	}
	readOnly := node.Anchor != "" || customYAMLTag(node.Tag) ||
		node.Style == yaml.LiteralStyle || node.Style == yaml.FoldedStyle
	reason := "YAML anchors, tags, and block scalars are read-only"
	var result *sourceNode
	switch node.Tag {
	case "!!str":
		result = stringNode(node.Value, start, end, func(text string) ([]byte, error) {
			return encodeYAMLString(text, raw)
		})
	case "!!bool":
		value, err := strconv.ParseBool(node.Value)
		if err != nil {
			return nil, err
		}
		result = &sourceNode{
			value: value, start: start, end: end, capability: CapabilityDirect,
			encode: func(next any) ([]byte, error) {
				value, ok := next.(bool)
				if !ok {
					return nil, mapTypeError("source", "a boolean")
				}
				return []byte(strconv.FormatBool(value)), nil
			},
		}
	case "!!int", "!!float":
		number, err := strconv.ParseFloat(strings.ReplaceAll(node.Value, "_", ""), 64)
		if err != nil {
			return nil, err
		}
		result = numberNode(number, start, end, func(next float64) ([]byte, error) {
			return []byte(strconv.FormatFloat(next, 'g', -1, 64)), nil
		})
	default:
		result = readOnlyNode(start, end, "YAML scalar type is read-only")
		result.value = node.Value
	}
	if readOnly {
		result.capability, result.reason, result.encode = CapabilityReadOnly, reason, nil
	}
	return result, nil
}

func customYAMLTag(tag string) bool {
	return tag != "" && !strings.HasPrefix(tag, "!!")
}

func markReadOnly(node *sourceNode, reason string) {
	if node == nil {
		return
	}
	node.capability, node.reason, node.encode = CapabilityReadOnly, reason, nil
	for _, child := range node.fields {
		markReadOnly(child, reason)
	}
	for _, child := range node.items {
		markReadOnly(child, reason)
	}
}
