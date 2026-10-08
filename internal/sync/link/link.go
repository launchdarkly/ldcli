// Package link creates a variation whose prompt lives in an external file in
// the repository. The variation file stores a reference to that file.
package link

import (
	"bytes"
	"cmp"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
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

// Catalog finds the project, the config, and the model config of a new variation.
type Catalog interface {
	syncinteractive.ProjectSearcher
	syncinteractive.ConfigSearcher
	Config(projectKey, configKey string) (syncapi.Config, error)
	ModelConfigs(projectKey string) ([]syncapi.ModelConfig, error)
}

// Options are the dependencies and the input of one link.
type Options struct {
	Catalog          Catalog
	Store            synclocal.Store
	RepositoryRoot   string
	WorkingDirectory string
	File             string
	Format           string
	Input            io.Reader
	Output           io.Writer
	// Target is the new variation. If it is nil, Run asks the user.
	Target  *Target
	NoInput bool
}

// Target is a new variation that the user named with flags. Name and Content
// replace values that the linked file does not have.
type Target struct {
	Variation      syncdomain.ResourceID
	ModelConfigKey string
	Name           string
	Content        string
}

// Selection is the complete destination of a new linked variation.
type Selection struct {
	Project     syncapi.Project
	Config      syncapi.Config
	ModelConfig syncapi.ModelConfig
	Key         string
	Name        string
}

// linkedPrompt is the external file. If the file has no prompt, content is
// the new prompt that link writes to the file.
type linkedPrompt struct {
	reference       synclocal.Reference
	parsed          adapters.Prompt
	originalContent []byte
	content         []byte
}

// Run creates the linked variation file and returns its path. The path is
// empty when the user cancels.
func Run(options Options) (string, error) {
	prompt, err := readLinkedPrompt(options)
	if err != nil {
		return "", err
	}

	var selection Selection
	var content string
	if options.Target != nil {
		selection, content, err = targetSelection(options, prompt, *options.Target)
	} else {
		if err := syncinteractive.RequireTerminal(
			options.Input, options.Output, options.NoInput, "--to and --model-config-key", "prompt linking",
		); err != nil {
			return "", err
		}
		var canceled bool
		selection, content, canceled, err = promptForSelection(options, prompt)
		if canceled {
			return "", nil
		}
	}
	if err != nil {
		return "", err
	}

	prompt, err = addMissingPromptContent(prompt, selection.Config.Mode, selection.Key, selection.Name, content)
	if err != nil {
		return "", err
	}
	return createLinkedPrompt(options, selection, prompt)
}

// targetSelection completes the destination that the user named with flags.
// It returns the prompt content to add when the linked file has none.
func targetSelection(options Options, prompt linkedPrompt, target Target) (Selection, string, error) {
	configKey, variationKey, err := target.Variation.VariationKeys()
	if err != nil {
		return Selection{}, "", fmt.Errorf("link destination must be a variation: %w", err)
	}
	hasContent := len(prompt.parsed.Messages) != 0
	switch {
	case prompt.parsed.Key != "" && prompt.parsed.Key != variationKey:
		return Selection{}, "", fmt.Errorf("linked file key %q does not match destination key %q", prompt.parsed.Key, variationKey)
	case target.Name != "" && strings.TrimSpace(target.Name) == "":
		return Selection{}, "", errors.New("--name cannot be blank")
	case target.Content != "" && strings.TrimSpace(target.Content) == "":
		return Selection{}, "", errors.New("--content cannot be blank")
	case prompt.parsed.Name != "" && target.Name != "" && prompt.parsed.Name != target.Name:
		return Selection{}, "", fmt.Errorf("linked file name %q does not match --name %q", prompt.parsed.Name, target.Name)
	case !hasContent && target.Content == "":
		return Selection{}, "", errors.New("--content is required when the linked file has no prompt content")
	case hasContent && target.Content != "":
		return Selection{}, "", errors.New("--content cannot be used when the linked file already has prompt content")
	}

	projectKey := target.Variation.ProjectKey
	config, err := options.Catalog.Config(projectKey, configKey)
	if err != nil {
		return Selection{}, "", err
	}
	modelConfigs, err := options.Catalog.ModelConfigs(projectKey)
	if err != nil {
		return Selection{}, "", err
	}
	index := slices.IndexFunc(modelConfigs, func(candidate syncapi.ModelConfig) bool {
		return candidate.Key == target.ModelConfigKey
	})
	if index < 0 {
		return Selection{}, "", fmt.Errorf("model config %q was not found", target.ModelConfigKey)
	}

	name := cmp.Or(prompt.parsed.Name, target.Name, displayName(variationKey))
	return Selection{
		Project:     syncapi.Project{Key: projectKey},
		Config:      config,
		ModelConfig: modelConfigs[index],
		Key:         variationKey,
		Name:        name,
	}, target.Content, nil
}

// promptForSelection asks the user for the destination, and for each value
// that the linked file does not have. The bool result is true when the user
// cancels.
func promptForSelection(options Options, prompt linkedPrompt) (Selection, string, bool, error) {
	project, canceled, err := syncinteractive.SelectProject(options.Input, options.Output, options.Catalog)
	if err != nil || canceled {
		return Selection{}, "", canceled, err
	}
	var modes []syncdomain.VariationMode
	if prompt.parsed.Mode != "" {
		modes = []syncdomain.VariationMode{prompt.parsed.Mode}
	}
	config, canceled, err := syncinteractive.SelectConfig(options.Input, options.Output, options.Catalog, project.Key, modes)
	if err != nil || canceled {
		return Selection{}, "", canceled, err
	}

	_ = syncconsole.New(options.Output).Line("Loading model configs...")
	modelConfigs, err := options.Catalog.ModelConfigs(project.Key)
	if err != nil {
		return Selection{}, "", false, err
	}
	choices := make([]syncinteractive.Choice[syncapi.ModelConfig], 0, len(modelConfigs))
	for _, modelConfig := range modelConfigs {
		choices = append(choices, syncinteractive.Choice[syncapi.ModelConfig]{
			Title: modelConfig.Name, Description: "Key: " + modelConfig.Key, Value: modelConfig,
		})
	}
	modelConfig, canceled, err := syncinteractive.Select(options.Input, options.Output, "Choose a model config", choices)
	if err != nil || canceled {
		return Selection{}, "", canceled, err
	}

	// Ask only for the values that the linked file does not have.
	key, name, content := prompt.parsed.Key, prompt.parsed.Name, ""
	var fields []huh.Field
	if key == "" {
		key = strings.TrimSuffix(filepath.Base(prompt.reference.File), filepath.Ext(prompt.reference.File))
		fields = append(fields, huh.NewInput().Title("Variation key").Value(&key).Validate(validateDerivedKey))
	}
	if name == "" {
		name = displayName(key)
		fields = append(fields, huh.NewInput().Title("Variation name").Value(&name).Validate(requiredValue("variation name")))
	}
	if len(prompt.parsed.Messages) == 0 {
		fields = append(fields, huh.NewText().Title("Prompt content").Lines(8).Value(&content).
			Validate(requiredValue("prompt content")))
	}
	if len(fields) != 0 {
		if canceled, err := syncinteractive.RunForm(options.Input, options.Output, fields...); err != nil || canceled {
			return Selection{}, "", canceled, err
		}
	}

	return Selection{Project: project, Config: config, ModelConfig: modelConfig, Key: key, Name: name}, content, false, nil
}

// addMissingPromptContent renders content as the new text of a linked file
// that has no prompt. It does not write the file. createLinkedPrompt writes
// it after every check passes.
func addMissingPromptContent(
	prompt linkedPrompt,
	mode syncdomain.VariationMode,
	key, name, content string,
) (linkedPrompt, error) {
	if len(prompt.parsed.Messages) != 0 {
		return prompt, nil
	}
	variation := syncdomain.Variation{Mode: mode, Key: key, Name: name}
	if mode == syncdomain.VariationModeAgent {
		variation.Instructions = content
	} else {
		variation.Messages = []syncdomain.Message{{Role: syncdomain.RoleSystem, Content: content}}
	}
	rendered, err := syncreference.Render(prompt.reference.Format, variation)
	if err != nil {
		return linkedPrompt{}, err
	}
	prompt.content = rendered
	return prompt, nil
}

// createLinkedPrompt checks the destination, writes new prompt content to the
// linked file, and creates the variation file. If the variation file fails,
// it restores the linked file.
func createLinkedPrompt(options Options, selection Selection, prompt linkedPrompt) (string, error) {
	variation := syncdomain.Variation{
		Mode:               selection.Config.Mode,
		Key:                selection.Key,
		Name:               selection.Name,
		ModelConfigKey:     selection.ModelConfig.Key,
		ModelConfigVersion: selection.ModelConfig.Version,
		Model:              selection.ModelConfig.VariationModel(),
	}
	if err := syncreference.ApplyToVariation(prompt.reference.Format, prompt.content, &variation); err != nil {
		return "", err
	}

	// Every check must pass before the linked file changes.
	if variation.Mode != selection.Config.Mode {
		return "", fmt.Errorf("referenced prompt mode %q does not match config mode %q", variation.Mode, selection.Config.Mode)
	}
	if err := validateDerivedKey(variation.Key); err != nil {
		return "", err
	}
	if err := variation.Validate(); err != nil {
		return "", fmt.Errorf("variation %q: %w", variation.Key, err)
	}
	if slices.ContainsFunc(selection.Config.Variations, func(existing syncdomain.Variation) bool {
		return existing.Key == variation.Key
	}) {
		return "", fmt.Errorf("variation %q already exists in config %q", variation.Key, selection.Config.Key)
	}
	exists, err := options.Store.VariationExists(selection.Project.Key, selection.Config.Key, variation.Key)
	if err != nil {
		return "", err
	}
	if exists {
		return "", fmt.Errorf("variation %q is already linked locally", variation.Key)
	}

	source := filepath.Join(options.RepositoryRoot, filepath.FromSlash(prompt.reference.File))
	sourceChanged := !bytes.Equal(prompt.originalContent, prompt.content)
	var sourceMode os.FileMode
	if sourceChanged {
		info, err := os.Stat(source)
		if err != nil {
			return "", err
		}
		sourceMode = info.Mode().Perm()
		err = synclocal.ReplaceFileAtomically(source, prompt.reference.File, prompt.originalContent, prompt.content, sourceMode)
		if err != nil {
			return "", fmt.Errorf("write linked file %q: %w", prompt.reference.File, err)
		}
	}

	creation, err := options.Store.Add([]synclocal.VariationFile{{
		ProjectKey: selection.Project.Key,
		ConfigKey:  selection.Config.Key,
		Upsert:     true,
		Ref:        &prompt.reference,
		Variation:  variation,
	}})
	if err != nil {
		if sourceChanged {
			restoreErr := synclocal.ReplaceFileAtomically(
				source, prompt.reference.File, prompt.content, prompt.originalContent, sourceMode,
			)
			if restoreErr != nil {
				err = errors.Join(err, fmt.Errorf("restore linked file %q: %w", prompt.reference.File, restoreErr))
			}
		}
		return "", err
	}
	return creation.VariationPaths[0], nil
}

// readLinkedPrompt reads and parses the external file.
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

// validateDerivedKey makes sure that a key from a file name is a safe key.
func validateDerivedKey(key string) error {
	if key == "" {
		return errors.New("cannot derive a variation key from the linked filename")
	}
	if syncdomain.ValidateKey(key) != nil {
		return fmt.Errorf("linked filename produces invalid variation key %q", key)
	}
	return nil
}

// requiredValue returns a form validator that rejects a blank value.
func requiredValue(label string) func(string) error {
	return func(value string) error {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("%s is required", label)
		}
		return nil
	}
}

// displayName converts a key such as "support-agent" to "Support agent".
func displayName(key string) string {
	runes := []rune(strings.NewReplacer("-", " ", "_", " ").Replace(key))
	if len(runes) != 0 {
		runes[0] = unicode.ToUpper(runes[0])
	}
	return string(runes)
}
