package login

import (
	"bytes"
	"fmt"
	"io"
	"net/http"

	"github.com/launchdarkly/ldcli/internal/errors"
)

type UnauthenticatedClient interface {
	MakeRequest(
		method string,
		path string,
		data []byte,
	) ([]byte, error)
}

type Client struct {
	cliVersion string
}

func NewClient(cliVersion string) Client {
	return Client{
		cliVersion: cliVersion,
	}
}

func (c Client) MakeRequest(
	method string,
	path string,
	data []byte,
) ([]byte, error) {
	client := http.Client{}

	req, _ := http.NewRequest(method, path, bytes.NewReader(data))
	req.Header.Add("Content-Type", "application/json")
	req.Header.Set("User-Agent", fmt.Sprintf("launchdarkly-cli/v%s", c.cliVersion))

	res, err := client.Do(req)
	if err != nil {
		return nil, err
	}

	body, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	if res.StatusCode < http.StatusBadRequest {
		return body, nil
	}

	if len(body) > 0 {
		return body, errors.NewError(string(body))
	}

	switch res.StatusCode {
	case http.StatusMethodNotAllowed:
		return body, errors.NewError(`{
			"code": "method_not_allowed",
			"message": "method not allowed"
		}`)
	default:
		return body, errors.NewError(fmt.Sprintf("could not complete the request: %d", res.StatusCode))
	}
}
