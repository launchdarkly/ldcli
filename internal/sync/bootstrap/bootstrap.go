package bootstrap

import (
	"cmp"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"golang.org/x/term"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
	syncapi "github.com/launchdarkly/ldcli/internal/sync/api"
	synclocal "github.com/launchdarkly/ldcli/internal/sync/local"
)

type Catalog interface {
	Projects() ([]syncapi.Project, error)
	Configs(projectKey string) ([]syncapi.Config, error)
	Config(projectKey string, configKey string) (syncapi.Config, error)
}

type Options struct {
	Catalog Catalog
	Store   synclocal.Store
	Input   io.Reader
	Output  io.Writer
	Initial bool
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

type configFetchedMsg struct {
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
		step:    selectProject,
	}
}

func (model model) Init() tea.Cmd {
	return model.fetchProjects()
}

func (model model) fetchProjects() tea.Cmd {
	return func() tea.Msg {
		projects, err := model.catalog.Projects()
		if err != nil {
			return errMsg{step: selectProject, err: err}
		}

		return projectsFetchedMsg{projects: projects}
	}
}

func (model model) fetchConfigs() tea.Cmd {
	projectKey := model.projectKey

	return func() tea.Msg {
		configs, err := model.catalog.Configs(projectKey)
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

func (model model) fetchConfig() tea.Cmd {
	projectKey := model.projectKey
	configKey := model.config.Key

	return func() tea.Msg {
		config, err := model.catalog.Config(projectKey, configKey)
		if err != nil {
			return errMsg{
				step:       selectVariations,
				projectKey: projectKey,
				configKey:  configKey,
				err:        err,
			}
		}

		slices.SortFunc(config.Variations, func(left, right syncdomain.Variation) int {
			return cmp.Or(
				cmp.Compare(strings.ToLower(left.Name), strings.ToLower(right.Name)),
				cmp.Compare(left.Key, right.Key),
			)
		})

		existing := make(map[string]bool, len(config.Variations))
		for _, variation := range config.Variations {
			found, err := model.store.VariationExists(
				projectKey,
				configKey,
				variation.Key,
			)
			if err != nil {
				return errMsg{
					step:       selectVariations,
					projectKey: projectKey,
					configKey:  configKey,
					err:        err,
				}
			}
			existing[variation.Key] = found
		}

		return configFetchedMsg{
			projectKey: projectKey,
			config:     config,
			existing:   existing,
		}
	}
}

func (model model) selectedVariationFiles() []synclocal.VariationFile {
	var files []synclocal.VariationFile

	for _, raw := range model.variations.Items() {
		item, ok := raw.(variationItem)
		if !ok || !item.selected {
			continue
		}

		files = append(files, synclocal.VariationFile{
			ProjectKey: model.projectKey,
			ConfigKey:  model.config.Key,
			Upsert:     true,
			Variation:  item.variation,
		})
	}

	return files
}

func Run(options Options) error {
	if !terminalStreams(options.Input, options.Output) {
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
	if len(files) == 0 {
		writeSummary(options.Output, options.Initial, true, nil)
		return nil
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

	writeSummary(options.Output, options.Initial, false, paths)

	return nil
}

func terminalStreams(input io.Reader, output io.Writer) bool {
	in, inputIsFile := input.(*os.File)
	out, outputIsFile := output.(*os.File)

	return inputIsFile &&
		outputIsFile &&
		term.IsTerminal(int(in.Fd())) &&
		term.IsTerminal(int(out.Fd()))
}

func writeSummary(
	output io.Writer,
	initial bool,
	noChange bool,
	paths []string,
) {
	if noChange {
		_, _ = fmt.Fprintln(
			output,
			"No variations added; every variation in that AI Config is already synced.",
		)
		return
	}

	verb := "Added"
	if initial {
		verb = "Bootstrapped"
	}

	resource := "variation file"
	if len(paths) != 1 {
		resource += "s"
	}

	_, _ = fmt.Fprintf(
		output,
		"%s %d %s in %s.\n",
		verb,
		len(paths),
		resource,
		syncdomain.RootDir,
	)
}
