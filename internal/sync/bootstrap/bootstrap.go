package bootstrap

import (
	"cmp"
	"fmt"
	"io"
	"path"
	"slices"
	"strings"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
	syncapi "github.com/launchdarkly/ldcli/internal/sync/api"
	syncinteractive "github.com/launchdarkly/ldcli/internal/sync/interactive"
	synclocal "github.com/launchdarkly/ldcli/internal/sync/local"
	syncmanifest "github.com/launchdarkly/ldcli/internal/sync/manifest"
)

// Catalog lists projects and configs available for bootstrap.
type Catalog interface {
	Projects() ([]syncapi.Project, error)
	Configs(projectKey string) ([]syncapi.Config, error)
}

// Options contains the dependencies and streams for one bootstrap flow.
type Options struct {
	Catalog  Catalog
	Store    synclocal.Store
	Manifest syncmanifest.Store
	Input    io.Reader
	Output   io.Writer
	Initial  bool
	DryRun   bool
}

type step int

const (
	selectProject step = iota
	selectConfig
	selectVariations
)

type model struct {
	catalog Catalog
	store   synclocal.Store
	dryRun  bool

	step   step
	width  int
	height int

	projects        list.Model
	projectsReady   bool
	configs         list.Model
	configsReady    bool
	variations      list.Model
	variationsReady bool

	projectKey string
	config     syncapi.Config

	notice   string
	err      error
	canceled bool
}

type projectItem struct {
	project syncapi.Project
}

func (item projectItem) Title() string {
	return item.project.Name
}

func (item projectItem) Description() string {
	return item.project.Key
}

func (item projectItem) FilterValue() string {
	return item.project.Name + " " + item.project.Key
}

type configItem struct {
	config syncapi.Config
}

func (item configItem) Title() string {
	return item.config.Name
}

func (item configItem) Description() string {
	return fmt.Sprintf("%s · %s", item.config.Key, item.config.Mode)
}

func (item configItem) FilterValue() string {
	return item.config.Name + " " + item.config.Key
}

type variationItem struct {
	variation syncdomain.Variation
	selected  bool
	existing  bool
}

func (item variationItem) Title() string {
	return item.variation.Name
}

func (item variationItem) Description() string {
	if item.existing {
		return item.variation.Key + " · already synced"
	}

	return item.variation.Key
}

func (item variationItem) FilterValue() string {
	return item.variation.Name + " " + item.variation.Key
}

type projectsFetchedMsg struct {
	projects []syncapi.Project
}

type configsFetchedMsg struct {
	projectKey string
	configs    []syncapi.Config
}

type variationsLoadedMsg struct {
	projectKey string
	config     syncapi.Config
	existing   map[string]bool
}

type errMsg struct {
	step       step
	projectKey string
	configKey  string
	err        error
}

func newModel(options Options) model {
	return model{
		catalog: options.Catalog,
		store:   options.Store,
		dryRun:  options.DryRun,
		step:    selectProject,
	}
}

func (state model) Init() tea.Cmd {
	return state.fetchProjects()
}

func (state model) fetchProjects() tea.Cmd {
	return func() tea.Msg {
		projects, err := state.catalog.Projects()
		if err != nil {
			return errMsg{step: selectProject, err: err}
		}

		return projectsFetchedMsg{projects: projects}
	}
}

func (state model) fetchConfigs() tea.Cmd {
	projectKey := state.projectKey

	return func() tea.Msg {
		configs, err := state.catalog.Configs(projectKey)
		if err != nil {
			return errMsg{
				step:       selectConfig,
				projectKey: projectKey,
				err:        err,
			}
		}

		return configsFetchedMsg{
			projectKey: projectKey,
			configs:    configs,
		}
	}
}

func (state model) loadVariations() tea.Cmd {
	projectKey := state.projectKey
	config := state.config

	return func() tea.Msg {
		slices.SortFunc(config.Variations, func(left, right syncdomain.Variation) int {
			return cmp.Or(
				cmp.Compare(strings.ToLower(left.Name), strings.ToLower(right.Name)),
				cmp.Compare(left.Key, right.Key),
			)
		})

		existing := make(map[string]bool, len(config.Variations))
		for _, variation := range config.Variations {
			found, err := state.store.VariationExists(
				projectKey,
				config.Key,
				variation.Key,
			)
			if err != nil {
				return errMsg{
					step:       selectVariations,
					projectKey: projectKey,
					configKey:  config.Key,
					err:        err,
				}
			}
			existing[variation.Key] = found
		}

		return variationsLoadedMsg{
			projectKey: projectKey,
			config:     config,
			existing:   existing,
		}
	}
}

func (state model) selectedVariationFiles() []synclocal.VariationFile {
	var files []synclocal.VariationFile

	for _, raw := range state.variations.Items() {
		item, ok := raw.(variationItem)
		if !ok || !item.selected {
			continue
		}

		files = append(files, synclocal.VariationFile{
			ProjectKey: state.projectKey,
			ConfigKey:  state.config.Key,
			Upsert:     true,
			Variation:  item.variation,
		})
	}

	return files
}

// Run interactively selects prompt variations and writes their local wrappers.
func Run(options Options) error {
	if !syncinteractive.StreamsAreTerminal(options.Input, options.Output) {
		return fmt.Errorf(
			"interactive prompt selection requires a terminal; run this command in a terminal",
		)
	}

	program := tea.NewProgram(
		newModel(options),
		tea.WithAltScreen(),
		tea.WithInput(options.Input),
		tea.WithOutput(options.Output),
	)
	final, err := program.Run()
	if err != nil {
		return err
	}

	result, ok := final.(model)
	if !ok {
		return fmt.Errorf("bootstrap returned an unexpected model")
	}
	if result.err != nil || result.canceled {
		return result.err
	}

	files := result.selectedVariationFiles()
	return finishSelection(options, files)
}

func finishSelection(options Options, files []synclocal.VariationFile) error {
	if len(files) == 0 {
		writeNoChangeSummary(options.Output, options.DryRun)
		return nil
	}
	for _, file := range files {
		if err := syncdomain.ValidateDirectAPIVariation(file.Variation); err != nil {
			return err
		}
	}
	if options.DryRun {
		previews, err := options.Store.RenderVariations(files)
		if err != nil {
			return err
		}
		writePreviews(options.Output, previews)
		return nil
	}

	manifest, _, err := options.Manifest.Load()
	if err != nil {
		return err
	}
	for _, file := range files {
		lookupKey := file.ConfigKey + "/" + file.Variation.Key
		fingerprint, err := syncdomain.FingerprintVariation(file.ProjectKey, lookupKey, file.Variation)
		if err != nil {
			return err
		}
		manifest.SetFingerprint(syncdomain.ResourceID{
			Kind: syncdomain.KindVariation, ProjectKey: file.ProjectKey, LookupKey: lookupKey,
		}, fingerprint)
	}

	var paths []string
	if options.Initial {
		paths, err = options.Store.Bootstrap(files)
	} else {
		paths, err = options.Store.Add(files)
	}
	if err != nil {
		return err
	}
	if err := options.Manifest.Write(manifest); err != nil {
		return err
	}

	writeSummary(options.Output, options.Initial, len(paths))

	return nil
}

func writeNoChangeSummary(output io.Writer, dryRun bool) {
	message := "No variations added; every variation in that config is already synced."
	if dryRun {
		message = "No variations would be added; every variation in that config is already synced."
	}
	_, _ = fmt.Fprintln(output, message)
}

func writeSummary(output io.Writer, initial bool, count int) {
	verb := "Added"
	if initial {
		verb = "Bootstrapped"
	}

	resource := "variation file"
	if count != 1 {
		resource += "s"
	}

	_, _ = fmt.Fprintf(
		output,
		"%s %d %s in %s.\n",
		verb,
		count,
		resource,
		syncdomain.RootDir,
	)
}

func writePreviews(output io.Writer, previews []synclocal.RenderedVariationFile) {
	for index, preview := range previews {
		if index != 0 {
			_, _ = fmt.Fprintln(output)
		}
		_, _ = fmt.Fprintln(output, "============================================================")
		_, _ = fmt.Fprintf(
			output,
			"File %d of %d\nWould create: %s\n",
			index+1,
			len(previews),
			path.Join(syncdomain.RootDir, preview.Path),
		)
		_, _ = fmt.Fprintln(output, "------------------------------------------------------------")
		_, _ = output.Write(preview.Content)
		if len(preview.Content) == 0 || preview.Content[len(preview.Content)-1] != '\n' {
			_, _ = fmt.Fprintln(output)
		}
		_, _ = fmt.Fprintln(output, "============================================================")
	}
}
