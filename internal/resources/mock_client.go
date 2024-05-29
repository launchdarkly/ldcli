package resources

import (
	"net/http"
	"net/url"
)

type MockClient struct {
	Err        error
	Input      []byte
	Response   []byte
	StatusCode int
}

var _ Client = &MockClient{}

func (c *MockClient) MakeRequest(
	accessToken, method, path, contentType string,
	query url.Values,
	data []byte,
	isBeta bool,
) ([]byte, error) {
	c.Input = data

	if c.StatusCode > http.StatusBadRequest {
		return c.Response, c.Err
	}

	return c.Response, nil
}
