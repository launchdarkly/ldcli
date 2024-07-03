package resources

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"github.com/launchdarkly/ldcli/internal/errors"
)

type UnauthenticatedClient interface {
	MakeUnauthenticatedRequest(
		method string,
		path string,
		data []byte,
	) ([]byte, error)
}

type Client interface {
	UnauthenticatedClient
	MakeRequest(accessToken, method, path, contentType string, query url.Values, data []byte, isBeta bool) ([]byte, error)
}

type ResourcesClient struct {
	cliVersion string
}

var _ Client = ResourcesClient{}

func NewClient(cliVersion string) ResourcesClient {
	return ResourcesClient{cliVersion: cliVersion}
}

func (c ResourcesClient) MakeUnauthenticatedRequest(
	method string,
	path string,
	data []byte,
) ([]byte, error) {
	return c.MakeRequest("", method, path, "application/json", nil, data, false)
}

func (c ResourcesClient) MakeRequest(
	accessToken, method, path, contentType string,
	query url.Values,
	data []byte,
	isBeta bool,
) ([]byte, error) {
	client := http.Client{}
	req, _ := http.NewRequest(method, path, bytes.NewReader(data))
	req.Header.Add("Authorization", accessToken)
	req.Header.Add("Content-Type", contentType)
	req.Header.Set("User-Agent", fmt.Sprintf("launchdarkly-cli/v%s", c.cliVersion))
	if isBeta {
		req.Header.Set("LD-API-Version", "beta")
	}
	req.URL.RawQuery = query.Encode()

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
		resp, _ := json.Marshal(map[string]string{
			"code":    "method_not_allowed",
			"message": "method not allowed",
		})
		return body, errors.NewError(string(resp))
	default:
		return body, errors.NewError(fmt.Sprintf("could not complete the request: %d", res.StatusCode))
	}
}
