package bootstrap

import (
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
)

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.resizeLists()

	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c":
			m.canceled = true
			return m, tea.Quit
		case "q":
			if !m.isFiltering() {
				m.canceled = true
				return m, tea.Quit
			}
		case "b":
			if !m.isFiltering() {
				return m.handleBack()
			}
		case " ":
			if m.step == selectVariations && !m.isFiltering() {
				return m, m.toggleVariation()
			}
		case "a":
			if m.step == selectVariations && !m.isFiltering() {
				return m, m.selectAllVariations()
			}
		case "enter":
			if m.isFiltering() {
				switch m.step {
				case selectProject, selectConfig:
					return m.submitServerSearch(msg)
				}
			}
			return m.handleEnter()
		}

	case remoteFetchedMsg:
		picker := m.picker(msg.step)
		if picker == nil ||
			msg.step != m.step ||
			(msg.step == selectConfig && msg.projectKey != m.projectKey) ||
			(picker.ready && msg.query != picker.FilterValue()) {
			return m, nil
		}
		m.searching = false
		picker.query = msg.query
		if !picker.ready {
			picker.ready = true
			picker.Model = newServerList(msg.items, picker.title, m.width, m.listHeight())
			return m, nil
		}
		replaceServerItems(&picker.Model, msg.items, msg.query)
		return m, nil

	case fetchFailedMsg:
		if msg.step != m.step ||
			(msg.step == selectConfig && msg.projectKey != m.projectKey) {
			return m, nil
		}
		if remote := m.remoteList(); remote != nil && msg.query != remote.FilterValue() {
			return m, nil
		}
		m.err = msg.err
		return m, tea.Quit

	case list.FilterMatchesMsg:
		if m.step == selectProject || m.step == selectConfig {
			return m, nil
		}

	case configFetchedMsg:
		if msg.projectKey != m.projectKey ||
			msg.config.Key != m.config.Key ||
			m.step != selectVariations {
			return m, nil
		}
		m.config = msg.config
		m.variationsReady = true
		items := make([]list.Item, len(msg.config.Variations))
		for index, variation := range msg.config.Variations {
			items[index] = variationItem{
				variation: variation,
				existing:  msg.existing[variation.Key],
			}
		}
		m.variationList = newVariationList(items, m.width, m.listHeight())
		return m, nil

	case errMsg:
		m.err = msg.err
		return m, tea.Quit

	}

	if remote := m.remoteList(); remote != nil {
		before := remote.FilterValue()
		updated, cmd := remote.Update(msg)
		*remote = updated
		if before != "" && remote.FilterValue() == "" {
			m.searching = true
			return m, tea.Batch(cmd, m.fetchRemote(""))
		}
		return m, cmd
	}
	if m.step == selectVariations && m.variationsReady {
		var cmd tea.Cmd
		m.variationList, cmd = m.variationList.Update(msg)
		return m, cmd
	}
	return m, nil
}

func newServerList(items []list.Item, title string, width, height int) list.Model {
	delegate := themedDefaultDelegate()
	result := list.New(items, delegate, width, height)
	result.Title = title
	result.Filter = serverFilter
	configureList(&result)

	return result
}

func newVariationList(items []list.Item, width, height int) list.Model {
	result := list.New(items, variationDelegate{}, width, height)
	result.Title = "Select variations"
	configureList(&result)
	result.AdditionalShortHelpKeys = variationListHints()

	return result
}

func serverFilter(_ string, targets []string) []list.Rank {
	ranks := make([]list.Rank, len(targets))
	for index := range targets {
		ranks[index] = list.Rank{Index: index}
	}
	return ranks
}

func replaceServerItems(model *list.Model, items []list.Item, query string) {
	_ = model.SetItems(items)
	if query != "" {
		model.SetFilterText(query)
	}
}

func configureList(model *list.Model) {
	applyListTheme(model)
	model.KeyMap.Quit = key.NewBinding(key.WithKeys("q"), key.WithHelp("q", "quit"))
	model.AdditionalShortHelpKeys = func() []key.Binding {
		return []key.Binding{
			key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "select")),
			key.NewBinding(key.WithKeys("b"), key.WithHelp("b", "back")),
		}
	}
}

func (m *model) resizeLists() {
	if m.projects.ready {
		m.projects.SetSize(m.width, m.listHeight())
	}
	if m.configs.ready {
		m.configs.SetSize(m.width, m.listHeight())
	}
	if m.variationsReady {
		m.variationList.SetSize(m.width, m.listHeight())
	}
}

func (m model) listHeight() int {
	height := m.height - 2
	if height < 5 {
		return 5
	}
	return height
}

func (m model) isFiltering() bool {
	if remote := m.remoteList(); remote != nil {
		return remote.FilterState() == list.Filtering
	}
	return m.step == selectVariations &&
		m.variationsReady &&
		m.variationList.FilterState() == list.Filtering
}

func (m model) handleBack() (tea.Model, tea.Cmd) {
	m.notice = ""
	m.searching = false
	switch m.step {
	case selectConfig:
		m.step = selectProject
		m.configs = remotePicker{title: "Select a Config"}
	case selectVariations:
		m.step = selectConfig
		m.variationsReady = false
		m.variationList = list.Model{}
	}
	return m, nil
}

func (m model) handleEnter() (tea.Model, tea.Cmd) {
	m.notice = ""
	switch m.step {
	case selectProject:
		if !m.projects.ready ||
			m.projects.FilterValue() != m.projects.query ||
			len(m.projects.Items()) == 0 {
			return m, nil
		}
		selected, ok := m.projects.SelectedItem().(projectItem)
		if !ok {
			return m, nil
		}
		m.projectKey = selected.project.Key
		m.configs = remotePicker{title: "Select a Config"}
		m.step = selectConfig
		m.searching = false
		return m, m.fetchConfigs("")

	case selectConfig:
		if !m.configs.ready ||
			m.configs.FilterValue() != m.configs.query ||
			len(m.configs.Items()) == 0 {
			return m, nil
		}
		selected, ok := m.configs.SelectedItem().(configItem)
		if !ok {
			return m, nil
		}
		m.config = selected.config
		m.variationsReady = false
		m.variationList = list.Model{}
		m.step = selectVariations
		m.searching = false
		return m, m.fetchConfig()

	case selectVariations:
		if !m.variationsReady {
			return m, nil
		}
		selected, selectable := m.variationCounts()
		if selected == 0 {
			if selectable == 0 {
				return m, tea.Quit
			}
			m.notice = "Select at least one variation to continue."
			return m, nil
		}
		return m, tea.Quit
	}
	return m, nil
}

func (m *model) toggleVariation() tea.Cmd {
	item, ok := m.variationList.SelectedItem().(variationItem)
	if !ok || item.existing {
		return nil
	}

	item.selected = !item.selected
	items := m.variationList.Items()
	for index, raw := range items {
		candidate, ok := raw.(variationItem)
		if ok && candidate.variation.Key == item.variation.Key {
			items[index] = item
			break
		}
	}
	m.notice = ""
	return m.variationList.SetItems(items)
}

func (m *model) selectAllVariations() tea.Cmd {
	items := m.variationList.Items()
	for index, raw := range items {
		item, ok := raw.(variationItem)
		if !ok || item.existing {
			continue
		}
		item.selected = true
		items[index] = item
	}
	m.notice = ""
	return m.variationList.SetItems(items)
}

func (m model) variationCounts() (selected, selectable int) {
	for _, raw := range m.variationList.Items() {
		item, ok := raw.(variationItem)
		if !ok {
			continue
		}
		if item.selected {
			selected++
		}
		if !item.existing {
			selectable++
		}
	}
	return selected, selectable
}

func (m model) submitServerSearch(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	remote := m.remoteList()
	if remote == nil {
		return m, nil
	}
	updated, inputCmd := remote.Update(msg)
	*remote = updated
	finishSubmittedFilter(remote)
	m.searching = true
	return m, tea.Batch(inputCmd, m.fetchRemote(remote.FilterValue()))
}

func finishSubmittedFilter(model *list.Model) {
	if model.FilterValue() == "" {
		model.SetFilterState(list.Unfiltered)
		return
	}
	model.SetFilterState(list.FilterApplied)
}

func (m *model) remoteList() *list.Model {
	picker := m.picker(m.step)
	if picker == nil || !picker.ready {
		return nil
	}
	return &picker.Model
}

func (m model) fetchRemote(search string) tea.Cmd {
	if m.step == selectProject {
		return m.fetchProjects(search)
	}
	return m.fetchConfigs(search)
}

func (m *model) picker(target step) *remotePicker {
	switch target {
	case selectProject:
		return &m.projects
	case selectConfig:
		return &m.configs
	default:
		return nil
	}
}
