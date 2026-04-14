package resources

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

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

	var baseURI string
	if parsed, err := url.Parse(path); err == nil && parsed.Scheme != "" {
		baseURI = parsed.Scheme + "://" + parsed.Host
	}

	if len(body) > 0 {
		var errMap map[string]interface{}
		if err := json.Unmarshal(body, &errMap); err != nil {
			errMap = map[string]interface{}{
				"code":       strings.ToLower(strings.ReplaceAll(http.StatusText(res.StatusCode), " ", "_")),
				"message":    string(body),
				"statusCode": res.StatusCode,
			}
		} else {
			if _, exists := errMap["statusCode"]; !exists {
				errMap["statusCode"] = res.StatusCode
			}
		}
		if suggestion := errors.SuggestionForStatus(res.StatusCode, baseURI); suggestion != "" {
			errMap["suggestion"] = suggestion
		}
		body, _ = json.Marshal(errMap)
		return body, errors.NewError(string(body))
	}

	errMap := map[string]interface{}{
		"code":       strings.ToLower(strings.ReplaceAll(http.StatusText(res.StatusCode), " ", "_")),
		"message":    http.StatusText(res.StatusCode),
		"statusCode": res.StatusCode,
	}
	if suggestion := errors.SuggestionForStatus(res.StatusCode, baseURI); suggestion != "" {
		errMap["suggestion"] = suggestion
	}
	resp, _ := json.Marshal(errMap)
	return body, errors.NewError(string(resp))
}
