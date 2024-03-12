package projects

import (
	"context"
	"encoding/json"

	ldapi "github.com/launchdarkly/api-client-go/v14"
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
		Expand("environments").
		Execute()
	if err != nil {
		// TODO: make this nicer
		return nil, err
	}

	return projects, nil
}

// func ListProjects(accessToken string, basePath string) ([]byte, error) {
func ListProjects(ctx context.Context, client2 Client) ([]byte, error) {
	projects, err := client2.List(ctx)
	if err != nil {
		// 401 - should return unauthorized type error with body(?)
		// 404 - should return not found type error with body
		e, ok := err.(ldapi.GenericOpenAPIError)
		if ok {
			return e.Body(), err
		}
		return nil, err
	}

	projectsJSON, err := json.Marshal(projects)
	if err != nil {
		return nil, err
	}

	return projectsJSON, nil
}
