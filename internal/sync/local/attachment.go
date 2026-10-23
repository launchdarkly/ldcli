package local

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"path"
	"path/filepath"
	"slices"
	"strings"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
)

// A tool file is JSON. A skill file is Markdown with YAML front matter that
// has the key and the description. Both can set upsert, and a skill file can
// set the name that sync sends when it creates the skill. Neither is part of
// the fingerprint.
const toolFormatVersion = 1

type toolFile struct {
	FormatVersion int  `json:"formatVersion"`
	Upsert        bool `json:"upsert,omitempty"`
	syncdomain.Tool
}

type skillFrontMatter struct {
	Key         string  `yaml:"key"`
	Description *string `yaml:"description"`
	Name        string  `yaml:"name,omitempty"`
	Upsert      bool    `yaml:"upsert,omitempty"`
}

// CompileAttachments reads each tool and skill file that include accepts. It
// does not read the other files, so an invalid file outside include does not
// stop the read. Like CompileWorkspace, it rejects a symbolic link in a
// managed path.
func CompileAttachments(repositoryRoot string, include func(syncdomain.ResourceID) bool) ([]syncdomain.SyncedAttachment, error) {
	root, err := filepath.EvalSymlinks(repositoryRoot)
	if err != nil {
		return nil, fmt.Errorf("resolve repository root: %w", err)
	}
	files, err := SourceFiles(root)
	if err != nil {
		return nil, err
	}

	fsys := workspaceFS{root: root}
	var attachments []syncdomain.SyncedAttachment
	for _, file := range files {
		id, ok := ParseManagedPath(file)
		if !ok || id.Kind == syncdomain.KindVariation || !include(id) {
			continue
		}
		attachment, err := readAttachment(fsys, id.ProjectKey, syncdomain.AttachmentKind(id.Kind), id.LookupKey)
		if err != nil {
			return nil, ParseError{Path: file, Err: err}
		}
		attachments = append(attachments, syncdomain.SyncedAttachment{ProjectKey: id.ProjectKey, Attachment: attachment})
	}
	return attachments, nil
}

// readAttachment reads the file of one tool or skill.
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

func readTool(fsys fs.FS, projectKey, key string) (syncdomain.Attachment, error) {
	data, err := readAttachmentFile(fsys, projectKey, syncdomain.AttachmentTool, key)
	if err != nil {
		return syncdomain.Attachment{}, err
	}

	var file toolFile
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&file); err != nil {
		return syncdomain.Attachment{}, fmt.Errorf("parse tool %q: %w", key, err)
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		if err == nil {
			err = errors.New("multiple JSON values are not supported")
		}
		return syncdomain.Attachment{}, fmt.Errorf("parse tool %q: %w", key, err)
	}

	switch {
	case file.FormatVersion != toolFormatVersion:
		return syncdomain.Attachment{}, fmt.Errorf("tool %q has unsupported formatVersion %d", key, file.FormatVersion)
	case file.Key != key:
		return syncdomain.Attachment{}, fmt.Errorf("tool key %q does not match filename %q", file.Key, key)
	case file.Schema == nil:
		return syncdomain.Attachment{}, fmt.Errorf("tool %q schema is required", key)
	}
	return syncdomain.Attachment{Kind: syncdomain.AttachmentTool, Upsert: file.Upsert, Tool: &file.Tool}, nil
}

// readSkill reads one skill file. The file name is the key, and the key in
// the front matter must match it.
func readSkill(fsys fs.FS, projectKey, key string) (syncdomain.Attachment, error) {
	data, err := readAttachmentFile(fsys, projectKey, syncdomain.AttachmentSkill, key)
	if err != nil {
		return syncdomain.Attachment{}, err
	}

	metadata, body, err := parseSkillFile(data)
	if err != nil {
		return syncdomain.Attachment{}, fmt.Errorf("parse skill %q: %w", key, err)
	}

	switch {
	case metadata.Key != key:
		return syncdomain.Attachment{}, fmt.Errorf("skill key %q does not match filename %q", metadata.Key, key)
	case metadata.Description == nil:
		return syncdomain.Attachment{}, fmt.Errorf("skill %q description is required in front matter", key)
	case strings.TrimSpace(string(body)) == "":
		return syncdomain.Attachment{}, fmt.Errorf("skill %q markdown is required", key)
	}
	skill := syncdomain.Skill{Key: key, Name: metadata.Name, Description: *metadata.Description, Markdown: string(body)}
	return syncdomain.Attachment{Kind: syncdomain.AttachmentSkill, Upsert: metadata.Upsert, Skill: &skill}, nil
}

// parseSkillFile decodes the front matter of a skill file and returns the
// Markdown that follows it.
func parseSkillFile(data []byte) (skillFrontMatter, []byte, error) {
	var metadata skillFrontMatter
	body, err := parseYAMLFrontMatter(data, &metadata)
	if err != nil {
		return skillFrontMatter{}, nil, err
	}
	// Rendering puts one blank line after the front matter. Remove it.
	if bytes.HasPrefix(body, []byte("\r\n")) {
		return metadata, body[2:], nil
	}
	return metadata, bytes.TrimPrefix(body, []byte("\n")), nil
}

// readAttachmentFile reads one tool or skill file. It rejects a symbolic link
// or another special file, so that a read stays in the managed directory.
func readAttachmentFile(fsys fs.FS, projectKey string, kind syncdomain.AttachmentKind, key string) ([]byte, error) {
	relativePath, err := attachmentPath(projectKey, kind, key)
	if err != nil {
		return nil, err
	}
	filePath := path.Join(syncdomain.RootDir, relativePath)

	info, err := fs.Stat(fsys, filePath)
	switch {
	case err != nil:
	case info.Mode()&fs.ModeSymlink != 0:
		err = errors.New("symbolic links are not supported")
	case !info.Mode().IsRegular():
		err = errors.New("not a regular file")
	}
	if err != nil {
		return nil, fmt.Errorf("read %s %q: %w", kind, key, err)
	}

	data, err := fs.ReadFile(fsys, filePath)
	if err != nil {
		return nil, fmt.Errorf("read %s %q: %w", kind, key, err)
	}
	return data, nil
}

// renderAttachmentFiles renders the file of each tool and skill that the
// variations use, in path order. Two variations can share an attachment, but
// they must have the same content for it.
func renderAttachmentFiles(files []VariationFile) ([]RenderedFile, error) {
	contents := make(map[string][]byte)
	for _, file := range files {
		for _, attachment := range file.Variation.Attachments {
			filePath, err := attachmentPath(file.ProjectKey, attachment.Kind, attachment.Key())
			if err != nil {
				return nil, err
			}
			content, err := renderAttachment(attachment)
			if err != nil {
				return nil, err
			}
			if existing, ok := contents[filePath]; ok && !bytes.Equal(existing, content) {
				return nil, fmt.Errorf("attachment %q has conflicting local definitions", filePath)
			}
			contents[filePath] = content
		}
	}

	rendered := make([]RenderedFile, 0, len(contents))
	for _, filePath := range slices.Sorted(maps.Keys(contents)) {
		rendered = append(rendered, RenderedFile{Path: filePath, Content: contents[filePath]})
	}
	return rendered, nil
}

func renderAttachment(attachment syncdomain.Attachment) ([]byte, error) {
	switch {
	case attachment.Kind == syncdomain.AttachmentTool && attachment.Tool != nil:
		if attachment.Tool.Schema == nil {
			return nil, fmt.Errorf("tool %q schema is required", attachment.Tool.Key)
		}
		return renderToolFile(toolFile{FormatVersion: toolFormatVersion, Upsert: attachment.Upsert, Tool: *attachment.Tool})
	case attachment.Kind == syncdomain.AttachmentSkill && attachment.Skill != nil:
		// LaunchDarkly owns the name of a skill, so the file does not get it.
		// A pull keeps the name of an existing file.
		skill := *attachment.Skill
		return renderSkillFile(skillFrontMatter{Key: skill.Key, Description: &skill.Description, Upsert: attachment.Upsert}, skill.Markdown)
	default:
		return nil, fmt.Errorf("%s %q has no content", attachment.Kind, attachment.Key())
	}
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

func renderSkillFile(metadata skillFrontMatter, markdown string) ([]byte, error) {
	if strings.TrimSpace(markdown) == "" {
		return nil, fmt.Errorf("skill %q markdown is required", metadata.Key)
	}
	frontMatter, err := marshalYAML(metadata)
	if err != nil {
		return nil, fmt.Errorf("marshal skill %q metadata: %w", metadata.Key, err)
	}

	var content bytes.Buffer
	content.WriteString("---\n")
	content.Write(frontMatter)
	content.WriteString("---\n\n")
	content.WriteString(markdown)
	return content.Bytes(), nil
}

// preserveLocalFields keeps the local choices of an existing tool or skill
// file: the upsert flag, and the name of a skill. New content from
// LaunchDarkly does not remove them.
func preserveLocalFields(filePath string, original, replacement []byte) ([]byte, error) {
	if strings.HasSuffix(filePath, toolFileSuffix) {
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

	local, _, err := parseSkillFile(original)
	if err != nil {
		return nil, fmt.Errorf("parse existing skill %q: %w", filePath, err)
	}
	if local.Name == "" && !local.Upsert {
		return replacement, nil
	}
	updated, markdown, err := parseSkillFile(replacement)
	if err != nil {
		return nil, fmt.Errorf("parse replacement skill %q: %w", filePath, err)
	}
	updated.Name, updated.Upsert = local.Name, updated.Upsert || local.Upsert
	return renderSkillFile(updated, string(markdown))
}
