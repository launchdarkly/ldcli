package setup

import (
	"fmt"
	"io"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

var (
	projectStyle      = lipgloss.NewStyle().PaddingLeft(4)
	selectedItemStyle = lipgloss.NewStyle().PaddingLeft(2).Foreground(lipgloss.Color("170"))

	_ list.Item = project{}
)

type project struct {
	Key  string `json:"key"`
	Name string `json:"name"`
}

func (p project) FilterValue() string { return "" }

type projectModel struct {
	choice string
	err    error
	list   list.Model
}

func NewProject() tea.Model {
	l := list.New([]list.Item{}, projectDelegate{}, 30, 14)
	l.Title = "Select a project"
	l.SetShowStatusBar(false)
	l.SetFilteringEnabled(false)

	return projectModel{
		list: l,
	}
}

func (p projectModel) Init() tea.Cmd {
	return nil
}

func (m projectModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	switch msg := msg.(type) {
	case fetchProjects:
		projects, err := getProjects()
		if err != nil {
			m.err = err
			return m, nil
		}
		m.list.SetItems(projectsToItems(projects))
	case tea.KeyMsg:
		switch {
		case key.Matches(msg, keys.Enter):
			i, ok := m.list.SelectedItem().(project)
			if ok {
				m.choice = i.Key
			}
		case key.Matches(msg, keys.Quit):
			return m, tea.Quit
		default:
			m.list, cmd = m.list.Update(msg)
		}
	}

	return m, cmd
}

func (m projectModel) View() string {
	return "\n" + m.list.View()
}

// projectDelegate is used for display the list and its elements.
type projectDelegate struct{}

func (d projectDelegate) Height() int                             { return 1 }
func (d projectDelegate) Spacing() int                            { return 0 }
func (d projectDelegate) Update(_ tea.Msg, _ *list.Model) tea.Cmd { return nil }
func (d projectDelegate) Render(w io.Writer, m list.Model, index int, listItem list.Item) {
	i, ok := listItem.(project)
	if !ok {
		return
	}

	str := fmt.Sprintf("%d. %s", index+1, i.Name)

	fn := projectStyle.Render
	if index == m.Index() {
		fn = func(s ...string) string {
			return selectedItemStyle.Render("> " + strings.Join(s, " "))
		}
	}

	fmt.Fprint(w, fn(str))
}

func projectsToItems(projects []project) []list.Item {
	items := make([]list.Item, len(projects))
	for i, proj := range projects {
		items[i] = list.Item(proj)
	}

	return items
}

type fetchProjects struct{}

// type projectsResponse struct {
// 	Items []project `json:"items"`
// }

func getProjects() ([]project, error) {
	return []project{
		{
			Key:  "proj1",
			Name: "project 1",
		},
		{
			Key:  "proj2",
			Name: "project 2",
		},
		{
			Key:  "proj3",
			Name: "project 3",
		},
	}, nil

	// uncomment out below to fetch projects locally after adding an access token to the
	// Authorization header

	// url := "http://localhost/api/v2/projects"
	// c := &http.Client{
	// 	Timeout: 10 * time.Second,
	// }

	// req, _ := http.NewRequest("GET", url, nil)
	// req.Header.Add("Authorization", "")
	// req.Header.Add("Content-type", "application/json")

	// res, err := c.Do(req)
	// if err != nil {
	// 	return nil, err
	// }
	// defer res.Body.Close()

	// body, err := io.ReadAll(res.Body)
	// if err != nil {
	// 	return nil, err
	// }
	// projects := projectsResponse{}
	// err = json.Unmarshal(body, &projects)
	// if err != nil {
	// 	return nil, err
	// }

	// return projects.Items, nil
}
