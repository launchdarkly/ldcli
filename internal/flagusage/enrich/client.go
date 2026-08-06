package enrich

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

// ErrNotFound means the LD API returned 404 — the resource does not exist in
// the configured project. For a flag this signals an orphaned code reference.
var ErrNotFound = errors.New("not found in project")

type Client struct {
	apiToken string
	baseURL  string
	http     *http.Client
}

func NewClient(apiToken string, baseURL string) *Client {
	if baseURL == "" {
		baseURL = "https://app.launchdarkly.com"
	}
	return &Client{
		apiToken: apiToken,
		baseURL:  baseURL,
		http: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

func (c *Client) get(path string, result any) error {
	return c.getWithHeaders(path, nil, result)
}

func (c *Client) getWithHeaders(path string, headers map[string]string, result any) error {
	return c.do("GET", path, headers, nil, result)
}

// postJSON sends a JSON body to path. The request body is part of the cache
// key, so two posts to the same path with different bodies cache separately.
func (c *Client) postJSON(path string, body any, headers map[string]string, result any) error {
	payload, err := json.Marshal(body)
	if err != nil {
		return err
	}
	if headers == nil {
		headers = map[string]string{}
	}
	if _, ok := headers["Content-Type"]; !ok {
		headers["Content-Type"] = "application/json"
	}
	return c.do("POST", path, headers, payload, result)
}

func (c *Client) do(method, path string, headers map[string]string, body []byte, result any) error {
	key := cacheKey(method, path, headers, body)
	if cacheGet(key, result) {
		return nil
	}

	var reqBody io.Reader
	if body != nil {
		reqBody = bytes.NewReader(body)
	}
	req, err := http.NewRequest(method, c.baseURL+path, reqBody)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", c.apiToken)
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return fmt.Errorf("%s: %w", path, ErrNotFound)
	}
	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("API error %d: %s", resp.StatusCode, string(respBody))
	}

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	if err := json.Unmarshal(respBody, result); err != nil {
		return err
	}

	cacheSet(key, result)
	return nil
}
