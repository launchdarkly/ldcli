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

// Options contains the dependencies and streams required by the bootstrap
// wizard.
type Options struct {
	API     Client
	Store   synclocal.Store
	Input   io.Reader
	Output  io.Writer
	Initial bool
}

type Client interface {
	Projects(search string) ([]syncapi.Project, error)
	Configs(projectKey, search string) ([]syncapi.Config, error)
	Config(projectKey, configKey string) (syncapi.Config, error)
}

type step int

const (
	selectProject step = iota
	selectConfig
	selectVariations
)

type model struct {
	api   Client
	store synclocal.Store

	step   step
	width  int
	height int

	projects        remotePicker
	configs         remotePicker
	variationList   list.Model
	variationsReady bool

	projectKey string
	config     syncapi.Config

	searching bool
	notice    string
	err       error
	canceled  bool
}

type remotePicker struct {
	list.Model
	title string
	query string
	ready bool
}

type projectItem struct {
	project syncapi.Project
}

func (i projectItem) Title() string       { return i.project.Name }
func (i projectItem) Description() string { return i.project.Key }
func (i projectItem) FilterValue() string { return i.project.Name + " " + i.project.Key }

type configItem struct {
	config syncapi.Config
}

func (i configItem) Title() string { return i.config.Name }
func (i configItem) Description() string {
	return fmt.Sprintf("%s · %s", i.config.Key, i.config.Mode)
}
func (i configItem) FilterValue() string { return i.config.Name + " " + i.config.Key }

type variationItem struct {
	variation syncdomain.Variation
	selected  bool
	existing  bool
}

func (i variationItem) Title() string       { return i.variation.Name }
func (i variationItem) FilterValue() string { return i.variation.Name + " " + i.variation.Key }
func (i variationItem) Description() string {
	if i.existing {
		return i.variation.Key + " · already synced"
	}
	return i.variation.Key
}

type remoteFetchedMsg struct {
	step       step
	projectKey string
	query      string
	items      []list.Item
}

type fetchFailedMsg struct {
	step       step
	projectKey string
	query      string
	err        error
}

type configFetchedMsg struct {
	projectKey string
	config     syncapi.Config
	existing   map[string]bool
}

type errMsg struct {
	err error
}

func newModel(options Options) model {
	return model{
		api:      options.API,
		store:    options.Store,
		step:     selectProject,
		projects: remotePicker{title: "Select a LaunchDarkly project"},
		configs:  remotePicker{title: "Select a Config"},
	}
}

func (m model) Init() tea.Cmd {
	return m.fetchProjects("")
}

func (m model) fetchProjects(search string) tea.Cmd {
	return func() tea.Msg {
		projects, err := m.api.Projects(search)
		if err != nil {
			return fetchFailedMsg{step: selectProject, query: search, err: err}
		}

		items := make([]list.Item, len(projects))
		for index, project := range projects {
			items[index] = projectItem{project: project}
		}

		return remoteFetchedMsg{
			step:  selectProject,
			query: search,
			items: items,
		}
	}
}

func (m model) fetchConfigs(search string) tea.Cmd {
	projectKey := m.projectKey

	return func() tea.Msg {
		configs, err := m.api.Configs(
			projectKey,
			search,
		)
		if err != nil {
			return fetchFailedMsg{
				step:       selectConfig,
				projectKey: projectKey,
				query:      search,
				err:        err,
			}
		}

		items := make([]list.Item, len(configs))
		for index, config := range configs {
			items[index] = configItem{config: config}
		}

		return remoteFetchedMsg{
			step:       selectConfig,
			projectKey: projectKey,
			query:      search,
			items:      items,
		}
	}
}

func (m model) fetchConfig() tea.Cmd {
	projectKey, configKey := m.projectKey, m.config.Key

	return func() tea.Msg {
		config, err := m.api.Config(
			projectKey,
			configKey,
		)
		if err != nil {
			return errMsg{err: err}
		}

		slices.SortFunc(config.Variations, func(a, b syncdomain.Variation) int {
			return cmp.Or(
				cmp.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name)),
				cmp.Compare(a.Key, b.Key),
			)
		})

		existing := make(map[string]bool, len(config.Variations))
		for _, variation := range config.Variations {
			found, err := m.store.VariationExists(projectKey, configKey, variation.Key)
			if err != nil {
				return errMsg{err: err}
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

func (m model) selectedVariationFiles() []synclocal.VariationFile {
	var resources []synclocal.VariationFile

	for _, raw := range m.variationList.Items() {
		item, ok := raw.(variationItem)
		if !ok || !item.selected {
			continue
		}

		resources = append(resources, synclocal.VariationFile{
			ProjectKey: m.projectKey,
			ConfigKey:  m.config.Key,
			Upsert:     true,
			Variation:  item.variation,
		})
	}

	return resources
}

// Run starts the interactive wizard and writes a completion summary.
func Run(options Options) error {
	if !terminalStreams(options.Input, options.Output) {
		return fmt.Errorf("interactive prompt selection requires a terminal; run this command in a terminal")
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
	in, inOK := input.(*os.File)
	out, outOK := output.(*os.File)

	return inOK && outOK &&
		term.IsTerminal(int(in.Fd())) &&
		term.IsTerminal(int(out.Fd()))
}

func writeSummary(out io.Writer, initial, noChange bool, paths []string) {
	if noChange {
		fmt.Fprintln(out, "No variations added; every variation in that Config is already synced.")
		return
	}

	action := "Added"
	if initial {
		action = "Bootstrapped"
	}
	resource := "variation file"
	if len(paths) != 1 {
		resource += "s"
	}
	fmt.Fprintf(out, "%s %d %s in %s.\n", action, len(paths), resource, syncdomain.RootDir)
}
