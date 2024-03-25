package projects

import (
	"context"
	"encoding/json"

	ldapi "github.com/launchdarkly/api-client-go/v14"

	"ldcli/internal/errors"
)

type Client interface {
	Create(ctx context.Context, accessToken, baseURI, name, key string) ([]byte, error)
	List(ctx context.Context, accessToken, baseURI string) ([]byte, error)
	CreateMember(ctx context.Context, accessToken, baseURI, email, role string) ([]byte, error)
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
	client := c.client(accessToken, baseURI)
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
	client := c.client(accessToken, baseURI)
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

func (c ProjectsClient) CreateMember(ctx context.Context, accessToken, baseURI, email, role string) ([]byte, error) {
	client := c.client(accessToken, baseURI)
	memberForm := ldapi.NewMemberForm{Email: email, Role: &role}
	members, _, err := client.AccountMembersApi.PostMembers(ctx).NewMemberForm([]ldapi.NewMemberForm{memberForm}).Execute()
	if err != nil {
		return nil, errors.NewAPIError(err)
	}
	memberJson, err := json.Marshal(members.Items[0])
	if err != nil {
		return nil, err
	}

	return memberJson, nil
}

// client creates an LD API client. It's not set as a field on the struct because the CLI flags
// are evaluated when running the command, not when executing the program. That means we don't have
// the flag values until the command's RunE method is called.
func (c ProjectsClient) client(accessToken string, baseURI string) *ldapi.APIClient {
	config := ldapi.NewConfiguration()
	config.AddDefaultHeader("Authorization", accessToken)
	config.Servers[0].URL = baseURI

	return ldapi.NewAPIClient(config)
}
