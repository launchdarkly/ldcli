package interactive

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
)

const (
	choiceSeparator = "\n"
	selectionHeight = 20
)

var formAccent = lipgloss.AdaptiveColor{Light: "24", Dark: "67"}

// Choice is one labeled value in an interactive selection.
type Choice[T any] struct {
	Title       string
	Description string
	Value       T
}

// SearchOptions describes a reusable server-filtered, incrementally paged selector.
type SearchOptions[T any] struct {
	Input             io.Reader
	Output            io.Writer
	SearchTitle       string
	SearchPlaceholder string
	SelectTitle       string
	ItemName          string
	PageSize          int
	Fetch             func(query string, limit, offset int) ([]T, int, error)
	Choice            func(T) Choice[T]
}

type searchAction int

const (
	chooseSearchResult searchAction = iota
	loadMoreSearchResults
	restartSearch
)

type searchItem[T any] struct {
	choice Choice[T]
	action searchAction
}

func (item searchItem[T]) Title() string       { return item.choice.Title }
func (item searchItem[T]) Description() string { return item.choice.Description }
func (item searchItem[T]) FilterValue() string { return "" }

type searchPage[T any] struct {
	query      string
	offset     int
	items      []T
	totalCount int
	err        error
}

type searchModel[T any] struct {
	options    SearchOptions[T]
	list       list.Model
	input      textinput.Model
	items      []T
	query      string
	pageSize   int
	totalCount int
	hasMore    bool
	searching  bool
	loading    bool
	canceled   bool
	selected   bool
	value      T
	err        error
}

// SearchSelect presents one reusable selector whose search and pagination are
// both backed by the catalog API. Pressing slash opens the search field.
func SearchSelect[T any](options SearchOptions[T]) (T, bool, error) {
	var zero T
	pageSize := options.PageSize
	if pageSize <= 0 {
		pageSize = 25
	}

	items, totalCount, err := options.Fetch("", pageSize, 0)
	if err != nil {
		return zero, false, err
	}

	model := newSearchModel(options, items, totalCount, pageSize)
	program := tea.NewProgram(
		model,
		tea.WithAltScreen(),
		tea.WithReportFocus(),
		tea.WithInput(options.Input),
		tea.WithOutput(options.Output),
	)
	final, err := program.Run()
	if err != nil {
		return zero, false, err
	}
	result, ok := final.(*searchModel[T])
	if !ok {
		return zero, false, errors.New("search selector returned an unexpected result")
	}
	if result.err != nil {
		return zero, false, result.err
	}
	if !result.selected {
		return zero, result.canceled, nil
	}
	return result.value, false, nil
}

func newSearchModel[T any](options SearchOptions[T], items []T, totalCount, pageSize int) *searchModel[T] {
	input := textinput.New()
	input.Prompt = "/ "
	input.Placeholder = options.SearchPlaceholder
	input.PromptStyle = lipgloss.NewStyle().Foreground(formAccent)
	input.Cursor.Style = lipgloss.NewStyle().Foreground(formAccent)

	delegate := list.NewDefaultDelegate()
	delegate.Styles.SelectedTitle = delegate.Styles.SelectedTitle.Foreground(formAccent).BorderLeft(false)
	delegate.Styles.SelectedDesc = delegate.Styles.SelectedDesc.Foreground(formAccent).BorderLeft(false)

	selector := list.New(nil, delegate, 80, selectionHeight+6)
	selector.Title = options.SelectTitle
	selector.Styles.Title = lipgloss.NewStyle().Foreground(formAccent).Bold(true).Padding(0, 1)
	selector.SetFilteringEnabled(false)
	selector.SetShowStatusBar(false)
	selector.SetShowPagination(false)
	selector.DisableQuitKeybindings()
	selector.AdditionalShortHelpKeys = func() []key.Binding {
		return []key.Binding{
			key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "search")),
			key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "cancel")),
		}
	}

	model := &searchModel[T]{
		options:    options,
		list:       selector,
		input:      input,
		items:      items,
		pageSize:   pageSize,
		totalCount: totalCount,
	}
	model.hasMore = len(items) == pageSize && (totalCount == 0 || len(items) < totalCount)
	model.rebuildList(false)
	return model
}

func (model *searchModel[T]) Init() tea.Cmd {
	return nil
}

func (model *searchModel[T]) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch message := message.(type) {
	case searchPage[T]:
		return model.receivePage(message)
	case tea.WindowSizeMsg:
		height := min(message.Height, selectionHeight+6)
		model.list.SetSize(message.Width, height)
		model.input.Width = max(1, min(message.Width-4, 76))
	case tea.KeyMsg:
		if model.loading {
			if message.String() == "ctrl+c" {
				model.canceled = true
				return model, tea.Quit
			}
			return model, nil
		}
		if model.searching {
			return model.updateSearch(message)
		}
		switch message.String() {
		case "/":
			return model.openSearch()
		case "esc", "ctrl+c":
			model.canceled = true
			return model, tea.Quit
		case "enter":
			return model.choose()
		}
	}

	var command tea.Cmd
	model.list, command = model.list.Update(message)
	return model, command
}

func (model *searchModel[T]) View() string {
	if model.loading {
		return model.list.Styles.Title.Render(model.options.SelectTitle) + "\n\nSearching LaunchDarkly…\n"
	}
	if model.searching {
		return model.list.Styles.Title.Render(model.options.SearchTitle) + "\n\n" +
			model.input.View() + "\n\n" +
			lipgloss.NewStyle().Faint(true).Render("enter search • esc back") + "\n"
	}
	return model.list.View()
}

func (model *searchModel[T]) openSearch() (tea.Model, tea.Cmd) {
	model.searching = true
	model.input.SetValue(model.query)
	model.input.CursorEnd()
	return model, model.input.Focus()
}

func (model *searchModel[T]) updateSearch(message tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch message.String() {
	case "ctrl+c":
		model.canceled = true
		return model, tea.Quit
	case "esc":
		model.searching = false
		model.input.Blur()
		return model, nil
	case "enter":
		model.searching = false
		model.loading = true
		model.input.Blur()
		return model, model.fetch(strings.TrimSpace(model.input.Value()), 0)
	}

	var command tea.Cmd
	model.input, command = model.input.Update(message)
	return model, command
}

func (model *searchModel[T]) choose() (tea.Model, tea.Cmd) {
	item, ok := model.list.SelectedItem().(searchItem[T])
	if !ok {
		return model, nil
	}
	switch item.action {
	case chooseSearchResult:
		model.selected = true
		model.value = item.choice.Value
		return model, tea.Quit
	case loadMoreSearchResults:
		model.loading = true
		return model, model.fetch(model.query, len(model.items))
	case restartSearch:
		return model.openSearch()
	}
	return model, nil
}

func (model *searchModel[T]) fetch(query string, offset int) tea.Cmd {
	return func() tea.Msg {
		items, totalCount, err := model.options.Fetch(query, model.pageSize, offset)
		return searchPage[T]{query: query, offset: offset, items: items, totalCount: totalCount, err: err}
	}
}

func (model *searchModel[T]) receivePage(page searchPage[T]) (tea.Model, tea.Cmd) {
	model.loading = false
	if page.err != nil {
		model.err = page.err
		return model, tea.Quit
	}

	previousCount := len(model.items)
	if page.offset == 0 {
		model.items = page.items
		model.query = page.query
		previousCount = 0
	} else {
		model.items = append(model.items, page.items...)
	}
	model.totalCount = page.totalCount
	model.hasMore = len(page.items) == model.pageSize && (page.totalCount == 0 || len(model.items) < page.totalCount)
	model.rebuildList(page.offset > 0)
	if page.offset > 0 {
		model.list.Select(previousCount)
	}
	return model, nil
}

func (model *searchModel[T]) rebuildList(loadedMore bool) {
	items := make([]list.Item, 0, len(model.items)+2)
	for _, value := range model.items {
		items = append(items, searchItem[T]{
			choice: model.options.Choice(value),
			action: chooseSearchResult,
		})
	}
	if model.hasMore {
		description := fmt.Sprintf("%d loaded", len(model.items))
		if model.totalCount > 0 {
			description = fmt.Sprintf("%d of %d loaded", len(model.items), model.totalCount)
		}
		items = append(items, searchItem[T]{
			choice: Choice[T]{Title: "Load more", Description: description},
			action: loadMoreSearchResults,
		})
	}

	if len(model.items) == 0 {
		items = append(items, searchItem[T]{
			choice: Choice[T]{
				Title:       "Search again",
				Description: fmt.Sprintf("No %s matched", model.options.ItemName),
			},
			action: restartSearch,
		})
	}
	_ = model.list.SetItems(items)

	title := model.options.SelectTitle
	if model.query != "" {
		title += fmt.Sprintf(" · Results for %q", model.query)
	}
	model.list.Title = title
	if !loadedMore {
		model.list.Select(0)
	}
}

// Select asks the user to choose one value.
func Select[T any](input io.Reader, output io.Writer, title string, choices []Choice[T]) (T, bool, error) {
	return SelectContext(context.Background(), input, output, title, choices)
}

// SelectContext asks the user to choose one value and stops when the context ends.
func SelectContext[T any](ctx context.Context, input io.Reader, output io.Writer, title string, choices []Choice[T]) (T, bool, error) {
	var zero T
	if len(choices) == 0 {
		return zero, false, fmt.Errorf("%s: no choices are available", title)
	}

	selected := 0
	options := make([]huh.Option[int], len(choices))
	for index, choice := range choices {
		options[index] = huh.NewOption(choiceLabel(choice), index)
	}

	field := huh.NewSelect[int]().
		Title(title).
		Options(options...).
		Height(selectionHeight).
		Value(&selected)

	canceled, err := RunFormContext(
		ctx,
		input,
		output,
		field,
	)
	if err != nil || canceled {
		return zero, canceled, err
	}
	return choices[selected].Value, false, nil
}

// MultiSelect asks the user to choose one or more values.
func MultiSelect[T any](input io.Reader, output io.Writer, title, description string, choices []Choice[T]) ([]T, bool, error) {
	if len(choices) == 0 {
		return nil, false, nil
	}

	var selected []int
	options := make([]huh.Option[int], len(choices))
	for index, choice := range choices {
		options[index] = huh.NewOption(choiceLabel(choice), index)
	}

	field := huh.NewMultiSelect[int]().
		Title(title).
		Description(description).
		Options(options...).
		Height(selectionHeight).
		Value(&selected).
		Validate(func(values []int) error {
			if len(values) == 0 {
				return errors.New("select at least one resource")
			}
			return nil
		})
	canceled, err := RunForm(input, output, field)
	if err != nil || canceled {
		return nil, canceled, err
	}

	values := make([]T, 0, len(selected))
	for _, index := range selected {
		values = append(values, choices[index].Value)
	}
	return values, false, nil
}

// RunForm runs fields with the shared sync theme and terminal streams.
func RunForm(input io.Reader, output io.Writer, fields ...huh.Field) (bool, error) {
	return RunFormContext(context.Background(), input, output, fields...)
}

// RunFormContext runs fields until they complete, abort, or the context ends.
func RunFormContext(ctx context.Context, input io.Reader, output io.Writer, fields ...huh.Field) (bool, error) {
	err := huh.NewForm(huh.NewGroup(fields...)).
		WithProgramOptions(tea.WithAltScreen(), tea.WithReportFocus()).
		WithInput(input).
		WithOutput(output).
		WithAccessible(false).
		WithTheme(formTheme()).
		RunWithContext(ctx)
	if errors.Is(err, huh.ErrUserAborted) {
		return true, nil
	}
	return false, err
}

// choiceLabel keeps the human-readable name visually above the stable key.
func choiceLabel[T any](choice Choice[T]) string {
	if choice.Description == "" {
		return choice.Title
	}
	return choice.Title + choiceSeparator + choice.Description + "\n"
}

// formTheme applies the subdued sync palette consistently to every form.
func formTheme() *huh.Theme {
	theme := huh.ThemeBase()
	buttonText := lipgloss.AdaptiveColor{Light: "255", Dark: "0"}

	theme.Focused.Base = lipgloss.NewStyle()
	theme.Focused.Card = theme.Focused.Base
	theme.Blurred.Base = lipgloss.NewStyle()
	theme.Blurred.Card = theme.Blurred.Base
	theme.Focused.Title = theme.Focused.Title.Foreground(formAccent).Bold(true)
	theme.Focused.SelectSelector = theme.Focused.SelectSelector.Foreground(formAccent)
	theme.Focused.MultiSelectSelector = theme.Focused.MultiSelectSelector.Foreground(formAccent)
	theme.Focused.SelectedOption = theme.Focused.SelectedOption.Foreground(formAccent)
	theme.Focused.SelectedPrefix = theme.Focused.SelectedPrefix.Foreground(formAccent)
	theme.Focused.FocusedButton = theme.Focused.FocusedButton.
		Background(formAccent).
		Foreground(buttonText)

	return theme
}
