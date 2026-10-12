package interactive

import (
	"fmt"
	"io"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
	syncapi "github.com/launchdarkly/ldcli/internal/sync/api"
)

// ProjectSearcher searches the LaunchDarkly projects.
type ProjectSearcher interface {
	SearchProjects(query string, limit, offset int) (syncapi.Page[syncapi.Project], error)
}

// ConfigSearcher searches the configs in a project.
type ConfigSearcher interface {
	SearchConfigs(projectKey, query string, modes []syncdomain.VariationMode, limit, offset int) (syncapi.Page[syncapi.Config], error)
}

// SelectProject asks the user to choose a project. The bool result is true
// when the user cancels.
func SelectProject(input io.Reader, output io.Writer, catalog ProjectSearcher) (syncapi.Project, bool, error) {
	return SearchSelect(SearchOptions[syncapi.Project]{
		Input:             input,
		Output:            output,
		SearchTitle:       "Search LaunchDarkly projects",
		SearchPlaceholder: "Project name or key",
		SelectTitle:       "Choose a LaunchDarkly project",
		ItemName:          "projects",
		Fetch: func(query string, limit, offset int) ([]syncapi.Project, int, error) {
			page, err := catalog.SearchProjects(query, limit, offset)
			return page.Items, page.TotalCount, err
		},
		Choice: func(project syncapi.Project) Choice[syncapi.Project] {
			return Choice[syncapi.Project]{Title: project.Name, Description: "Key: " + project.Key, Value: project}
		},
	})
}

// SelectConfig asks the user to choose a config in a project. If modes is
// not empty, the list has only configs with one of the modes. The bool
// result is true when the user cancels.
func SelectConfig(
	input io.Reader,
	output io.Writer,
	catalog ConfigSearcher,
	projectKey string,
	modes []syncdomain.VariationMode,
) (syncapi.Config, bool, error) {
	return SearchSelect(SearchOptions[syncapi.Config]{
		Input:             input,
		Output:            output,
		SearchTitle:       "Search LaunchDarkly configs",
		SearchPlaceholder: "Config name or key",
		SelectTitle:       "Choose a config",
		ItemName:          "configs",
		Fetch: func(query string, limit, offset int) ([]syncapi.Config, int, error) {
			page, err := catalog.SearchConfigs(projectKey, query, modes, limit, offset)
			return page.Items, page.TotalCount, err
		},
		Choice: func(config syncapi.Config) Choice[syncapi.Config] {
			return Choice[syncapi.Config]{
				Title:       config.Name,
				Description: fmt.Sprintf("Key: %s · Mode: %s", config.Key, config.Mode),
				Value:       config,
			}
		},
	})
}
