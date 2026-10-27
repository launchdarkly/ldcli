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

// AttachmentSearcher searches the tools or the skills in a project.
type AttachmentSearcher interface {
	SearchAttachments(
		projectKey string, kind syncdomain.AttachmentKind, query string, limit, offset int,
	) (syncapi.Page[syncdomain.Attachment], error)
}

const attachmentPageSize = 25

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

// SelectAttachment asks the user to search for a tool or a skill and choose
// it. The bool result is true when the user cancels.
func SelectAttachment(
	input io.Reader,
	output io.Writer,
	searcher AttachmentSearcher,
	projectKey string,
	kind syncdomain.AttachmentKind,
) (syncdomain.Attachment, bool, error) {
	name := string(kind)
	return SearchSelect(SearchOptions[syncdomain.Attachment]{
		Input:             input,
		Output:            output,
		SearchTitle:       "Search " + name + "s",
		SearchPlaceholder: "Name or key",
		SelectTitle:       "Choose a " + name,
		ItemName:          name + "s",
		PageSize:          attachmentPageSize,
		Fetch: func(query string, limit, offset int) ([]syncdomain.Attachment, int, error) {
			page, err := searcher.SearchAttachments(projectKey, kind, query, limit, offset)
			return page.Items, page.TotalCount, err
		},
		Choice: attachmentChoice,
	})
}

// attachmentChoice shows the name of a skill, or the key of a tool, which has no name.
func attachmentChoice(attachment syncdomain.Attachment) Choice[syncdomain.Attachment] {
	if attachment.Skill != nil {
		return Choice[syncdomain.Attachment]{
			Title:       attachment.Skill.Name,
			Description: fmt.Sprintf("Key: %s · Latest version: %d", attachment.Key(), attachment.Version),
			Value:       attachment,
		}
	}
	return Choice[syncdomain.Attachment]{
		Title:       "Key: " + attachment.Key(),
		Description: fmt.Sprintf("Latest version: %d", attachment.Version),
		Value:       attachment,
	}
}
