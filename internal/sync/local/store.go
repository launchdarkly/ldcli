package local

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
	"gopkg.in/yaml.v3"
)

var ErrVariationExists = errors.New("variation already exists locally")

type VariationFile struct {
	ProjectKey string
	ConfigKey  string
	Upsert     bool
	Variation  syncdomain.Variation
}

type Store struct {
	root string
}

func NewStore(repoRoot string) Store {
	return Store{root: filepath.Join(repoRoot, syncdomain.RootDir)}
}

func (s Store) Exists() (bool, error) {
	info, err := os.Stat(s.root)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("inspect %s: %w", s.root, err)
	}
	if !info.IsDir() {
		return false, fmt.Errorf("%s exists but is not a directory", s.root)
	}

	return true, nil
}

func (s Store) VariationExists(projectKey, configKey, variationKey string) (bool, error) {
	path, err := s.variationPath(projectKey, configKey, variationKey)
	if err != nil {
		return false, err
	}

	_, err = os.Stat(path)
	switch {
	case err == nil:
		return true, nil
	case errors.Is(err, os.ErrNotExist):
		return false, nil
	default:
		return false, fmt.Errorf("inspect variation %s: %w", variationKey, err)
	}
}

func (s Store) Bootstrap(resources []VariationFile) ([]string, error) {
	if _, err := os.Stat(s.root); err == nil {
		return nil, fmt.Errorf("%s already exists", s.root)
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("inspect %s: %w", s.root, err)
	}

	stage, err := os.MkdirTemp(filepath.Dir(s.root), ".launchdarkly.tmp-")
	if err != nil {
		return nil, fmt.Errorf("create bootstrap staging directory: %w", err)
	}
	defer func() {
		_ = os.RemoveAll(stage)
	}()

	stagedStore := Store{root: stage}
	paths, err := stagedStore.createVariations(resources)
	if err != nil {
		return nil, err
	}
	if err := os.Rename(stage, s.root); err != nil {
		return nil, fmt.Errorf("finish bootstrap: %w", err)
	}

	return paths, nil
}

func (s Store) Add(resources []VariationFile) ([]string, error) {
	if err := os.MkdirAll(s.root, 0o755); err != nil {
		return nil, fmt.Errorf("create %s: %w", s.root, err)
	}

	return s.createVariations(resources)
}

func (s Store) createVariations(resources []VariationFile) ([]string, error) {
	type pendingFile struct {
		absolute string
		relative string
		data     []byte
	}

	pending := make([]pendingFile, 0, len(resources))
	seen := make(map[string]struct{}, len(resources))
	for _, resource := range resources {
		path, err := s.variationPath(
			resource.ProjectKey,
			resource.ConfigKey,
			resource.Variation.Key,
		)
		if err != nil {
			return nil, err
		}
		if _, ok := seen[path]; ok {
			return nil, fmt.Errorf("variation %q was selected more than once", resource.Variation.Key)
		}
		seen[path] = struct{}{}

		data, err := marshalVariationFile(resource)
		if err != nil {
			return nil, err
		}
		pending = append(pending, pendingFile{
			absolute: path,
			relative: filepath.ToSlash(strings.TrimPrefix(path, s.root+string(filepath.Separator))),
			data:     data,
		})
	}

	var created []string
	for _, file := range pending {
		if err := createFile(file.absolute, file.data); err != nil {
			for index := len(created) - 1; index >= 0; index-- {
				_ = os.Remove(filepath.Join(s.root, filepath.FromSlash(created[index])))
			}
			return nil, err
		}
		created = append(created, file.relative)
	}

	return created, nil
}

func (s Store) variationPath(projectKey, configKey, variationKey string) (string, error) {
	segments := []struct {
		name  string
		value string
	}{
		{name: "project key", value: projectKey},
		{name: "config key", value: configKey},
		{name: "variation key", value: variationKey},
	}
	for _, segment := range segments {
		if err := validatePathSegment(segment.value); err != nil {
			return "", fmt.Errorf("invalid %s %q: %w", segment.name, segment.value, err)
		}
	}

	return filepath.Join(
		s.root,
		projectKey,
		configsDir,
		configKey,
		variationKey+variationFileSuffix,
	), nil
}

func validatePathSegment(value string) error {
	if value == "" {
		return errors.New("must not be empty")
	}
	if value == "." || value == ".." || strings.ContainsAny(value, `/\`) {
		return errors.New("must be a single path segment")
	}
	if strings.IndexByte(value, 0) >= 0 {
		return errors.New("must not contain a null byte")
	}

	return nil
}

func marshalVariationFile(resource VariationFile) ([]byte, error) {
	if !resource.Variation.Mode.Valid() {
		return nil, fmt.Errorf(
			"variation %q has unsupported mode %q",
			resource.Variation.Key,
			resource.Variation.Mode,
		)
	}
	switch resource.Variation.Mode {
	case syncdomain.VariationModeAgent:
		if len(resource.Variation.Messages) != 0 {
			return nil, fmt.Errorf("agent variation %q cannot contain messages", resource.Variation.Key)
		}
	case syncdomain.VariationModeCompletion:
		if resource.Variation.Instructions != "" {
			return nil, fmt.Errorf("completion variation %q cannot contain instructions", resource.Variation.Key)
		}
	}

	front, err := yaml.Marshal(variationFrontMatter{
		FormatVersion: 1,
		Upsert:        resource.Upsert,
		Variation:     resource.Variation,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal variation %q: %w", resource.Variation.Key, err)
	}

	var file bytes.Buffer
	file.WriteString("---\n")
	file.Write(front)
	file.WriteString("---\n")
	if resource.Variation.Mode == syncdomain.VariationModeAgent {
		if instructions := strings.TrimSpace(resource.Variation.Instructions); instructions != "" {
			fmt.Fprintf(&file, "\n%s\n", instructions)
		}
		return file.Bytes(), nil
	}
	for _, message := range resource.Variation.Messages {
		if !validMessageRole(message.Role) {
			return nil, fmt.Errorf(
				"variation %q has unsupported message role %q",
				resource.Variation.Key,
				message.Role,
			)
		}
		fmt.Fprintf(
			&file,
			"\n<%s>\n%s\n</%s>\n",
			message.Role,
			strings.TrimSpace(message.Content),
			message.Role,
		)
	}

	return file.Bytes(), nil
}

func validMessageRole(role string) bool {
	return role == "system" || role == "user" || role == "assistant"
}

func createFile(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create variation directory: %w", err)
	}

	temp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp-")
	if err != nil {
		return fmt.Errorf("stage variation %s: %w", filepath.Base(path), err)
	}
	tempPath := temp.Name()
	defer func() {
		_ = os.Remove(tempPath)
	}()

	if err := temp.Chmod(0o644); err != nil {
		_ = temp.Close()
		return fmt.Errorf("set variation permissions: %w", err)
	}
	if _, err := temp.Write(data); err != nil {
		_ = temp.Close()
		return fmt.Errorf("write variation %s: %w", filepath.Base(path), err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("close variation %s: %w", filepath.Base(path), err)
	}
	if err := os.Link(tempPath, path); err != nil {
		if errors.Is(err, os.ErrExist) {
			return fmt.Errorf("%w: %s", ErrVariationExists, path)
		}
		return fmt.Errorf("create variation %s: %w", filepath.Base(path), err)
	}

	return nil
}
