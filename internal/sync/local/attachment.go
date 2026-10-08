package local

import (
	"bytes"
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
)

// A tool file is JSON. A skill file is Markdown with YAML front matter that
// has the key and the description.
const toolFormatVersion = 1

type toolFile struct {
	FormatVersion int  `json:"formatVersion"`
	Upsert        bool `json:"upsert,omitempty"`
	syncdomain.Tool
}

type skillFrontMatter struct {
	Key         string  `yaml:"key"`
	Description *string `yaml:"description"`
}

// OrphanedAttachment is a tool or skill file that no variation uses.
type OrphanedAttachment struct {
	ProjectKey string
	Kind       syncdomain.AttachmentKind
	Key        string
	Path       string
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

	var metadata skillFrontMatter
	body, err := parseYAMLFrontMatter(data, &metadata)
	if err != nil {
		return syncdomain.Attachment{}, fmt.Errorf("parse skill %q: %w", key, err)
	}
	// Rendering puts one blank line after the front matter. Remove it.
	if bytes.HasPrefix(body, []byte("\r\n")) {
		body = body[2:]
	} else {
		body = bytes.TrimPrefix(body, []byte("\n"))
	}

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
		return renderSkill(*attachment.Skill)
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

func renderSkill(skill syncdomain.Skill) ([]byte, error) {
	if strings.TrimSpace(skill.Markdown) == "" {
		return nil, fmt.Errorf("skill %q markdown is required", skill.Key)
	}
	metadata, err := marshalYAML(skillFrontMatter{Key: skill.Key, Description: &skill.Description})
	if err != nil {
		return nil, fmt.Errorf("marshal skill %q metadata: %w", skill.Key, err)
	}

	var content bytes.Buffer
	content.WriteString("---\n")
	content.Write(metadata)
	content.WriteString("---\n\n")
	content.WriteString(skill.Markdown)
	return content.Bytes(), nil
}

// preserveToolUpsert keeps the upsert flag of an existing tool file. The flag
// is a local choice, so new content from LaunchDarkly does not remove it.
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

// OrphanedAttachments returns each tool and skill file that no local
// variation uses.
func (store Store) OrphanedAttachments() ([]OrphanedAttachment, error) {
	variations, err := CompileWorkspace(store.repositoryRoot)
	if err != nil && !errors.Is(err, ErrNoDirectory) {
		return nil, err
	}
	used := make(map[syncdomain.ResourceID]struct{})
	for _, variation := range variations {
		for _, attachment := range variation.Variation.Attachments {
			used[attachment.ID(variation.ProjectKey)] = struct{}{}
		}
	}

	var orphaned []OrphanedAttachment
	err = filepath.WalkDir(store.root, func(filePath string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		relativePath, err := filepath.Rel(store.repositoryRoot, filePath)
		if err != nil {
			return err
		}
		id, ok := ParseManagedPath(filepath.ToSlash(relativePath))
		if !ok || id.Kind == syncdomain.KindVariation {
			return nil
		}
		if _, isUsed := used[id]; !isUsed {
			orphaned = append(orphaned, OrphanedAttachment{
				ProjectKey: id.ProjectKey,
				Kind:       syncdomain.AttachmentKind(id.Kind),
				Key:        id.LookupKey,
				Path:       strings.TrimPrefix(filepath.ToSlash(relativePath), syncdomain.RootDir+"/"),
			})
		}
		return nil
	})
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find unreferenced attachments: %w", err)
	}

	slices.SortFunc(orphaned, func(left, right OrphanedAttachment) int {
		return cmp.Or(
			strings.Compare(left.ProjectKey, right.ProjectKey),
			strings.Compare(string(left.Kind), string(right.Kind)),
			strings.Compare(left.Key, right.Key),
		)
	})
	return orphaned, nil
}

// DeleteAttachments deletes tool and skill files in one transaction.
func (store Store) DeleteAttachments(attachments []OrphanedAttachment) ([]string, error) {
	paths := make([]string, 0, len(attachments))
	for _, attachment := range attachments {
		relativePath, err := attachmentPath(attachment.ProjectKey, attachment.Kind, attachment.Key)
		if err != nil {
			return nil, err
		}
		if relativePath != attachment.Path {
			return nil, fmt.Errorf("attachment path %q does not match %q", attachment.Path, relativePath)
		}
		paths = append(paths, relativePath)
	}
	return store.deleteFiles(paths)
}
