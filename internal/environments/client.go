package environments

import (
	"context"
	"fmt"
	"strings"

	ldapi "github.com/launchdarkly/api-client-go/v14"

	"ldcli/internal/client"
	"ldcli/internal/errors"
)

type Client interface {
	Get(ctx context.Context, accessToken, baseURI, key, projKey string) ([]byte, error)
}

type EnvironmentsClient struct {
	cliVersion string
}

var _ Client = EnvironmentsClient{}

func NewClient(cliVersion string) EnvironmentsClient {
	return EnvironmentsClient{
		cliVersion: cliVersion,
	}
}

func (c EnvironmentsClient) Get(
	ctx context.Context,
	accessToken,
	baseURI,
	key,
	projectKey string,
) ([]byte, error) {
	client := client.New(accessToken, baseURI, c.cliVersion)
	environment, _, err := client.EnvironmentsApi.GetEnvironment(ctx, projectKey, key).Execute()
	if err != nil {
		return nil, errors.NewLDAPIError(err)

	}

	fnPlaintext := func(p *ldapi.Environment) string {
		return fmt.Sprintf("%s (%s)", p.Name, p.Key)
	}
	return foo([]*ldapi.Environment{environment}, fnPlaintext), nil

	// responseJSON, err := json.Marshal(environment)
	// if err != nil {
	// 	return nil, err
	// }

	// return responseJSON, nil
}

func foo[T any](coll []T, fn func(T) string) []byte {
	lst := make([]string, 0, len(coll))
	for _, c := range coll {
		lst = append(lst, fn(c))
	}

	return []byte(strings.Join(lst, "\n"))
}
