package sync

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"regexp"
	"strconv"
)

const toolsDir = "tools"

var toolFileName = regexp.MustCompile(`^([^/]+)\.v(\d+)\.json$`)

type toolParser struct{}

func (toolParser) Dir() string {
	return toolsDir
}

func (toolParser) Accept(relPath string) bool {
	return toolFileName.MatchString(relPath)
}

type toolFile struct {
	Key     string `json:"key"`
	Version int    `json:"version"`
	Schema  any    `json:"schema"`
}

func (toolParser) Parse(file File) (SyncedResource, error) {
	var parsed toolFile
	dec := json.NewDecoder(bytes.NewReader(file.Data))
	dec.DisallowUnknownFields()

	if err := dec.Decode(&parsed); err != nil {
		return SyncedResource{}, fmt.Errorf("invalid tool file: %w", err)
	}

	stemKey, stemVersion, err := parseToolFileName(path.Base(file.RelPath))
	if err != nil {
		return SyncedResource{}, err
	}

	switch {
	case parsed.Key == "":
		return SyncedResource{}, errors.New("key is required")
	case parsed.Version == 0:
		return SyncedResource{}, errors.New("version is required")
	case parsed.Key != stemKey:
		return SyncedResource{}, fmt.Errorf("key %q does not match filename %q", parsed.Key, stemKey)
	case parsed.Version != stemVersion:
		return SyncedResource{}, fmt.Errorf("version %d does not match filename v%d", parsed.Version, stemVersion)
	case parsed.Schema == nil:
		return SyncedResource{}, errors.New("schema is required")
	}

	payload, err := marshalPayload(toolFile{
		Key:     parsed.Key,
		Version: parsed.Version,
		Schema:  parsed.Schema,
	})
	if err != nil {
		return SyncedResource{}, err
	}

	return SyncedResource{
		Kind:        KindTool,
		ProjectKey:  file.ProjectKey,
		LookupKey:   fmt.Sprintf("%s/%d", parsed.Key, parsed.Version),
		Payload:     payload,
		Fingerprint: Hash(payload),
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
