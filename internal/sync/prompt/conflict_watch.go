package prompt

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

type watchedSources struct {
	repositoryRoot string
	snapshot       [sha256.Size]byte
}

type watchedConflictTick struct{}

type watchedConflictModel struct {
	watched watchedSources
	choice  conflictChoice
	err     error
}

func (model watchedConflictModel) Init() tea.Cmd {
	return waitForConflictWatchTick()
}

func (model watchedConflictModel) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch message := message.(type) {
	case tea.KeyMsg:
		switch message.String() {
		case "1":
			model.choice.resolution = useLaunchDarkly
			return model, tea.Quit
		case "2":
			model.choice.resolution = useLocal
			return model, tea.Quit
		case "3", "q", "esc", "ctrl+c":
			model.choice.aborted = true
			return model, tea.Quit
		}
	case watchedConflictTick:
		current, err := sourceSnapshot(model.watched.repositoryRoot)
		if err != nil {
			model.err = err
			return model, tea.Quit
		}
		if current != model.watched.snapshot {
			model.choice.sourcesChanged = true
			return model, tea.Quit
		}
		return model, waitForConflictWatchTick()
	}
	return model, nil
}

func (watchedConflictModel) View() string {
	return ""
}

func waitForConflictWatchTick() tea.Cmd {
	return tea.Tick(watchPollInterval, func(time.Time) tea.Msg {
		return watchedConflictTick{}
	})
}

// promptWatchedConflictResolution accepts a choice until a watched source changes.
func promptWatchedConflictResolution(
	ctx context.Context,
	input io.Reader,
	output io.Writer,
	watched watchedSources,
) (conflictChoice, error) {
	if _, err := fmt.Fprint(output, conflictResolutionPrompt); err != nil {
		return conflictChoice{}, err
	}

	programOptions := []tea.ProgramOption{tea.WithInput(input), tea.WithOutput(output), tea.WithoutRenderer()}
	if ctx != nil {
		programOptions = append(programOptions, tea.WithContext(ctx))
	}
	final, err := tea.NewProgram(watchedConflictModel{watched: watched}, programOptions...).Run()
	if err != nil {
		if ctx != nil && ctx.Err() != nil {
			return conflictChoice{aborted: true}, nil
		}
		return conflictChoice{}, err
	}

	model, ok := final.(watchedConflictModel)
	if !ok {
		return conflictChoice{}, fmt.Errorf("conflict prompt returned an unexpected model")
	}
	if model.err != nil {
		return conflictChoice{}, model.err
	}

	switch {
	case model.choice.sourcesChanged:
		_, _ = fmt.Fprintln(output, "\nPrompt files changed; refreshing...")
	case model.choice.aborted:
		_, _ = fmt.Fprintln(output, "\nSync canceled; conflict left unresolved.")
	case model.choice.resolution == useLaunchDarkly:
		_, _ = fmt.Fprintln(output, "\nUsing LaunchDarkly.")
	case model.choice.resolution == useLocal:
		_, _ = fmt.Fprintln(output, "\nUsing local.")
	}
	return model.choice, nil
}
