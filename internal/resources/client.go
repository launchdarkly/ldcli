package resources

import (
	"bytes"
	"fmt"
	"io"
	"ldcli/internal/errors"
	"net/http"
	"net/url"
)

type Client interface {
	MakeRequest(accessToken, method, path, contentType string, query url.Values, data []byte) ([]byte, error)
}

type ResourcesClient struct {
	cliVersion string
}

var _ Client = ResourcesClient{}

func NewClient(cliVersion string) ResourcesClient {
	return ResourcesClient{cliVersion: cliVersion}
}

func (c ResourcesClient) MakeRequest(accessToken, method, path, contentType string, query url.Values, data []byte) ([]byte, error) {
	client := http.Client{}

	req, _ := http.NewRequest(method, path, bytes.NewReader(data))
	req.Header.Add("Authorization", accessToken)
	req.Header.Add("Content-type", contentType)
	req.Header.Set("User-Agent", fmt.Sprintf("launchdarkly-cli/v%s", c.cliVersion))
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

	if res.StatusCode >= 400 {
		return body, errors.NewError(string(body))
	}

	return body, nil
}
