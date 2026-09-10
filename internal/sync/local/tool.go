package local

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"regexp"
	"strconv"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
)

const toolsDir = "tools"

var toolFileName = regexp.MustCompile(`^([^/]+)\.v(\d+)\.json$`)

type toolParser struct{}

func (toolParser) dir() string {
	return toolsDir
}

func (toolParser) accept(relPath string) bool {
	return toolFileName.MatchString(relPath)
}

type toolFile struct {
	Key     string `json:"key"`
	Version int    `json:"version"`
	Schema  any    `json:"schema"`
}

func (toolParser) parse(file file) (syncdomain.SyncedResource, error) {
	var parsed toolFile
	decoder := json.NewDecoder(bytes.NewReader(file.Data))
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(&parsed); err != nil {
		return syncdomain.SyncedResource{}, fmt.Errorf("invalid tool file: %w", err)
	}

	stemKey, stemVersion, err := parseToolFileName(path.Base(file.RelPath))
	if err != nil {
		return syncdomain.SyncedResource{}, err
	}

	switch {
	case parsed.Key == "":
		return syncdomain.SyncedResource{}, errors.New("key is required")
	case parsed.Version == 0:
		return syncdomain.SyncedResource{}, errors.New("version is required")
	case parsed.Key != stemKey:
		return syncdomain.SyncedResource{}, fmt.Errorf("key %q does not match filename %q", parsed.Key, stemKey)
	case parsed.Version != stemVersion:
		return syncdomain.SyncedResource{}, fmt.Errorf(
			"version %d does not match filename v%d",
			parsed.Version,
			stemVersion,
		)
	case parsed.Schema == nil:
		return syncdomain.SyncedResource{}, errors.New("schema is required")
	}

	payload, err := marshalPayload(toolFile{
		Key:     parsed.Key,
		Version: parsed.Version,
		Schema:  parsed.Schema,
	})
	if err != nil {
		return syncdomain.SyncedResource{}, err
	}

	return syncdomain.SyncedResource{
		Kind:        syncdomain.KindTool,
		ProjectKey:  file.ProjectKey,
		LookupKey:   fmt.Sprintf("%s/%d", parsed.Key, parsed.Version),
		Payload:     payload,
		Fingerprint: syncdomain.Hash(payload),
	}, nil
}

func parseToolFileName(name string) (string, int, error) {
	matches := toolFileName.FindStringSubmatch(name)
	if matches == nil {
		return "", 0, fmt.Errorf("invalid tool filename %q", name)
	}

	version, err := strconv.Atoi(matches[2])
	if err != nil {
		return "", 0, fmt.Errorf("invalid tool filename %q", name)
	}

	return matches[1], version, nil
}
