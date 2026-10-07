package link

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/charmbracelet/huh"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
	syncapi "github.com/launchdarkly/ldcli/internal/sync/api"
	syncconsole "github.com/launchdarkly/ldcli/internal/sync/console"
	syncinteractive "github.com/launchdarkly/ldcli/internal/sync/interactive"
	synclocal "github.com/launchdarkly/ldcli/internal/sync/local"
	syncreference "github.com/launchdarkly/ldcli/internal/sync/reference"
	"github.com/launchdarkly/ldcli/internal/sync/reference/adapters"
)

// Catalog lists the LaunchDarkly resources required to link a prompt.
type Catalog interface {
	SearchProjects(query string, limit, offset int) (syncapi.Page[syncapi.Project], error)
	SearchConfigs(projectKey, query string, modes []syncdomain.VariationMode, limit, offset int) (syncapi.Page[syncapi.Config], error)
	ModelConfigs(projectKey string) ([]syncapi.ModelConfig, error)
}

// Options contains the dependencies and inputs for one link operation.
type Options struct {
	Catalog          Catalog
	Store            synclocal.Store
	RepositoryRoot   string
	WorkingDirectory string
	File             string
	Format           string
	Input            io.Reader
	Output           io.Writer
}

// Selection is the LaunchDarkly destination selected for a linked prompt.
type Selection struct {
	Project     syncapi.Project
	Config      syncapi.Config
	ModelConfig syncapi.ModelConfig
	Key         string
	Name        string
}

type linkedPrompt struct {
	reference       synclocal.Reference
	parsed          adapters.Prompt
	originalContent []byte
	content         []byte
}

// Run interactively selects a destination and creates the local linked
// variation wrapper.
func Run(options Options) (string, error) {
	if !syncinteractive.StreamsAreTerminal(options.Input, options.Output) {
		return "", fmt.Errorf("interactive prompt linking requires a terminal; run this command in a terminal")
	}

	prompt, err := readLinkedPrompt(options)
	if err != nil {
		return "", err
	}

	console := syncconsole.New(options.Output)
	project, canceled, err := syncinteractive.SearchSelect(syncinteractive.SearchOptions[syncapi.Project]{
		Input:             options.Input,
		Output:            options.Output,
		SearchTitle:       "Search LaunchDarkly projects",
		SearchPlaceholder: "Project name or key",
		SelectTitle:       "Choose a LaunchDarkly project",
		ItemName:          "projects",
		Fetch: func(query string, limit, offset int) ([]syncapi.Project, int, error) {
			page, err := options.Catalog.SearchProjects(query, limit, offset)
			return page.Items, page.TotalCount, err
		},
		Choice: projectChoice,
	})
	if err != nil || canceled {
		return "", err
	}

	var configModes []syncdomain.VariationMode
	if prompt.parsed.Mode != "" {
		configModes = []syncdomain.VariationMode{syncdomain.VariationMode(prompt.parsed.Mode)}
	}
	config, canceled, err := syncinteractive.SearchSelect(syncinteractive.SearchOptions[syncapi.Config]{
		Input:             options.Input,
		Output:            options.Output,
		SearchTitle:       "Search LaunchDarkly configs",
		SearchPlaceholder: "Config name or key",
		SelectTitle:       "Choose a config",
		ItemName:          "configs",
		Fetch: func(query string, limit, offset int) ([]syncapi.Config, int, error) {
			page, err := options.Catalog.SearchConfigs(project.Key, query, configModes, limit, offset)
			return page.Items, page.TotalCount, err
		},
		Choice: configChoice,
	})
	if err != nil || canceled {
		return "", err
	}

	_ = console.Line("Loading model configs...")
	modelConfigs, err := options.Catalog.ModelConfigs(project.Key)
	if err != nil {
		return "", err
	}
	modelConfig, canceled, err := syncinteractive.Select(
		options.Input,
		options.Output,
		"Choose a model config",
		modelConfigChoices(modelConfigs),
	)
	if err != nil || canceled {
		return "", err
	}

	defaultKey := strings.TrimSuffix(filepath.Base(prompt.reference.File), filepath.Ext(prompt.reference.File))
	key := prompt.parsed.Key
	var fields []huh.Field
	if key == "" {
		key = defaultKey
		fields = append(fields, huh.NewInput().
			Title("Variation key").
			Value(&key).
			Validate(validateDerivedKey))
	}
	name := prompt.parsed.Name
	if name == "" {
		name = displayName(key)
		fields = append(fields, huh.NewInput().
			Title("Variation name").
			Value(&name).
			Validate(requiredValue("variation name")))
	}
	var content string
	if len(prompt.parsed.Messages) == 0 {
		fields = append(fields, huh.NewText().
			Title("Prompt content").
			Lines(8).
			Value(&content).
			Validate(requiredValue("prompt content")))
	}
	if len(fields) != 0 {
		canceled, err = syncinteractive.RunForm(options.Input, options.Output, fields...)
		if err != nil || canceled {
			return "", err
		}
	}

	prompt, err = addMissingPromptContent(prompt, config.Mode, key, name, content)
	if err != nil {
		return "", err
	}

	return createLinkedPrompt(options, Selection{
		Project:     project,
		Config:      config,
		ModelConfig: modelConfig,
		Key:         key,
		Name:        name,
	}, prompt)
}

// addMissingPromptContent renders entered content in memory so destination
// validation can finish before the referenced file is changed.
func addMissingPromptContent(
	prompt linkedPrompt,
	mode syncdomain.VariationMode,
	key string,
	name string,
	content string,
) (linkedPrompt, error) {
	if len(prompt.parsed.Messages) != 0 {
		return prompt, nil
	}

	variation := syncdomain.Variation{Mode: mode, Key: key, Name: name}
	if mode == syncdomain.VariationModeAgent {
		variation.Instructions = content
	} else {
		variation.Messages = []syncdomain.Message{{Role: "system", Content: content}}
	}
	rendered, err := syncreference.Render(prompt.reference.Format, variation)
	if err != nil {
		return linkedPrompt{}, err
	}

	prompt.content = rendered
	return prompt, nil
}

// Create writes the wrapper for an already selected destination.
func Create(options Options, selection Selection) (string, error) {
	prompt, err := readLinkedPrompt(options)
	if err != nil {
		return "", err
	}
	return createLinkedPrompt(options, selection, prompt)
}

// createLinkedPrompt validates the destination, updates newly entered source
// content, and creates the wrapper that binds both sides.
func createLinkedPrompt(options Options, selection Selection, prompt linkedPrompt) (string, error) {
	variation := syncdomain.Variation{
		Mode:               selection.Config.Mode,
		Key:                selection.Key,
		Name:               selection.Name,
		ModelConfigKey:     selection.ModelConfig.Key,
		ModelConfigVersion: selection.ModelConfig.Version,
		Model:              selection.ModelConfig.VariationModel(),
	}
	if _, err := syncreference.ApplyToVariation(prompt.reference.Format, prompt.content, &variation); err != nil {
		return "", err
	}
	if variation.Mode != selection.Config.Mode {
		return "", fmt.Errorf("referenced prompt mode %q does not match config mode %q", variation.Mode, selection.Config.Mode)
	}
	if err := validateDerivedKey(variation.Key); err != nil {
		return "", err
	}
	if variation.Name == "" {
		return "", fmt.Errorf("variation name is required")
	}
	for _, existingVariation := range selection.Config.Variations {
		if existingVariation.Key == variation.Key {
			return "", fmt.Errorf("variation %q already exists in config %q", variation.Key, selection.Config.Key)
		}
	}
	exists, err := options.Store.VariationExists(selection.Project.Key, selection.Config.Key, variation.Key)
	if err != nil {
		return "", err
	}
	if exists {
		return "", fmt.Errorf("variation %q is already linked locally", variation.Key)
	}

	// Validation above must complete before an empty referenced file is filled
	// with interactively entered content. If wrapper creation then fails, put
	// the source back exactly as the user had it.
	target := filepath.Join(options.RepositoryRoot, filepath.FromSlash(prompt.reference.File))
	sourceChanged := !bytes.Equal(prompt.originalContent, prompt.content)
	var sourceMode os.FileMode
	if sourceChanged {
		info, err := os.Stat(target)
		if err != nil {
			return "", err
		}
		sourceMode = info.Mode().Perm()
		if err := synclocal.ReplaceFileAtomically(target, prompt.reference.File, prompt.originalContent, prompt.content, sourceMode); err != nil {
			return "", fmt.Errorf("write linked file %q: %w", prompt.reference.File, err)
		}
	}

	paths, err := options.Store.Add([]synclocal.VariationFile{{
		ProjectKey: selection.Project.Key,
		ConfigKey:  selection.Config.Key,
		Upsert:     true,
		Ref:        &prompt.reference,
		Variation:  variation,
	}})
	if err != nil {
		if sourceChanged {
			err = errors.Join(err, restoreLinkedFile(target, prompt.reference.File, prompt.content, prompt.originalContent, sourceMode))
		}
		return "", err
	}
	return paths[0], nil
}

// restoreLinkedFile rolls back a referenced file changed during a failed link.
func restoreLinkedFile(target, displayPath string, currentContent, originalContent []byte, mode os.FileMode) error {
	if err := synclocal.ReplaceFileAtomically(target, displayPath, currentContent, originalContent, mode); err != nil {
		return fmt.Errorf("restore linked file %q: %w", displayPath, err)
	}
	return nil
}

// readLinkedPrompt resolves, reads, and parses a repository-contained source file.
func readLinkedPrompt(options Options) (linkedPrompt, error) {
	reference, err := synclocal.NewReference(options.RepositoryRoot, options.WorkingDirectory, options.File, options.Format)
	if err != nil {
		return linkedPrompt{}, err
	}
	content, err := os.ReadFile(filepath.Join(options.RepositoryRoot, filepath.FromSlash(reference.File)))
	if err != nil {
		return linkedPrompt{}, fmt.Errorf("read linked file %q: %w", reference.File, err)
	}
	prompt, err := syncreference.Parse(reference.Format, content)
	if err != nil {
		return linkedPrompt{}, err
	}
	return linkedPrompt{reference: reference, parsed: prompt, originalContent: content, content: content}, nil
}

// validateDerivedKey ensures a filename-derived key is also a safe path segment.
func validateDerivedKey(key string) error {
	switch {
	case key == "":
		return fmt.Errorf("cannot derive a variation key from the linked filename")
	case key == "." || key == ".." || strings.ContainsAny(key, `/\`) || strings.IndexByte(key, 0) >= 0:
		return fmt.Errorf("linked filename produces invalid variation key %q", key)
	default:
		return nil
	}
}

// requiredValue builds a reusable non-blank form validator.
func requiredValue(label string) func(string) error {
	return func(value string) error {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("%s is required", label)
		}
		return nil
	}
}

// displayName turns a kebab- or snake-case key into a readable default name.
func displayName(key string) string {
	name := strings.NewReplacer("-", " ", "_", " ").Replace(key)
	runes := []rune(name)
	if len(runes) != 0 {
		runes[0] = unicode.ToUpper(runes[0])
	}
	return string(runes)
}

// projectChoice keeps the readable project name above its stable key.
func projectChoice(project syncapi.Project) syncinteractive.Choice[syncapi.Project] {
	return syncinteractive.Choice[syncapi.Project]{
		Title: project.Name, Description: "Key: " + project.Key, Value: project,
	}
}

// configChoice includes both the stable config identity and its mode.
func configChoice(config syncapi.Config) syncinteractive.Choice[syncapi.Config] {
	return syncinteractive.Choice[syncapi.Config]{
		Title: config.Name, Description: fmt.Sprintf("Key: %s · Mode: %s", config.Key, config.Mode), Value: config,
	}
}

// modelConfigChoices adapts model configs to interactive labels.
func modelConfigChoices(configs []syncapi.ModelConfig) []syncinteractive.Choice[syncapi.ModelConfig] {
	choices := make([]syncinteractive.Choice[syncapi.ModelConfig], 0, len(configs))
	for _, config := range configs {
		choices = append(choices, syncinteractive.Choice[syncapi.ModelConfig]{
			Title: config.Name, Description: "Key: " + config.Key, Value: config,
		})
	}
	return choices
}
