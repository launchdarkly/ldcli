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

type ProjectsClient struct {
	cliVersion string
}

var _ Client = ProjectsClient{}

func NewClient(cliVersion string) *ProjectsClient {
	return &ProjectsClient{
		cliVersion: cliVersion,
	}
}

func (c ProjectsClient) Create(
	ctx context.Context,
	accessToken,
	baseURI,
	name,
	key string,
) ([]byte, error) {
	client := client.New(accessToken, baseURI, c.cliVersion)
	projectPost := ldapi.NewProjectPost(name, key)
	project, _, err := client.ProjectsApi.PostProject(ctx).ProjectPost(*projectPost).Execute()
	if err != nil {
		return nil, errors.NewLDAPIError(err)
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
	client := client.New(accessToken, baseURI, c.cliVersion)
	projects, _, err := client.ProjectsApi.
		GetProjects(ctx).
		Limit(2).
		Execute()
	if err != nil {
		return nil, errors.NewLDAPIError(err)
	}

	projectsJSON, err := json.Marshal(projects)
	if err != nil {
		return nil, err
	}

	return projectsJSON, nil
}
