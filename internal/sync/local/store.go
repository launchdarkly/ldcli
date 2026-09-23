package local

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"

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

type VariationReplacement struct {
	ProjectKey string
	ConfigKey  string
	Variation  syncdomain.Variation
}

type VariationDeletion struct {
	ProjectKey   string
	ConfigKey    string
	VariationKey string
}

type RenderedVariationFile struct {
	Path    string
	Content []byte
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

func (s Store) ProjectKeys() ([]string, error) {
	entries, err := os.ReadDir(s.root)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", s.root, err)
	}

	var keys []string
	for _, entry := range entries {
		if entry.IsDir() {
			keys = append(keys, entry.Name())
		}
	}
	slices.Sort(keys)
	return keys, nil
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

// ReplaceVariations stages the entire batch before replacing any source file.
func (s Store) ReplaceVariations(resources []VariationReplacement) ([]string, error) {
	replacements, err := s.prepareReplacements(resources)
	if err != nil {
		return nil, err
	}
	if err := stageReplacements(replacements); err != nil {
		return nil, err
	}
	defer removeStagedVariations(replacements)

	// Staging can take long enough for an editor to change a source file.
	// Recheck every source before replacing any of them.
	if err := verifyReplacementSources(replacements); err != nil {
		return nil, err
	}
	if err := commitReplacements(replacements); err != nil {
		return nil, err
	}

	return replacementPaths(replacements), nil
}

func (s Store) prepareReplacements(
	resources []VariationReplacement,
) ([]stagedVariation, error) {
	replacements := make([]stagedVariation, 0, len(resources))
	seen := make(map[string]struct{}, len(resources))

	for _, resource := range resources {
		existing, err := s.inspectVariation(
			resource.ProjectKey,
			resource.ConfigKey,
			resource.Variation.Key,
		)
		if err != nil {
			return nil, err
		}
		if _, duplicate := seen[existing.absolutePath]; duplicate {
			return nil, fmt.Errorf(
				"variation %q was selected more than once",
				resource.Variation.Key,
			)
		}
		seen[existing.absolutePath] = struct{}{}

		content, err := marshalVariationFile(VariationFile{
			ProjectKey: resource.ProjectKey,
			ConfigKey:  resource.ConfigKey,
			Upsert:     existing.frontMatter.Upsert,
			Variation:  resource.Variation,
		})
		if err != nil {
			return nil, err
		}

		replacements = append(replacements, stagedVariation{
			relativePath:       existing.relativePath,
			destinationPath:    existing.absolutePath,
			originalContent:    existing.content,
			replacementContent: content,
			mode:               existing.mode,
		})
	}

	return replacements, nil
}

func stageReplacements(replacements []stagedVariation) error {
	for index := range replacements {
		stagedPath, err := stageReplacement(replacements[index])
		if err != nil {
			removeStagedVariations(replacements)
			return err
		}
		replacements[index].stagedPath = stagedPath
	}
	return nil
}

func verifyReplacementSources(replacements []stagedVariation) error {
	for _, replacement := range replacements {
		current, err := os.ReadFile(replacement.destinationPath)
		if err != nil {
			return fmt.Errorf("recheck variation %s: %w", replacement.relativePath, err)
		}
		if !bytes.Equal(current, replacement.originalContent) {
			return fmt.Errorf("variation %s changed while syncing", replacement.relativePath)
		}
	}
	return nil
}

func commitReplacements(replacements []stagedVariation) error {
	var replaced []stagedVariation
	for _, replacement := range replacements {
		if err := os.Rename(replacement.stagedPath, replacement.destinationPath); err != nil {
			return errors.Join(
				fmt.Errorf("replace variation %s: %w", replacement.relativePath, err),
				rollbackVariations(replaced),
			)
		}
		replaced = append(replaced, replacement)
	}
	return nil
}

func replacementPaths(replacements []stagedVariation) []string {
	paths := make([]string, len(replacements))
	for index, replacement := range replacements {
		paths[index] = replacement.relativePath
	}
	return paths
}

// DeleteVariations stages the entire batch before removing any file permanently.
func (s Store) DeleteVariations(resources []VariationDeletion) ([]string, error) {
	deletions, err := s.prepareDeletions(resources)
	if err != nil {
		return nil, err
	}

	// Move the complete batch to same-directory backups before removing
	// anything permanently. A staging failure can therefore restore every file.
	if err := stageDeletions(deletions); err != nil {
		return nil, err
	}
	if err := commitDeletions(s.root, deletions); err != nil {
		return nil, err
	}

	return deletionPaths(deletions), nil
}

func (s Store) prepareDeletions(
	resources []VariationDeletion,
) ([]stagedDeletion, error) {
	deletions := make([]stagedDeletion, 0, len(resources))
	seen := make(map[string]struct{}, len(resources))

	for _, resource := range resources {
		existing, err := s.inspectVariation(
			resource.ProjectKey,
			resource.ConfigKey,
			resource.VariationKey,
		)
		if err != nil {
			return nil, err
		}
		if _, duplicate := seen[existing.absolutePath]; duplicate {
			return nil, fmt.Errorf(
				"variation %q was selected more than once",
				resource.VariationKey,
			)
		}
		seen[existing.absolutePath] = struct{}{}

		deletions = append(deletions, stagedDeletion{
			relativePath: existing.relativePath,
			originalPath: existing.absolutePath,
		})
	}

	return deletions, nil
}

func stageDeletions(deletions []stagedDeletion) error {
	var staged []stagedDeletion
	for index := range deletions {
		backupPath, err := reserveBackupPath(deletions[index].originalPath)
		if err == nil {
			err = os.Rename(deletions[index].originalPath, backupPath)
		}
		if err != nil {
			return errors.Join(
				fmt.Errorf("stage deletion %s: %w", deletions[index].relativePath, err),
				rollbackDeletions(staged),
			)
		}

		deletions[index].backupPath = backupPath
		staged = append(staged, deletions[index])
	}
	return nil
}

func reserveBackupPath(originalPath string) (string, error) {
	temp, err := os.CreateTemp(
		filepath.Dir(originalPath),
		"."+filepath.Base(originalPath)+".deleted-",
	)
	if err != nil {
		return "", err
	}

	backupPath := temp.Name()
	if err := temp.Close(); err != nil {
		_ = os.Remove(backupPath)
		return "", err
	}
	if err := os.Remove(backupPath); err != nil {
		return "", err
	}
	return backupPath, nil
}

func commitDeletions(root string, deletions []stagedDeletion) error {
	for _, deletion := range deletions {
		if err := os.Remove(deletion.backupPath); err != nil {
			return fmt.Errorf(
				"finish deleting variation %s: %w",
				deletion.relativePath,
				err,
			)
		}
		removeEmptyParentsThroughRoot(root, filepath.Dir(deletion.originalPath))
	}
	return nil
}

func deletionPaths(deletions []stagedDeletion) []string {
	paths := make([]string, len(deletions))
	for index, deletion := range deletions {
		paths[index] = deletion.relativePath
	}
	return paths
}

type existingVariation struct {
	relativePath string
	absolutePath string
	content      []byte
	mode         os.FileMode
	frontMatter  variationFrontMatter
}

func (s Store) inspectVariation(
	projectKey string,
	configKey string,
	variationKey string,
) (existingVariation, error) {
	absolutePath, err := s.variationPath(projectKey, configKey, variationKey)
	if err != nil {
		return existingVariation{}, err
	}

	content, err := os.ReadFile(absolutePath)
	if err != nil {
		return existingVariation{}, fmt.Errorf("read variation %s: %w", variationKey, err)
	}
	info, err := os.Stat(absolutePath)
	if err != nil {
		return existingVariation{}, fmt.Errorf("stat variation %s: %w", variationKey, err)
	}
	if !info.Mode().IsRegular() {
		return existingVariation{}, fmt.Errorf("variation %s is not a regular file", variationKey)
	}

	var frontMatter variationFrontMatter
	if _, err := parseYAMLFrontMatter(content, &frontMatter); err != nil {
		return existingVariation{}, fmt.Errorf("parse variation %s: %w", variationKey, err)
	}
	if err := validateVariation(filepath.Base(absolutePath), frontMatter); err != nil {
		return existingVariation{}, fmt.Errorf("validate variation %s: %w", variationKey, err)
	}

	return existingVariation{
		relativePath: filepath.ToSlash(strings.TrimPrefix(
			absolutePath,
			s.root+string(filepath.Separator),
		)),
		absolutePath: absolutePath,
		content:      content,
		mode:         info.Mode().Perm(),
		frontMatter:  frontMatter,
	}, nil
}

func (s Store) RemoveEmptyDirectories() error {
	var directories []string
	err := filepath.WalkDir(s.root, func(path string, entry os.DirEntry, walkErr error) error {
		if errors.Is(walkErr, os.ErrNotExist) {
			return nil
		}
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			directories = append(directories, path)
		}
		return nil
	})
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect empty sync directories: %w", err)
	}
	for index := len(directories) - 1; index >= 0; index-- {
		err := os.Remove(directories[index])
		if err == nil || errors.Is(err, os.ErrNotExist) || errors.Is(err, syscall.ENOTEMPTY) {
			continue
		}
		return fmt.Errorf("remove empty sync directory %s: %w", directories[index], err)
	}
	return nil
}

func (s Store) RenderVariations(resources []VariationFile) ([]RenderedVariationFile, error) {
	rendered := make([]RenderedVariationFile, 0, len(resources))
	seen := make(map[string]struct{}, len(resources))
	for _, resource := range resources {
		absolute, err := s.variationPath(
			resource.ProjectKey,
			resource.ConfigKey,
			resource.Variation.Key,
		)
		if err != nil {
			return nil, err
		}
		if _, ok := seen[absolute]; ok {
			return nil, fmt.Errorf("variation %q was selected more than once", resource.Variation.Key)
		}
		seen[absolute] = struct{}{}

		content, err := marshalVariationFile(resource)
		if err != nil {
			return nil, err
		}
		rendered = append(rendered, RenderedVariationFile{
			Path:    filepath.ToSlash(strings.TrimPrefix(absolute, s.root+string(filepath.Separator))),
			Content: content,
		})
	}

	return rendered, nil
}

func (s Store) createVariations(resources []VariationFile) ([]string, error) {
	rendered, err := s.RenderVariations(resources)
	if err != nil {
		return nil, err
	}

	var created []string
	for _, file := range rendered {
		absolute := filepath.Join(s.root, filepath.FromSlash(file.Path))
		if err := createFile(absolute, file.Content); err != nil {
			for index := len(created) - 1; index >= 0; index-- {
				_ = os.Remove(filepath.Join(s.root, filepath.FromSlash(created[index])))
			}
			return nil, err
		}
		created = append(created, file.Path)
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

type stagedVariation struct {
	relativePath       string
	destinationPath    string
	originalContent    []byte
	replacementContent []byte
	mode               os.FileMode
	stagedPath         string
}

type stagedDeletion struct {
	relativePath string
	originalPath string
	backupPath   string
}

func rollbackDeletions(deletions []stagedDeletion) error {
	var rollbackErr error
	for index := len(deletions) - 1; index >= 0; index-- {
		if err := os.Rename(
			deletions[index].backupPath,
			deletions[index].originalPath,
		); err != nil {
			rollbackErr = errors.Join(
				rollbackErr,
				fmt.Errorf(
					"restore variation %s: %w",
					deletions[index].relativePath,
					err,
				),
			)
		}
	}
	return rollbackErr
}

// removeEmptyParentsThroughRoot removes empty variation, config, and project
// directories. It also removes the .launchdarkly root when the store is empty.
func removeEmptyParentsThroughRoot(root, current string) {
	for {
		if err := os.Remove(current); err != nil {
			return
		}
		if current == root {
			return
		}
		current = filepath.Dir(current)
	}
}

func stageReplacement(replacement stagedVariation) (string, error) {
	temp, err := os.CreateTemp(
		filepath.Dir(replacement.destinationPath),
		"."+filepath.Base(replacement.destinationPath)+".tmp-",
	)
	if err != nil {
		return "", fmt.Errorf("stage variation %s: %w", replacement.relativePath, err)
	}
	tempPath := temp.Name()
	closeWithError := func(err error) (string, error) {
		_ = temp.Close()
		_ = os.Remove(tempPath)
		return "", err
	}

	if err := temp.Chmod(replacement.mode); err != nil {
		return closeWithError(fmt.Errorf(
			"set variation permissions %s: %w",
			replacement.relativePath,
			err,
		))
	}
	if _, err := temp.Write(replacement.replacementContent); err != nil {
		return closeWithError(fmt.Errorf(
			"write staged variation %s: %w",
			replacement.relativePath,
			err,
		))
	}
	if err := temp.Sync(); err != nil {
		return closeWithError(fmt.Errorf(
			"sync staged variation %s: %w",
			replacement.relativePath,
			err,
		))
	}
	if err := temp.Close(); err != nil {
		_ = os.Remove(tempPath)
		return "", fmt.Errorf("close staged variation %s: %w", replacement.relativePath, err)
	}

	return tempPath, nil
}

func removeStagedVariations(replacements []stagedVariation) {
	for _, replacement := range replacements {
		if replacement.stagedPath != "" {
			_ = os.Remove(replacement.stagedPath)
		}
	}
}

func rollbackVariations(replacements []stagedVariation) error {
	var rollbackErr error
	for index := len(replacements) - 1; index >= 0; index-- {
		replacement := replacements[index]
		tempPath, err := stageReplacement(stagedVariation{
			relativePath:       replacement.relativePath,
			destinationPath:    replacement.destinationPath,
			replacementContent: replacement.originalContent,
			mode:               replacement.mode,
		})
		if err == nil {
			err = os.Rename(tempPath, replacement.destinationPath)
			if err != nil {
				_ = os.Remove(tempPath)
			}
		}
		if err != nil {
			rollbackErr = errors.Join(
				rollbackErr,
				fmt.Errorf("roll back variation %s: %w", replacement.relativePath, err),
			)
		}
	}
	return rollbackErr
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
