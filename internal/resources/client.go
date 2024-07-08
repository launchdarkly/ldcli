package resources

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"github.com/launchdarkly/ldcli/internal/errors"
)

type Client interface {
	MakeRequest(accessToken, method, path, contentType string, query url.Values, data []byte, isBeta bool) ([]byte, error)
}

type ResourcesClient struct {
	cliVersion string
}

var _ Client = ResourcesClient{}

func NewClient(cliVersion string) ResourcesClient {
	return ResourcesClient{cliVersion: cliVersion}
}

func (c ResourcesClient) MakeRequest(accessToken, method, path, contentType string, query url.Values, data []byte, isBeta bool) ([]byte, error) {
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

	if res.StatusCode >= 400 {
		return body, errors.NewError(string(body))
	}

	return body, nil
}
