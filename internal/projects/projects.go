package projects

import (
	"context"
	"encoding/json"
	"fmt"

	ldapi "github.com/launchdarkly/api-client-go/v14"

	"ld-cli/internal/errors"
)

type Client interface {
	Create(ctx context.Context, name string, key string) ([]byte, error)
	List(ctx context.Context) ([]byte, error)
}

type ProjectsClient struct {
	client *ldapi.APIClient
}

var _ Client = MockClient{}
var _ Client = ProjectsClient{}

type ProjectsClientFn = func(accessToken string, baseUri string) Client

func NewClient(accessToken string, baseURI string) Client {
	fmt.Println(">>> NewClient", baseURI)
	config := ldapi.NewConfiguration()
	config.AddDefaultHeader("Authorization", accessToken)
	config.Servers[0].URL = baseURI
	client := ldapi.NewAPIClient(config)

	return ProjectsClient{
		client: client,
	}
}

func (c ProjectsClient) Create(ctx context.Context, name string, key string) ([]byte, error) {
	projectPost := ldapi.NewProjectPost(name, key)
	project, _, err := c.client.ProjectsApi.PostProject(ctx).ProjectPost(*projectPost).Execute()
	if err != nil {
		return nil, err
	}
	projectJSON, err := json.Marshal(project)
	if err != nil {
		return nil, err
	}

	return projectJSON, nil
}

func (c ProjectsClient) List(ctx context.Context) ([]byte, error) {
	// fmt.Println(">>> client config")
	// spew.Dump(c.client.GetConfig())

	projects, _, err := c.client.ProjectsApi.
		GetProjects(ctx).
		Limit(2).
		Execute()
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

type MockClient struct {
	HasForbiddenErr    bool
	HasUnauthorizedErr bool
}

func NewMockClient(accessToken string, baseURI string) Client {
	return MockClient{}
}

func (c MockClient) Create(ctx context.Context, name string, key string) ([]byte, error) {
	return []byte(
		fmt.Sprintf(
			`{
				"_id": "000000000000000000000001",
				"_links": null,
				"environments": null,
				"includeInSnippetByDefault": false,
				"key": %q,
				"name": %q,
				"tags": null
			}`,
			key,
			name,
		),
	), nil
}

func (c MockClient) List(ctx context.Context) ([]byte, error) {
	if c.HasForbiddenErr {
		return nil, errors.ErrForbidden
	}
	if c.HasUnauthorizedErr {
		return nil, errors.ErrUnauthorized
	}

	return []byte(`{
		"_links": {
			"last": {
				"href": "/api/v2/projects?expand=environments&limit=1&offset=1",
					"type": "application/json"
			},
			"next": {
				"href": "/api/v2/projects?expand=environments&limit=1&offset=0",
					"type": "application/json"
			},
			"self": {
				"href": "/api/v2/projects?expand=environments&limit=1",
					"type": "application/json"
			}
		},
		"items": [
			{
				"_id": "000000000000000000000001",
				"_links": null,
				"includeInSnippetByDefault": false,
				"key": "test-project",
				"name": "",
				"tags": null
			}
		],
		"totalCount": 1
	}`), nil
}
