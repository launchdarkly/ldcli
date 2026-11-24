package local

import (
	"bytes"
	"errors"
	"fmt"
	"maps"
	"os"
	"path"
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
	fingerprints := make(map[syncdomain.ResourceID]string)
	for _, replacement := range replacements {
		for _, attachment := range replacement.RelationshipAttachments {
			id := attachment.ID(replacement.ID.ProjectKey)
			fingerprint, err := syncdomain.FingerprintAttachment(replacement.ID.ProjectKey, attachment)
			if err != nil {
				return nil, err
			}
			// Two variations can pull one tool with two bodies, for example
			// from two versions. One publish writes one file, so the bodies
			// must agree.
			if previous, seen := fingerprints[id]; seen && previous != fingerprint {
				return nil, syncdomain.NewProblem(
					syncdomain.ErrorDefinitionConflict,
					fmt.Sprintf("%s %q has two different definitions in this pull", id.Kind, id.LookupKey),
					fmt.Sprintf("Run ldcli sync %ss first, so that the variations share one version.", id.Kind),
				).WithResource(id)
			}
			available[id], fingerprints[id] = attachment, fingerprint
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
				managed, err := ResourcePath(id)
				if err != nil {
					return nil, err
				}
				descriptor, err := ReferencePath(id)
				if err != nil {
					return nil, err
				}
				// A descriptor defines the resource. A leftover link at the
				// managed path is not a definition, as it is not for compile,
				// so it neither defines the resource nor stops the pull. A
				// link at the descriptor path stops the pull.
				descriptorTarget := filepath.Join(store.repositoryRoot, filepath.FromSlash(descriptor))
				if err := rejectSymlinkedPath(store.root, descriptorTarget); err != nil {
					return nil, err
				}
				managedTarget := filepath.Join(store.repositoryRoot, filepath.FromSlash(managed))
				if fileExists(descriptorTarget) || fileExists(managedTarget) {
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
	staged, err := store.prepareAttachmentFiles(rendered)
	if err != nil {
		return nil, err
	}
	// A rendered attachment path is relative to the managed directory, and
	// every other path that a publish reports is relative to the repository.
	for index := range staged {
		staged[index].displayPath = path.Join(syncdomain.RootDir, staged[index].displayPath)
	}
	return staged, nil
}

// fileExists reports whether a regular file is at path. A symbolic link is
// not a definition, because compile rejects it.
func fileExists(path string) bool {
	info, err := os.Lstat(path)
	return err == nil && info.Mode().IsRegular()
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

// referenceWithRelationships returns a reference with the changed lists.
func referenceWithRelationships(
	reference Reference,
	tools []syncdomain.AttachmentRef,
	toolsChanged bool,
	skills []syncdomain.AttachmentRef,
	skillsChanged bool,
) Reference {
	if toolsChanged {
		reference.Tools = &tools
	}
	if skillsChanged {
		reference.Skills = &skills
	}
	return reference
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
	// Sync owns the managed directory, so the write refuses a path that goes
	// through a symbolic link, as every other managed write does.
	if err := rejectSymlinkedPath(store.root, target); err != nil {
		return stagedFile{}, false, err
	}
	original, err := os.ReadFile(target)
	if errors.Is(err, os.ErrNotExist) {
		// A link checks its plan before it writes the descriptor, so there
		// is no list to edit yet. A write checked the descriptor first.
		return stagedFile{}, false, nil
	}
	if err != nil {
		return stagedFile{}, false, fmt.Errorf("read descriptor %q: %w", descriptorPath, err)
	}
	// The bytes of this one read serve the checks, the edit, and the
	// original that the commit verifies, so a descriptor that another
	// process changes between two reads cannot pass the checks on one
	// content and stage an edit of another.
	current, err := decodeDescriptor(original, replacement.ID)
	if err != nil {
		return stagedFile{}, false, err
	}
	mappingChanged, err := descriptorMappingChanged(replacement, current)
	if err != nil {
		return stagedFile{}, false, err
	}
	if mappingChanged {
		return stagedFile{}, false, syncdomain.NewProblem(
			syncdomain.ErrorStateChanged, "the source descriptor changed after review", "Run sync again.",
		).WithResource(replacement.ID).WithFile(descriptorPath, 0, 0)
	}
	if toolsChanged && !sameRelationshipPointer(current.Tools, replacement.Reference.Tools) ||
		skillsChanged && !sameRelationshipPointer(current.Skills, replacement.Reference.Skills) {
		return stagedFile{}, false, syncdomain.NewProblem(
			syncdomain.ErrorStateChanged,
			"descriptor relationships changed after review",
			"Run sync again.",
		).WithResource(replacement.ID).WithFile(descriptorPath, 0, 0)
	}
	next := referenceWithRelationships(current, tools, toolsChanged, skills, skillsChanged)
	updated, err := updateDescriptorRelationships(original, tools, toolsChanged, skills, skillsChanged)
	if err == nil && !descriptorReadsBack(updated, replacement.ID, next) {
		// The in-place edit does not read back as the pull, for example
		// because an anchor on the list was lost with it.
		err = errRelationshipsShareLine
	}
	if errors.Is(err, errRelationshipsShareLine) {
		// The lists cannot be edited in place, so the descriptor is
		// rendered again from its content. The comments do not survive.
		updated, err = RenderDescriptor(replacement.ID, next)
	}
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

// descriptorReadsBack reports whether edited descriptor bytes decode to the
// file, format, selection, map, tools, and skills of the reference.
func descriptorReadsBack(data []byte, id syncdomain.ResourceID, reference Reference) bool {
	parsed, err := decodeDescriptor(data, id)
	if err != nil {
		return false
	}
	sortReferenceRelationships(&reference)
	return parsed.File == reference.File && parsed.Format == reference.Format &&
		parsed.Select == reference.Select && maps.Equal(parsed.Map, reference.Map) &&
		sameRelationshipPointer(parsed.Tools, reference.Tools) &&
		sameRelationshipPointer(parsed.Skills, reference.Skills)
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

// errRelationshipsShareLine reports that a tools or skills list of a
// descriptor shares its lines with other content, as in a flow mapping, so
// the list cannot be edited in place and the descriptor is rendered again.
var errRelationshipsShareLine = errors.New("descriptor relationship lists share a line")

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
	// An in-place edit replaces whole lines, and a flow mapping keeps every
	// key on one line.
	if document.Content[0].Style&yaml.FlowStyle != 0 {
		return nil, errRelationshipsShareLine
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
			// Another key on the replaced lines would be lost with them.
			for other := 0; other+1 < len(root.Content); other += 2 {
				if other != index && root.Content[other].Line >= key.Line && root.Content[other].Line <= lastLine {
					return errRelationshipsShareLine
				}
			}
			start := lineStarts[key.Line-1]
			end := len(data)
			if lastLine < len(lineStarts) {
				end = lineStarts[lastLine]
			}
			// A comment on the lines after the list belongs to it, and the
			// rendered list repeats it, so the replaced range takes it in.
			end = relationshipCommentLinesEnd(data, end, relationshipNodeComments(value))
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
	// Two lists on one line, as in a flow mapping, share a range, and two
	// edits of one range cannot both apply.
	for index := 1; index < len(edits); index++ {
		if edits[index].end > edits[index-1].start {
			return nil, errRelationshipsShareLine
		}
	}
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

// relationshipCommentLinesEnd extends an end offset over the comment lines
// that follow it, as long as each one is a comment that the list repeats.
func relationshipCommentLinesEnd(data []byte, end int, comments []string) int {
	repeated := make(map[string]struct{}, len(comments))
	for _, comment := range comments {
		repeated[comment] = struct{}{}
	}
	for end < len(data) {
		lineEnd := bytes.IndexByte(data[end:], '\n')
		next := len(data)
		if lineEnd >= 0 {
			next = end + lineEnd + 1
		}
		line := strings.TrimSpace(string(data[end:next]))
		if _, ok := repeated[line]; !ok || !strings.HasPrefix(line, "#") {
			return end
		}
		end = next
	}
	return end
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
