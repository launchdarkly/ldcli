package config

import (
	"fmt"

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
	path := fmt.Sprintf(
		"%s/api/v2/account",
		baseURI,
	)

	_, err := s.client.MakeRequest(accessToken, "HEAD", path, "application/json", nil, nil)

	return err == nil
}
