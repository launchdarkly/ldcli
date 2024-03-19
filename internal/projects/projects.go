package projects

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/davecgh/go-spew/spew"
	ldapi "github.com/launchdarkly/api-client-go/v14"

	"ld-cli/internal/errors"
)

type Client interface {
	Create(ctx context.Context, name string, key string) (*ldapi.ProjectRep, error)
	List(ctx context.Context) (*ldapi.Projects, error)
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

func (c ProjectsClient) Create(ctx context.Context, name string, key string) (*ldapi.ProjectRep, error) {
	projectPost := ldapi.NewProjectPost(name, key)
	fmt.Println(">>> projectPost")
	spew.Dump(projectPost)
	project, _, err := c.client.ProjectsApi.PostProject(ctx).ProjectPost(*projectPost).Execute()
	if err != nil {
		fmt.Println(">>> error creating project", err)
		return nil, err
	}

	fmt.Println(">>> created project")
	return project, nil
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

func CreateProject(ctx context.Context, client Client, name string, key string) ([]byte, error) {
	if name == "" {
		return nil, errors.New("name cannot be empty")
	}
	if key == "" {
		return nil, errors.New("key cannot be empty")
	}

	project, err := client.Create(ctx, name, key)
	if err != nil {
		return nil, handleError(err)
	}

	projectJSON, err := json.Marshal(project)
	if err != nil {
		fmt.Println(">>> error marshaling project", err)
		return nil, err
	}

	fmt.Println(">>> returning project JSON", string(projectJSON))
	return projectJSON, nil
}

func ListProjects(ctx context.Context, client Client) ([]byte, error) {
	projects, err := client.List(ctx)
	if err != nil {
		return nil, handleError(err)
	}

	projectsJSON, err := json.Marshal(projects)
	if err != nil {
		return nil, err
	}

	return projectsJSON, nil
}

// type errorResponse struct {
// 	Code    string `json:"code"`
// 	Message string `json:"message"`
// }

func handleError(err error) error {
	if e, ok := err.(*ldapi.GenericOpenAPIError); ok {
		// try to get the error message out of the response
		if string(e.Body()) != "" {
			var r struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			}
			// var r errorResponse
			err := json.Unmarshal(e.Body(), &r)
			if err != nil {
				return err
			}

			return errors.New(r.Message)
		}
	}

	// without an error message in the response body, we create our own message
	if err.Error() == "401 Unauthorized" {
		return errors.ErrUnauthorized
	}

	return err
}
