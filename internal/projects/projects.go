package projects

import (
	"context"
	"encoding/json"

	ldapi "github.com/launchdarkly/api-client-go/v14"

	"ldcli/internal/client"
	"ldcli/internal/errors"
)

type Client interface {
	Create(ctx context.Context, accessToken, baseURI, name, key string) ([]byte, error)
	List(ctx context.Context, accessToken, baseURI string) ([]byte, error)
}

type ProjectsClient struct{}

var _ Client = ProjectsClient{}

func NewClient() Client {
	return ProjectsClient{}
}

func (c ProjectsClient) Create(
	ctx context.Context,
	accessToken,
	baseURI,
	name,
	key string,
) ([]byte, error) {
	client := client.New(accessToken, baseURI)
	projectPost := ldapi.NewProjectPost(name, key)
	project, _, err := client.ProjectsApi.PostProject(ctx).ProjectPost(*projectPost).Execute()
	if err != nil {
		return nil, errors.NewAPIError(err)
	}
	projectJSON, err := json.Marshal(project)
	if err != nil {
		return nil, err
	}

	return projectJSON, nil
}

func (c ProjectsClient) List(
	ctx context.Context,
	accessToken,
	baseURI string,
) ([]byte, error) {
	client := client.New(accessToken, baseURI)
	projects, _, err := client.ProjectsApi.
		GetProjects(ctx).
		Limit(2).
		Execute()
	if err != nil {
		return nil, errors.NewAPIError(err)
	}

	projectsJSON, err := json.Marshal(projects)
	if err != nil {
		return nil, err
	}

	return projectsJSON, nil
}
