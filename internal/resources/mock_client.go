package resources

import "net/url"

type MockClient struct {
	Input    []byte
	Response []byte
}

var _ Client = &MockClient{}

func (c *MockClient) MakeRequest(
	accessToken, method, path, contentType string,
	query url.Values,
	data []byte,
	isBeta bool,
) ([]byte, error) {
	c.Input = data

	return c.Response, nil
}
