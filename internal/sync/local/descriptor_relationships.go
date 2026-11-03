package local

import (
	"bytes"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
	"gopkg.in/yaml.v3"
)

// This file keeps the top-level tools and skills lists of a descriptor in
// step with a pulled variation. A pull edits only those list nodes and keeps
// the comments of the descriptor.

func (store Store) prepareMissingRelationshipAttachments(
	replacements []SourceReplacement,
) ([]stagedFile, error) {
	available := make(map[syncdomain.ResourceID]syncdomain.Attachment)
	for _, replacement := range replacements {
		for _, attachment := range replacement.RelationshipAttachments {
			available[attachment.ID(replacement.ID.ProjectKey)] = attachment
		}
	}
	needed := make(map[syncdomain.ResourceID]struct{})
	for _, replacement := range replacements {
		for _, kind := range syncdomain.AttachmentKinds {
			field := syncdomain.FieldTools
			if kind == syncdomain.AttachmentSkill {
				field = syncdomain.FieldSkills
			}
			refs, changed, err := projectionRelationship(replacement.Projection, field)
			if err != nil {
				return nil, err
			}
			if !changed {
				continue
			}
			for _, ref := range refs {
				id := syncdomain.ResourceID{
					Kind: syncdomain.Kind(kind), ProjectKey: replacement.ID.ProjectKey, LookupKey: ref.Key,
				}
				managed, _ := ResourcePath(id)
				descriptor, _ := ReferencePath(id)
				if fileExists(filepath.Join(store.repositoryRoot, filepath.FromSlash(managed))) ||
					fileExists(filepath.Join(store.repositoryRoot, filepath.FromSlash(descriptor))) {
					continue
				}
				needed[id] = struct{}{}
			}
		}
	}
	rendered := make([]RenderedFile, 0, len(needed))
	for _, id := range slices.SortedFunc(maps.Keys(needed), syncdomain.CompareResourceIDs) {
		attachment, ok := available[id]
		if !ok {
			return nil, missingLocalAttachmentDefinition(id)
		}
		file, err := RenderAttachment(syncdomain.SyncedAttachment{
			ProjectKey: id.ProjectKey, Attachment: attachment,
		})
		if err != nil {
			return nil, err
		}
		rendered = append(rendered, file)
	}
	return store.prepareAttachmentFiles(rendered)
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func referenceWithProjectedRelationships(reference Reference, projection syncdomain.Projection) (Reference, error) {
	tools, toolsChanged, err := projectionRelationship(projection, syncdomain.FieldTools)
	if err != nil {
		return Reference{}, err
	}
	if toolsChanged {
		reference.Tools = &tools
	}
	skills, skillsChanged, err := projectionRelationship(projection, syncdomain.FieldSkills)
	if err != nil {
		return Reference{}, err
	}
	if skillsChanged {
		reference.Skills = &skills
	}
	return reference, nil
}

func (store Store) prepareDescriptorRelationshipReplacement(
	replacement SourceReplacement,
) (stagedFile, bool, error) {
	tools, toolsChanged, err := projectionRelationship(replacement.Projection, syncdomain.FieldTools)
	if err != nil {
		return stagedFile{}, false, err
	}
	skills, skillsChanged, err := projectionRelationship(replacement.Projection, syncdomain.FieldSkills)
	if err != nil {
		return stagedFile{}, false, err
	}
	if !toolsChanged && !skillsChanged {
		return stagedFile{}, false, nil
	}
	descriptorPath, err := ReferencePath(replacement.ID)
	if err != nil {
		return stagedFile{}, false, err
	}
	target := filepath.Join(store.repositoryRoot, filepath.FromSlash(descriptorPath))
	original, err := os.ReadFile(target)
	if err != nil {
		return stagedFile{}, false, fmt.Errorf("read descriptor %q: %w", descriptorPath, err)
	}
	current, err := ReadDescriptor(os.DirFS(store.repositoryRoot), descriptorPath, replacement.ID)
	if err != nil {
		return stagedFile{}, false, err
	}
	if toolsChanged && !sameRelationshipPointer(current.Tools, replacement.Reference.Tools) ||
		skillsChanged && !sameRelationshipPointer(current.Skills, replacement.Reference.Skills) {
		return stagedFile{}, false, syncdomain.NewProblem(
			syncdomain.ErrorStateChanged,
			"descriptor relationships changed after review",
			"Run sync again.",
		).WithResource(replacement.ID).WithFile(descriptorPath, 0, 0)
	}
	updated, err := updateDescriptorRelationships(original, tools, toolsChanged, skills, skillsChanged)
	if err != nil {
		return stagedFile{}, false, fmt.Errorf("update descriptor %q: %w", descriptorPath, err)
	}
	if bytes.Equal(original, updated) {
		return stagedFile{}, false, nil
	}
	info, err := os.Stat(target)
	if err != nil {
		return stagedFile{}, false, fmt.Errorf("inspect descriptor %q: %w", descriptorPath, err)
	}
	return stagedFile{
		displayPath: descriptorPath, path: target, original: original, originalExists: true,
		replacement: updated, mode: info.Mode().Perm(),
	}, true, nil
}

func projectionRelationship(
	projection syncdomain.Projection,
	field syncdomain.Field,
) ([]syncdomain.AttachmentRef, bool, error) {
	if _, present := projection.States()[field]; !present {
		return nil, false, nil
	}
	raw, ok := projection.Value(field)
	if !ok {
		return nil, false, fmt.Errorf("relationship field %q has no value", field)
	}
	keys, ok := raw.([]string)
	if !ok {
		return nil, false, fmt.Errorf("relationship field %q has type %T", field, raw)
	}
	refs := make([]syncdomain.AttachmentRef, len(keys))
	for index := range keys {
		refs[index] = syncdomain.AttachmentRef{Key: keys[index]}
	}
	slices.SortFunc(refs, func(left, right syncdomain.AttachmentRef) int {
		return strings.Compare(left.Key, right.Key)
	})
	return refs, true, nil
}

func sameRelationshipPointer(left, right *[]syncdomain.AttachmentRef) bool {
	if left == nil || right == nil {
		return left == right
	}
	return slices.Equal(*left, *right)
}

func updateDescriptorRelationships(
	data []byte,
	tools []syncdomain.AttachmentRef,
	toolsChanged bool,
	skills []syncdomain.AttachmentRef,
	skillsChanged bool,
) ([]byte, error) {
	var document yaml.Node
	if err := yaml.Unmarshal(data, &document); err != nil {
		return nil, err
	}
	if len(document.Content) == 0 || document.Content[0].Kind != yaml.MappingNode {
		return nil, errors.New("descriptor must be a YAML mapping")
	}
	lineStarts := []int{0}
	for index, value := range data {
		if value == '\n' {
			lineStarts = append(lineStarts, index+1)
		}
	}
	type edit struct {
		start, end  int
		replacement []byte
	}
	var edits []edit
	appendField := func(name string, refs []syncdomain.AttachmentRef, changed bool) error {
		if !changed {
			return nil
		}
		replacement := renderRelationshipNode(name, refs, nil)
		root := document.Content[0]
		for index := 0; index+1 < len(root.Content); index += 2 {
			if root.Content[index].Value != name {
				continue
			}
			key, value := root.Content[index], root.Content[index+1]
			lastLine := relationshipNodeLastLine(value)
			if key.Line < 1 || key.Line > len(lineStarts) || lastLine < key.Line {
				return errors.New("descriptor relationship has invalid source positions")
			}
			replacement = renderRelationshipNode(name, refs, relationshipNodeComments(value))
			comment := strings.TrimSpace(value.LineComment)
			if comment == "" {
				comment = strings.TrimSpace(key.LineComment)
			}
			if comment != "" {
				lineEnd := bytes.IndexByte(replacement, '\n')
				replacement = slices.Concat(
					replacement[:lineEnd],
					[]byte(" "+comment),
					replacement[lineEnd:],
				)
			}
			start := lineStarts[key.Line-1]
			end := len(data)
			if lastLine < len(lineStarts) {
				end = lineStarts[lastLine]
			}
			edits = append(edits, edit{start: start, end: end, replacement: replacement})
			return nil
		}
		start := len(data)
		for index := range edits {
			if edits[index].start == start && edits[index].end == start {
				edits[index].replacement = append(edits[index].replacement, replacement...)
				return nil
			}
		}
		prefix := []byte{}
		if start != 0 && data[start-1] != '\n' {
			prefix = []byte("\n")
		}
		edits = append(edits, edit{start: start, end: start, replacement: append(prefix, replacement...)})
		return nil
	}
	if err := appendField("tools", tools, toolsChanged); err != nil {
		return nil, err
	}
	if err := appendField("skills", skills, skillsChanged); err != nil {
		return nil, err
	}
	slices.SortFunc(edits, func(left, right edit) int { return right.start - left.start })
	updated := bytes.Clone(data)
	for _, item := range edits {
		next := make([]byte, 0, len(updated)-item.end+item.start+len(item.replacement))
		next = append(next, updated[:item.start]...)
		next = append(next, item.replacement...)
		next = append(next, updated[item.end:]...)
		updated = next
	}
	return updated, nil
}

func relationshipNodeLastLine(node *yaml.Node) int {
	line := node.Line
	for _, child := range node.Content {
		if childLine := relationshipNodeLastLine(child); childLine > line {
			line = childLine
		}
	}
	return line
}

func relationshipNodeComments(node *yaml.Node) []string {
	var comments []string
	seen := make(map[string]struct{})
	var visit func(*yaml.Node)
	visit = func(current *yaml.Node) {
		for _, value := range []string{current.HeadComment, current.LineComment, current.FootComment} {
			for line := range strings.SplitSeq(value, "\n") {
				line = strings.TrimSpace(line)
				if line == "" {
					continue
				}
				if _, duplicate := seen[line]; !duplicate {
					seen[line] = struct{}{}
					comments = append(comments, line)
				}
			}
		}
		for _, child := range current.Content {
			visit(child)
		}
	}
	for _, child := range node.Content {
		visit(child)
	}
	return comments
}

func renderRelationshipNode(name string, refs []syncdomain.AttachmentRef, comments []string) []byte {
	refs = slices.Clone(refs)
	slices.SortFunc(refs, func(left, right syncdomain.AttachmentRef) int {
		return strings.Compare(left.Key, right.Key)
	})
	if len(refs) == 0 {
		var content strings.Builder
		content.WriteString(name + ": []\n")
		for _, comment := range comments {
			content.WriteString("  " + comment + "\n")
		}
		return []byte(content.String())
	}
	var content strings.Builder
	content.WriteString(name + ":\n")
	for _, comment := range comments {
		content.WriteString("  " + comment + "\n")
	}
	for _, ref := range refs {
		content.WriteString("  - key: ")
		value, _ := yaml.Marshal(ref.Key)
		content.Write(bytes.TrimSpace(value))
		content.WriteByte('\n')
	}
	return []byte(content.String())
}
