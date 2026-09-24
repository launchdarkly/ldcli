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

// RenderVariations validates and renders variation files without writing them.
func (store Store) RenderVariations(resources []VariationFile) ([]RenderedVariationFile, error) {
	rendered := make([]RenderedVariationFile, 0, len(resources))
	seenPaths := make(map[string]struct{}, len(resources))

	for _, resource := range resources {
		absolutePath, err := store.variationPath(resource.ProjectKey, resource.ConfigKey, resource.Variation.Key)
		if err != nil {
			return nil, err
		}
		if _, duplicate := seenPaths[absolutePath]; duplicate {
			return nil, fmt.Errorf("variation %q was selected more than once", resource.Variation.Key)
		}
		seenPaths[absolutePath] = struct{}{}

		content, err := marshalVariationFile(resource)
		if err != nil {
			return nil, err
		}
		rendered = append(rendered, RenderedVariationFile{
			Path:    filepath.ToSlash(strings.TrimPrefix(absolutePath, store.root+string(filepath.Separator))),
			Content: content,
		})
	}
	return rendered, nil
}

// createVariations writes a prevalidated batch and rolls back files created
// before the first failure.
func (store Store) createVariations(resources []VariationFile) ([]string, error) {
	// Render the complete batch first so validation failures cannot leave a
	// partially created workspace.
	rendered, err := store.RenderVariations(resources)
	if err != nil {
		return nil, err
	}

	var createdPaths []string
	for _, file := range rendered {
		absolutePath := filepath.Join(store.root, filepath.FromSlash(file.Path))
		if err := createFile(absolutePath, file.Content); err != nil {
			// Creates are independent filesystem operations. Remove earlier
			// files in reverse order to recover the pre-call state.
			for index := len(createdPaths) - 1; index >= 0; index-- {
				_ = os.Remove(filepath.Join(store.root, filepath.FromSlash(createdPaths[index])))
			}
			return nil, errors.Join(err, store.RemoveEmptyDirectories())
		}
		createdPaths = append(createdPaths, file.Path)
	}
	return createdPaths, nil
}

// marshalVariationFile converts the canonical variation into front matter and
// the mode-specific prompt body used by local wrapper files.
func marshalVariationFile(resource VariationFile) ([]byte, error) {
	if !resource.Variation.Mode.Valid() {
		return nil, fmt.Errorf("variation %q has unsupported mode %q", resource.Variation.Key, resource.Variation.Mode)
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

	var frontMatter bytes.Buffer
	encoder := yaml.NewEncoder(&frontMatter)
	encoder.SetIndent(2)
	err := encoder.Encode(variationFrontMatter{
		FormatVersion: 1,
		Upsert:        resource.Upsert,
		Ref:           resource.Ref,
		Variation:     resource.Variation,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal variation %q: %w", resource.Variation.Key, err)
	}
	if err := encoder.Close(); err != nil {
		return nil, fmt.Errorf("marshal variation %q: %w", resource.Variation.Key, err)
	}

	var file bytes.Buffer
	file.WriteString("---\n")
	file.Write(frontMatter.Bytes())
	file.WriteString("---\n")
	if resource.Ref != nil {
		// Referenced wrappers contain metadata only. Keeping the body empty
		// prevents two local sources from competing for the same content.
		if err := validateReference(*resource.Ref); err != nil {
			return nil, err
		}
		return file.Bytes(), nil
	}
	if resource.Variation.Mode == syncdomain.VariationModeAgent {
		if instructions := strings.TrimSpace(resource.Variation.Instructions); instructions != "" {
			_, _ = fmt.Fprintf(&file, "\n%s\n", instructions)
		}
		return file.Bytes(), nil
	}

	for _, message := range resource.Variation.Messages {
		if !validMessageRole(message.Role) {
			return nil, fmt.Errorf("variation %q has unsupported message role %q", resource.Variation.Key, message.Role)
		}
		content := escapeMessageContent(strings.TrimSpace(message.Content), message.Role)
		_, _ = fmt.Fprintf(&file, "\n<%s>\n%s\n</%s>\n", message.Role, content, message.Role)
	}
	return file.Bytes(), nil
}

// validMessageRole reports whether the wrapper syntax supports a role.
func validMessageRole(role string) bool {
	return role == "system" || role == "user" || role == "assistant"
}

// escapeMessageContent protects literal role delimiters and backslashes so a
// rendered completion prompt can always be parsed back losslessly.
func escapeMessageContent(content, role string) string {
	content = strings.ReplaceAll(content, `\`, `\\`)
	content = strings.ReplaceAll(content, "<"+role+">", `<\`+role+">")
	return strings.ReplaceAll(content, "</"+role+">", `<\/`+role+">")
}

// createFile stages content beside its destination and hard-links it into
// place, which guarantees an existing wrapper is never overwritten.
func createFile(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create variation directory: %w", err)
	}

	temp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp-")
	if err != nil {
		return fmt.Errorf("stage variation %s: %w", filepath.Base(path), err)
	}
	tempPath := temp.Name()
	defer func() { _ = os.Remove(tempPath) }()

	// The temporary file is fully written before publication. A hard link is
	// then an atomic create-if-absent operation on the same filesystem.
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
