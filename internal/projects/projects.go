package projects

import (
	"context"
	"encoding/json"
	"fmt"

	ldapi "github.com/launchdarkly/api-client-go/v14"

	"ldcli/cmd/output"
	"ldcli/internal/client"
	"ldcli/internal/errors"
)

type Client interface {
	Create(ctx context.Context, accessToken, baseURI, name, key string) ([]byte, error)
	List(ctx context.Context, accessToken, baseURI, outputKind string) ([]byte, error)
}

type ProjectsClient struct {
	cliVersion string
}

var _ Client = ProjectsClient{}

func NewClient(cliVersion string) ProjectsClient {
	return ProjectsClient{
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
	outputKind string,
) ([]byte, error) {
	client := client.New(accessToken, baseURI, c.cliVersion)
	projects, _, err := client.ProjectsApi.
		GetProjects(ctx).
		Limit(2).
		Execute()
	if err != nil {
		return nil, errors.NewLDAPIError(err)
	}

	output, err := output.CmdOutput(outputKind, NewProjectOutputter(projects))
	if err != nil {
		return nil, errors.NewLDAPIError(err)

	}

	return []byte(output), nil
}

type ProjectOutputter struct {
	projects *ldapi.Projects
}

func (o ProjectOutputter) JSON() (string, error) {
	responseJSON, err := json.Marshal(o.projects)
	if err != nil {
		return "", err
	}

	return string(responseJSON), nil
}

func (o ProjectOutputter) String() string {
	fnPlaintext := func(p ldapi.Project) string {
		return fmt.Sprintf("* %s (%s)", p.Name, p.Key)
	}

	return output.FormatColl(o.projects.Items, fnPlaintext)
}

func NewProjectOutputter(projects *ldapi.Projects) ProjectOutputter {
	return ProjectOutputter{
		projects: projects,
	}
}
