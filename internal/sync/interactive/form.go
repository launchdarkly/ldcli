package interactive

import (
	"errors"
	"fmt"
	"io"

	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
)

const choiceSeparator = "\n  "

// Choice is one labeled value in an interactive selection.
type Choice[T any] struct {
	Title       string
	Description string
	Value       T
}

// Select asks the user to choose one value.
func Select[T any](
	input io.Reader,
	output io.Writer,
	title string,
	choices []Choice[T],
) (T, bool, error) {
	var zero T
	if len(choices) == 0 {
		return zero, false, fmt.Errorf("%s: no choices are available", title)
	}

	selected := 0
	options := make([]huh.Option[int], len(choices))
	for index, choice := range choices {
		options[index] = huh.NewOption(choiceLabel(choice), index)
	}

	canceled, err := RunForm(
		input,
		output,
		huh.NewSelect[int]().
			Title(title).
			Options(options...).
			Value(&selected),
	)
	if err != nil || canceled {
		return zero, canceled, err
	}
	return choices[selected].Value, false, nil
}

// MultiSelect asks the user to choose one or more values.
func MultiSelect[T any](
	input io.Reader,
	output io.Writer,
	title string,
	description string,
	choices []Choice[T],
) ([]T, bool, error) {
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
func RunForm(
	input io.Reader,
	output io.Writer,
	fields ...huh.Field,
) (bool, error) {
	err := huh.NewForm(huh.NewGroup(fields...)).
		WithInput(input).
		WithOutput(output).
		WithTheme(formTheme()).
		WithProgramOptions(tea.WithAltScreen()).
		Run()
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
	accent := lipgloss.Color("170")

	theme.Focused.Title = theme.Focused.Title.Foreground(accent).Bold(true)
	theme.Focused.SelectSelector = theme.Focused.SelectSelector.Foreground(accent)
	theme.Focused.MultiSelectSelector = theme.Focused.MultiSelectSelector.Foreground(accent)
	theme.Focused.SelectedOption = theme.Focused.SelectedOption.Foreground(accent)
	theme.Focused.SelectedPrefix = theme.Focused.SelectedPrefix.Foreground(accent)
	theme.Focused.FocusedButton = theme.Focused.FocusedButton.
		Background(accent).
		Foreground(lipgloss.Color("0"))

	return theme
}
