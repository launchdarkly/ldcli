package local

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"path"
	"slices"
	"strings"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
	"gopkg.in/yaml.v3"
)

const (
	toolsDir        = "tools"
	skillsDir       = "skills"
	toolFileSuffix  = ".json"
	skillFileSuffix = ".md"
)

type toolFile struct {
	FormatVersion int  `json:"formatVersion"`
	Upsert        bool `json:"upsert,omitempty"`
	syncdomain.Tool
}

type skillFrontMatter struct {
	Key         string  `yaml:"key"`
	Description *string `yaml:"description"`
}

// readTool loads and validates the deterministic local file for one tool key.
func readTool(fsys fs.FS, projectKey, key string) (syncdomain.Attachment, error) {
	if err := validatePathSegment(key); err != nil {
		return syncdomain.Attachment{}, fmt.Errorf("invalid tool key %q: %w", key, err)
	}

	relativePath, _ := attachmentPath(projectKey, syncdomain.AttachmentTool, key)
	filePath := path.Join(syncdomain.RootDir, relativePath)
	data, err := readAttachmentFile(fsys, filePath)
	if err != nil {
		return syncdomain.Attachment{}, fmt.Errorf("read tool %q: %w", key, err)
	}

	var file toolFile
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&file); err != nil {
		return syncdomain.Attachment{}, fmt.Errorf("parse tool %q: %w", key, err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return syncdomain.Attachment{}, fmt.Errorf("parse tool %q: multiple JSON values are not supported", key)
		}
		return syncdomain.Attachment{}, fmt.Errorf("parse tool %q: %w", key, err)
	}
	switch {
	case file.FormatVersion != 1:
		return syncdomain.Attachment{}, fmt.Errorf("tool %q has unsupported formatVersion %d", key, file.FormatVersion)
	case file.Key != key:
		return syncdomain.Attachment{}, fmt.Errorf("tool key %q does not match filename %q", file.Key, key)
	case file.Schema == nil:
		return syncdomain.Attachment{}, fmt.Errorf("tool %q schema is required", key)
	}
	return syncdomain.Attachment{
		Kind: syncdomain.AttachmentTool, Upsert: file.Upsert, Tool: &file.Tool,
	}, nil
}

// readSkill loads one Markdown skill and its editable description. The filename
// remains authoritative for the read-only key shown in front matter.
func readSkill(fsys fs.FS, projectKey, key string) (syncdomain.Attachment, error) {
	if err := validatePathSegment(key); err != nil {
		return syncdomain.Attachment{}, fmt.Errorf("invalid skill key %q: %w", key, err)
	}

	relativePath, _ := attachmentPath(projectKey, syncdomain.AttachmentSkill, key)
	filePath := path.Join(syncdomain.RootDir, relativePath)
	data, err := readAttachmentFile(fsys, filePath)
	if err != nil {
		return syncdomain.Attachment{}, fmt.Errorf("read skill %q: %w", key, err)
	}

	var metadata skillFrontMatter
	body, err := parseYAMLFrontMatter(data, &metadata)
	if err != nil {
		return syncdomain.Attachment{}, fmt.Errorf("parse skill %q: %w", key, err)
	}
	body = trimSkillBodySeparator(body)
	switch {
	case metadata.Key != key:
		return syncdomain.Attachment{}, fmt.Errorf("skill key %q does not match filename %q", metadata.Key, key)
	case metadata.Description == nil:
		return syncdomain.Attachment{}, fmt.Errorf("skill %q description is required in front matter", key)
	case strings.TrimSpace(string(body)) == "":
		return syncdomain.Attachment{}, fmt.Errorf("skill %q markdown is required", key)
	}

	skill := syncdomain.Skill{Key: key, Description: *metadata.Description, Markdown: string(body)}
	return syncdomain.Attachment{Kind: syncdomain.AttachmentSkill, Skill: &skill}, nil
}

func readAttachment(fsys fs.FS, projectKey string, kind syncdomain.AttachmentKind, key string) (syncdomain.Attachment, error) {
	switch kind {
	case syncdomain.AttachmentTool:
		return readTool(fsys, projectKey, key)
	case syncdomain.AttachmentSkill:
		return readSkill(fsys, projectKey, key)
	default:
		return syncdomain.Attachment{}, fmt.Errorf("unsupported attachment kind %q", kind)
	}
}

func trimSkillBodySeparator(body []byte) []byte {
	if bytes.HasPrefix(body, []byte("\r\n")) {
		return body[2:]
	}
	return bytes.TrimPrefix(body, []byte("\n"))
}

// readAttachmentFile rejects links and non-regular files before reading
// managed dependency content. This keeps all reads inside the workspace tree.
func readAttachmentFile(fsys fs.FS, filePath string) ([]byte, error) {
	info, err := fs.Stat(fsys, filePath)
	if err != nil {
		return nil, err
	}
	if info.Mode()&fs.ModeSymlink != 0 {
		return nil, fmt.Errorf("symbolic links are not supported")
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("not a regular file")
	}
	return fs.ReadFile(fsys, filePath)
}

// renderAttachmentFiles renders each shared dependency once, rejecting two
// variations that claim different local content for the same key.
func renderAttachmentFiles(resources []VariationFile) ([]RenderedVariationFile, error) {
	files := make(map[string][]byte)

	for _, resource := range resources {
		if err := validatePathSegment(resource.ProjectKey); err != nil {
			return nil, fmt.Errorf("invalid project key %q: %w", resource.ProjectKey, err)
		}
		for _, attachment := range resource.Variation.Attachments {
			var content []byte
			var err error

			switch attachment.Kind {
			case syncdomain.AttachmentTool:
				content, err = renderTool(attachment)
			case syncdomain.AttachmentSkill:
				if attachment.Skill == nil {
					return nil, fmt.Errorf("skill content is required")
				}
				content, err = renderSkill(*attachment.Skill)
			default:
				err = fmt.Errorf("unsupported attachment kind %q", attachment.Kind)
			}
			if err != nil {
				return nil, err
			}
			filePath, err := attachmentPath(resource.ProjectKey, attachment.Kind, attachment.Key())
			if err != nil {
				return nil, err
			}
			if err := addAttachmentFile(files, filePath, content); err != nil {
				return nil, err
			}
		}
	}

	paths := make([]string, 0, len(files))
	for filePath := range files {
		paths = append(paths, filePath)
	}
	slices.Sort(paths)

	rendered := make([]RenderedVariationFile, 0, len(paths))
	for _, filePath := range paths {
		rendered = append(rendered, RenderedVariationFile{Path: filePath, Content: files[filePath]})
	}
	return rendered, nil
}

func renderTool(attachment syncdomain.Attachment) ([]byte, error) {
	tool := attachment.Tool
	if tool == nil {
		return nil, fmt.Errorf("tool content is required")
	}
	if err := validatePathSegment(tool.Key); err != nil {
		return nil, fmt.Errorf("invalid tool key %q: %w", tool.Key, err)
	}
	if tool.Schema == nil {
		return nil, fmt.Errorf("tool %q schema is required", tool.Key)
	}

	return renderToolFile(toolFile{FormatVersion: 1, Upsert: attachment.Upsert, Tool: *tool})
}

func renderToolFile(file toolFile) ([]byte, error) {
	var content bytes.Buffer
	encoder := json.NewEncoder(&content)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(file); err != nil {
		return nil, fmt.Errorf("marshal tool %q: %w", file.Key, err)
	}
	return content.Bytes(), nil
}

func renderSkill(skill syncdomain.Skill) ([]byte, error) {
	if err := validatePathSegment(skill.Key); err != nil {
		return nil, fmt.Errorf("invalid skill key %q: %w", skill.Key, err)
	}
	if strings.TrimSpace(skill.Markdown) == "" {
		return nil, fmt.Errorf("skill %q markdown is required", skill.Key)
	}

	var metadata bytes.Buffer
	encoder := yaml.NewEncoder(&metadata)
	encoder.SetIndent(2)
	if err := encoder.Encode(skillFrontMatter{Key: skill.Key, Description: &skill.Description}); err != nil {
		return nil, fmt.Errorf("marshal skill %q metadata: %w", skill.Key, err)
	}

	var file bytes.Buffer
	file.WriteString("---\n")
	file.Write(metadata.Bytes())
	file.WriteString("---\n\n")
	file.WriteString(skill.Markdown)
	return file.Bytes(), nil
}

func addAttachmentFile(files map[string][]byte, filePath string, content []byte) error {
	if existing, ok := files[filePath]; ok && !bytes.Equal(existing, content) {
		return fmt.Errorf("attachment %q has conflicting local definitions", filePath)
	}
	files[filePath] = content
	return nil
}

func preserveToolUpsert(filePath string, original, replacement []byte) ([]byte, error) {
	if !strings.HasSuffix(filePath, toolFileSuffix) {
		return replacement, nil
	}
	var local, updated toolFile
	if err := json.Unmarshal(original, &local); err != nil {
		return nil, fmt.Errorf("parse existing tool %q: %w", filePath, err)
	}
	if !local.Upsert {
		return replacement, nil
	}
	if err := json.Unmarshal(replacement, &updated); err != nil {
		return nil, fmt.Errorf("parse replacement tool %q: %w", filePath, err)
	}
	updated.Upsert = true
	return renderToolFile(updated)
}

// AttachVariation commits dependency files and their consuming variation in
// the same staged transaction.
func (store Store) AttachVariation(projectKey, configKey string, variation syncdomain.Variation) error {
	_, err := store.ReplaceVariations([]VariationReplacement{{
		ProjectKey: projectKey,
		ConfigKey:  configKey,
		Variation:  variation,
	}})
	return err
}

func attachmentPath(projectKey string, kind syncdomain.AttachmentKind, key string) (string, error) {
	if err := validatePathSegment(projectKey); err != nil {
		return "", fmt.Errorf("invalid project key %q: %w", projectKey, err)
	}
	if err := validatePathSegment(key); err != nil {
		return "", fmt.Errorf("invalid %s key %q: %w", kind, key, err)
	}
	dir, suffix, err := attachmentLayout(kind)
	if err != nil {
		return "", err
	}
	return path.Join(projectKey, dir, key+suffix), nil
}

func attachmentLayout(kind syncdomain.AttachmentKind) (string, string, error) {
	switch kind {
	case syncdomain.AttachmentTool:
		return toolsDir, toolFileSuffix, nil
	case syncdomain.AttachmentSkill:
		return skillsDir, skillFileSuffix, nil
	default:
		return "", "", fmt.Errorf("unsupported attachment kind %q", kind)
	}
}
