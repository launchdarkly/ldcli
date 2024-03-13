package projects

import (
	"context"
	"encoding/json"

	ldapi "github.com/launchdarkly/api-client-go/v14"

	"ld-cli/internal/errors"
)

type Client interface {
	List(context.Context) (*ldapi.Projects, error)
}

type ProjectsClient struct {
	client *ldapi.APIClient
}

func NewClient(accessToken string, baseURI string) ProjectsClient {
	config := ldapi.NewConfiguration()
	config.AddDefaultHeader("Authorization", accessToken)
	config.Servers[0].URL = baseURI
	client := ldapi.NewAPIClient(config)

	return ProjectsClient{
		client: client,
	}
}

func (c ProjectsClient) List(ctx context.Context) (*ldapi.Projects, error) {
	projects, _, err := c.client.ProjectsApi.
		GetProjects(ctx).
		Limit(2).
		Execute()
	if err != nil {
		return nil, err
	}

	return projects, nil
}

func ListProjects(ctx context.Context, client Client) ([]byte, error) {
	projects, err := client.List(ctx)
	if err != nil {
		switch err.Error() {
		case "401 Unauthorized":
			return nil, errors.ErrUnauthorized
		case "403 Forbidden":
			return nil, errors.ErrForbidden
		default:
			return nil, err
		}
	}

	projectsJSON, err := json.Marshal(projects)
	if err != nil {
		return nil, err
	}

	return projectsJSON, nil
}
