package dev_server

import (
	"bytes"
	"io"
	"net/http"

	"github.com/launchdarkly/ldcli/internal/errors"
)

type LocalClient interface {
	MakeRequest(method, path string, data []byte) ([]byte, error)
}

type DevClient struct{}

var _ LocalClient = DevClient{}

func NewLocalClient() DevClient {
	return DevClient{}
}

func (c DevClient) MakeRequest(method, path string, data []byte) ([]byte, error) {
	client := http.Client{}

	req, _ := http.NewRequest(method, path, bytes.NewReader(data))
	req.Header.Add("Content-type", "application/json")

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
		return body, errors.NewAPIError(body, errors.NewError(string(body)), []string{})
	}

	return body, nil
}
