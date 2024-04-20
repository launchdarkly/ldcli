package projects

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

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
) ([]byte, error) {
	client := client.New(accessToken, baseURI, c.cliVersion)
	projects, _, err := client.ProjectsApi.
		GetProjects(ctx).
		Limit(2).
		Execute()
	if err != nil {
		return nil, errors.NewLDAPIError(err)
	}

	fnPlaintext := func(p ldapi.Project) string {
		return fmt.Sprintf("* %s (%s)", p.Name, p.Key)
	}
	return foo(projects.Items, fnPlaintext), nil

	projectsJSON, err := json.Marshal(projects)
	if err != nil {
		return nil, err
	}

	return projectsJSON, nil

	/*
		return outputter.Bytes(projects.Items)
	*/
}

func foo[T any](coll []T, fn func(T) string) []byte {
	lst := make([]string, 0, len(coll))
	for _, c := range coll {
		lst = append(lst, fn(c))
	}

	return []byte(strings.Join(lst, "\n"))
}

// TODO: return string instead of []byte?
type ResourceOutputter interface {
	Bytes(t any) ([]byte, error)
}

type PlaintextOutput struct {
	fn func(t any) string
}

func (o PlaintextOutput) Bytes(t []any) ([]byte, error) {
	return foo(t, o.fn), nil
}

type JSONOutput struct{}

func (o JSONOutput) Bytes(t any) ([]byte, error) {
	bytes, err := json.Marshal(t)
	if err != nil {
		return nil, err
	}

	return bytes, nil
}
