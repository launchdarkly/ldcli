package structured

import (
	"bytes"
	"io"
	"unicode/utf8"

	"gopkg.in/yaml.v3"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
	"github.com/launchdarkly/ldcli/internal/sync/adapter"
	"github.com/launchdarkly/ldcli/internal/sync/adapter/yamltree"
)

type yamlAdapter struct{}

func (yamlAdapter) Format() string   { return "yaml" }
func (yamlAdapter) Behavior() string { return "yaml/1" }

func (yamlAdapter) Inspect(bundle adapter.SourceBundle) (*adapter.Node, error) {
	return parseYAMLDocument(bundle.Primary)
}

func (yamlAdapter) Defaults(syncdomain.Kind) (string, map[string]string) { return "$", nil }

func init() { adapter.MustRegister(yamlAdapter{}) }

func parseYAMLDocument(document adapter.Document) (*adapter.Node, error) {
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
	// The source has several documents when any later document has content.
	// A trailing "---" decodes as an empty document, which is not content.
	multiple := false
	for !multiple {
		var extra yaml.Node
		extraErr := decoder.Decode(&extra)
		if extraErr == io.EOF {
			break
		}
		if extraErr != nil {
			return nil, syncdomain.WrapProblem(syncdomain.NewProblem(
				syncdomain.ErrorInvalidSource, "YAML source is invalid", "Fix the YAML syntax.",
			).WithFile(document.Path, 0, 0), extraErr)
		}
		multiple = len(extra.Content) != 0 &&
			!(extra.Content[0].Kind == yaml.ScalarNode && extra.Content[0].Tag == "!!null" && extra.Content[0].Value == "")
	}
	if len(first.Content) == 0 {
		return nil, syncdomain.NewProblem(
			syncdomain.ErrorInvalidSource, "YAML document is empty", "Add one YAML value.",
		)
	}
	root, err := yamltree.Build(data, first.Content[0], offset, false)
	if err != nil {
		return nil, err
	}
	if multiple {
		root.MarkReadOnly("multi-document YAML is read-only")
	}
	return root, nil
}
