package projects_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ld-cli/internal/errors"
)

func strPtr(s string) *string {
	return &s
}

type GetProjectsResponse struct{}

type MockClient struct {
	hasForbiddenErr    bool
	hasUnauthorizedErr bool
}

func (c MockClient) Create(ctx context.Context, name string, key string) ([]byte, error) {
	return []byte(fmt.Sprintf(`{
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
	)), nil
}

func (c MockClient) List(ctx context.Context) ([]byte, error) {
	if c.hasForbiddenErr {
		return nil, errors.ErrForbidden
	}
	if c.hasUnauthorizedErr {
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

func TestCreateProject(t *testing.T) {
	t.Run("return a new project", func(t *testing.T) {
		expected := `{
			"_id": "000000000000000000000001",
			"_links": null,
			"environments": null,
			"includeInSnippetByDefault": false,
			"key": "test-key",
			"name": "test-name",
			"tags": null
		}`

		mockClient := MockClient{}

		response, err := mockClient.Create(context.Background(), "test-name", "test-key")

		require.NoError(t, err)
		assert.JSONEq(t, expected, string(response))
	})
}

func TestListProjects(t *testing.T) {
	t.Run("returns a paginated list of projects", func(t *testing.T) {
		expected := `{
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
		}`
		mockClient := MockClient{}

		response, err := mockClient.List(context.Background())

		require.NoError(t, err)
		assert.JSONEq(t, expected, string(response))
	})

	t.Run("without access is forbidden", func(t *testing.T) {
		mockClient := MockClient{
			hasForbiddenErr: true,
		}

		_, err := mockClient.List(context.Background())

		assert.EqualError(t, err, "You do not have permission to make this request")
	})

	t.Run("with invalid accessToken is unauthorized", func(t *testing.T) {
		mockClient := MockClient{
			hasUnauthorizedErr: true,
		}

		_, err := mockClient.List(context.Background())

		assert.EqualError(t, err, "You are not authorized to make this request")
	})
}
