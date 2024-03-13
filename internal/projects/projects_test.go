package projects_test

import (
	"context"
	"errors"
	"testing"

	ldapi "github.com/launchdarkly/api-client-go/v14"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ld-cli/internal/projects"
)

func strPtr(s string) *string {
	return &s
}

type GetProjectsResponse struct{}

type MockClient struct {
	hasUnauthorizedErr bool
}

func (c MockClient) List(ctx context.Context) (*ldapi.Projects, error) {
	if c.hasUnauthorizedErr {
		return nil, errors.New("401 Unauthorized")
	}

	totalCount := int32(1)

	return &ldapi.Projects{
		Links: map[string]ldapi.Link{
			"last": {
				Href: strPtr("/api/v2/projects?expand=environments&limit=1&offset=1"),
				Type: strPtr("application/json"),
			},
			"next": {
				Href: strPtr("/api/v2/projects?expand=environments&limit=1&offset=0"),
				Type: strPtr("application/json"),
			},
			"self": {
				Href: strPtr("/api/v2/projects?expand=environments&limit=1"),
				Type: strPtr("application/json"),
			},
		},
		Items: []ldapi.Project{
			{
				Id:  "000000000000000000000001",
				Key: "test-project",
			},
		},
		TotalCount: &totalCount,
	}, nil
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

		response, err := projects.ListProjects(context.Background(), mockClient)

		require.NoError(t, err)
		assert.JSONEq(t, expected, string(response))
	})

	t.Run("with invalid accessToken is forbidden", func(t *testing.T) {
		mockClient := MockClient{
			hasUnauthorizedErr: true,
		}

		_, err := projects.ListProjects(context.Background(), mockClient)

		assert.EqualError(t, err, "You are not authorized to make this request.")
	})
}
