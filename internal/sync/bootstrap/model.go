package bootstrap

import (
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
	syncapi "github.com/launchdarkly/ldcli/internal/sync/api"
)

func (model model) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch message := message.(type) {
	case tea.WindowSizeMsg:
		model.width = message.Width
		model.height = message.Height
		model.resizeLists()

	case tea.KeyMsg:
		switch message.String() {
		case "ctrl+c":
			model.canceled = true
			return model, tea.Quit
		case "q":
			if !model.isFiltering() {
				model.canceled = true
				return model, tea.Quit
			}
		case "b":
			if !model.isFiltering() {
				return model.handleBack()
			}
		case " ":
			if model.step == selectVariations && !model.isFiltering() {
				return model, model.toggleVariation()
			}
		case "a":
			if model.step == selectVariations && !model.isFiltering() {
				return model, model.selectAllVariations()
			}
		case "enter":
			if !model.isFiltering() {
				return model.handleEnter()
			}
		}

	case projectsFetchedMsg:
		model.projects = newList(
			projectItems(message.projects),
			"Select a LaunchDarkly project",
			model.width,
			model.listHeight(),
		)
		model.projectsReady = true
		return model, nil

	case configsFetchedMsg:
		if message.projectKey != model.projectKey ||
			model.step != selectConfig {
			return model, nil
		}

		model.configs = newList(
			configItems(message.configs),
			"Select an AI Config",
			model.width,
			model.listHeight(),
		)
		model.configsReady = true
		return model, nil

	case variationsLoadedMsg:
		if message.projectKey != model.projectKey ||
			message.config.Key != model.config.Key ||
			model.step != selectVariations {
			return model, nil
		}

		model.config = message.config
		model.variations = newVariationList(
			variationItems(message.config.Variations, message.existing),
			model.width,
			model.listHeight(),
		)
		model.variationsReady = true
		return model, nil

	case errMsg:
		if message.step != model.step ||
			(message.step == selectConfig &&
				message.projectKey != model.projectKey) ||
			(message.step == selectVariations &&
				(message.projectKey != model.projectKey ||
					message.configKey != model.config.Key)) {
			return model, nil
		}

		model.err = message.err
		return model, tea.Quit
	}

	current := model.currentList()
	if current == nil {
		return model, nil
	}

	updated, command := current.Update(message)
	*current = updated

	return model, command
}

func newList(
	items []list.Item,
	title string,
	width int,
	height int,
) list.Model {
	result := list.New(items, themedDefaultDelegate(), width, height)
	result.Title = title
	configureList(&result)

	return result
}

func newVariationList(
	items []list.Item,
	width int,
	height int,
) list.Model {
	result := list.New(items, variationDelegate{}, width, height)
	result.Title = "Select prompt variations"
	configureList(&result)
	result.AdditionalShortHelpKeys = variationListHints()

	return result
}

func configureList(model *list.Model) {
	applyListTheme(model)
	model.KeyMap.Quit = key.NewBinding(
		key.WithKeys("q"),
		key.WithHelp("q", "quit"),
	)
	model.AdditionalShortHelpKeys = func() []key.Binding {
		return []key.Binding{
			key.NewBinding(
				key.WithKeys("enter"),
				key.WithHelp("enter", "select"),
			),
			key.NewBinding(
				key.WithKeys("b"),
				key.WithHelp("b", "back"),
			),
		}
	}
}

func projectItems(projects []syncapi.Project) []list.Item {
	items := make([]list.Item, len(projects))
	for index, project := range projects {
		items[index] = projectItem{project: project}
	}

	return items
}

func configItems(configs []syncapi.Config) []list.Item {
	items := make([]list.Item, len(configs))
	for index, config := range configs {
		items[index] = configItem{config: config}
	}

	return items
}

func variationItems(
	variations []syncdomain.Variation,
	existing map[string]bool,
) []list.Item {
	items := make([]list.Item, len(variations))
	for index, variation := range variations {
		items[index] = variationItem{
			variation: variation,
			existing:  existing[variation.Key],
		}
	}

	return items
}

func (model *model) resizeLists() {
	if model.projectsReady {
		model.projects.SetSize(model.width, model.listHeight())
	}
	if model.configsReady {
		model.configs.SetSize(model.width, model.listHeight())
	}
	if model.variationsReady {
		model.variations.SetSize(model.width, model.listHeight())
	}
}

func (model model) listHeight() int {
	height := model.height - 2
	if height < 5 {
		return 5
	}

	return height
}

func (model model) isFiltering() bool {
	current := model.currentList()

	return current != nil && current.FilterState() == list.Filtering
}

func (model *model) currentList() *list.Model {
	switch model.step {
	case selectProject:
		if model.projectsReady {
			return &model.projects
		}
	case selectConfig:
		if model.configsReady {
			return &model.configs
		}
	case selectVariations:
		if model.variationsReady {
			return &model.variations
		}
	}

	return nil
}

func (model model) handleBack() (tea.Model, tea.Cmd) {
	model.notice = ""

	switch model.step {
	case selectConfig:
		model.step = selectProject
		model.configsReady = false
		model.configs = list.Model{}
	case selectVariations:
		model.step = selectConfig
		model.variationsReady = false
		model.variations = list.Model{}
	}

	return model, nil
}

func (model model) handleEnter() (tea.Model, tea.Cmd) {
	model.notice = ""

	switch model.step {
	case selectProject:
		if !model.projectsReady || len(model.projects.VisibleItems()) == 0 {
			return model, nil
		}

		selected, ok := model.projects.SelectedItem().(projectItem)
		if !ok {
			return model, nil
		}

		model.projectKey = selected.project.Key
		model.configsReady = false
		model.configs = list.Model{}
		model.step = selectConfig

		return model, model.fetchConfigs()

	case selectConfig:
		if !model.configsReady || len(model.configs.VisibleItems()) == 0 {
			return model, nil
		}

		selected, ok := model.configs.SelectedItem().(configItem)
		if !ok {
			return model, nil
		}

		model.config = selected.config
		model.variationsReady = false
		model.variations = list.Model{}
		model.step = selectVariations

		return model, model.loadVariations()

	case selectVariations:
		if !model.variationsReady {
			return model, nil
		}

		selected, selectable := model.variationCounts()
		if selected == 0 {
			if selectable == 0 {
				return model, tea.Quit
			}

			model.notice = "Select at least one variation to continue."
			return model, nil
		}

		return model, tea.Quit
	}

	return model, nil
}

func (model *model) toggleVariation() tea.Cmd {
	item, ok := model.variations.SelectedItem().(variationItem)
	if !ok || item.existing {
		return nil
	}

	item.selected = !item.selected
	items := model.variations.Items()
	items[model.variations.Index()] = item
	model.notice = ""

	return model.variations.SetItems(items)
}

func (model *model) selectAllVariations() tea.Cmd {
	items := model.variations.Items()
	for index, raw := range items {
		item, ok := raw.(variationItem)
		if !ok || item.existing {
			continue
		}

		item.selected = true
		items[index] = item
	}

	model.notice = ""

	return model.variations.SetItems(items)
}

func (model model) variationCounts() (selected int, selectable int) {
	for _, raw := range model.variations.Items() {
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
