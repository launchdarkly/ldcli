package config

import (
	"net/url"

	"github.com/launchdarkly/ldcli/internal/resources"
)

type Service struct {
	client resources.Client
}

func NewService(client resources.Client) Service {
	return Service{
		client: client,
	}
}

// VerifyAccessToken is true if the given access token is valid to make API requests.
func (s Service) VerifyAccessToken(accessToken string, baseURI string) bool {
	path, _ := url.JoinPath(baseURI, "api/v2/account")

	_, err := s.client.MakeRequest(accessToken, "HEAD", path, "application/json", nil, nil, false)

	return err == nil
}
