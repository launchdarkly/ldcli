package link

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
	syncapi "github.com/launchdarkly/ldcli/internal/sync/api"
	syncinteractive "github.com/launchdarkly/ldcli/internal/sync/interactive"
	synclocal "github.com/launchdarkly/ldcli/internal/sync/local"
	syncreference "github.com/launchdarkly/ldcli/internal/sync/reference"
	"github.com/launchdarkly/ldcli/internal/sync/reference/adapters"
)

// Catalog lists the LaunchDarkly resources required to link a prompt.
type Catalog interface {
	Projects() ([]syncapi.Project, error)
	Configs(projectKey string) ([]syncapi.Config, error)
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
	reference synclocal.Reference
	parsed    adapters.Prompt
	content   []byte
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

	_, _ = fmt.Fprintln(options.Output, "Loading LaunchDarkly projects...")
	projects, err := options.Catalog.Projects()
	if err != nil {
		return "", err
	}
	projectChoice, canceled, err := choose(options.Input, options.Output, "Choose a LaunchDarkly project", projectChoices(projects))
	if err != nil || canceled {
		return "", err
	}
	project := projectChoice.value

	_, _ = fmt.Fprintln(options.Output, "Loading configs...")
	configs, err := options.Catalog.Configs(project.Key)
	if err != nil {
		return "", err
	}
	configs = configsForPrompt(configs, prompt.parsed)
	configChoice, canceled, err := choose(options.Input, options.Output, "Choose a config", configChoices(configs))
	if err != nil || canceled {
		return "", err
	}
	config := configChoice.value

	_, _ = fmt.Fprintln(options.Output, "Loading model configs...")
	modelConfigs, err := options.Catalog.ModelConfigs(project.Key)
	if err != nil {
		return "", err
	}
	modelChoice, canceled, err := choose(options.Input, options.Output, "Choose a model config", modelConfigChoices(modelConfigs))
	if err != nil || canceled {
		return "", err
	}

	defaultKey := strings.TrimSuffix(filepath.Base(prompt.reference.File), filepath.Ext(prompt.reference.File))
	reader := bufio.NewReader(options.Input)
	key := prompt.parsed.Key
	if key == "" {
		key, err = askValue(reader, options.Output, "Variation key", defaultKey)
		if err != nil {
			return "", err
		}
	}
	name := prompt.parsed.Name
	if name == "" {
		name, err = askValue(reader, options.Output, "Variation name", displayName(key))
		if err != nil {
			return "", err
		}
	}
	prompt, err = addMissingPromptContent(options, reader, prompt, config.Mode, key, name)
	if err != nil {
		return "", err
	}

	return createLinkedPrompt(options, Selection{
		Project:     project,
		Config:      config,
		ModelConfig: modelChoice.value,
		Key:         key,
		Name:        name,
	}, prompt)
}

// addMissingPromptContent asks for content only when the referenced format did
// not provide any messages, then updates the referenced file before linking it.
func addMissingPromptContent(
	options Options,
	reader *bufio.Reader,
	prompt linkedPrompt,
	mode syncdomain.VariationMode,
	key string,
	name string,
) (linkedPrompt, error) {
	if len(prompt.parsed.Messages) != 0 {
		return prompt, nil
	}

	content, err := askValue(reader, options.Output, "Prompt content", "")
	if err != nil {
		return linkedPrompt{}, err
	}
	if content == "" {
		return linkedPrompt{}, fmt.Errorf("prompt content is required")
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

	target := filepath.Join(options.RepositoryRoot, filepath.FromSlash(prompt.reference.File))
	info, err := os.Stat(target)
	if err != nil {
		return linkedPrompt{}, err
	}
	if err := os.WriteFile(target, rendered, info.Mode().Perm()); err != nil {
		return linkedPrompt{}, fmt.Errorf("write linked file %q: %w", prompt.reference.File, err)
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

	paths, err := options.Store.Add([]synclocal.VariationFile{{
		ProjectKey: selection.Project.Key,
		ConfigKey:  selection.Config.Key,
		Upsert:     true,
		Ref:        &prompt.reference,
		Variation:  variation,
	}})
	if err != nil {
		return "", err
	}
	return paths[0], nil
}

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
	return linkedPrompt{reference: reference, parsed: prompt, content: content}, nil
}

func configsForPrompt(configs []syncapi.Config, prompt adapters.Prompt) []syncapi.Config {
	if prompt.Mode == "" {
		return configs
	}
	result := make([]syncapi.Config, 0, len(configs))
	for _, config := range configs {
		if string(config.Mode) == string(prompt.Mode) {
			result = append(result, config)
		}
	}
	return result
}

func askValue(input *bufio.Reader, output io.Writer, label, defaultValue string) (string, error) {
	prompt := label + ": "
	if defaultValue != "" {
		prompt = fmt.Sprintf("%s [%s]: ", label, defaultValue)
	}
	if _, err := fmt.Fprint(output, prompt); err != nil {
		return "", err
	}
	value, err := input.ReadString('\n')
	if err != nil && err != io.EOF {
		return "", fmt.Errorf("read %s: %w", strings.ToLower(label), err)
	}
	value = strings.TrimSpace(value)
	if value == "" {
		value = defaultValue
	}
	return value, nil
}

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

func displayName(key string) string {
	name := strings.NewReplacer("-", " ", "_", " ").Replace(key)
	runes := []rune(name)
	if len(runes) != 0 {
		runes[0] = unicode.ToUpper(runes[0])
	}
	return string(runes)
}
