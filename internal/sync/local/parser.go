package local

import (
	"bytes"
	"encoding/json"
	"fmt"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
)

type file struct {
	ProjectKey string
	RelPath    string
	Data       []byte
}

type parser interface {
	dir() string
	accept(relPath string) bool
	parse(file file) (syncdomain.SyncedResource, error)
}

func defaultParsers() []parser {
	return []parser{
		variationParser{},
		toolParser{},
	}
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
