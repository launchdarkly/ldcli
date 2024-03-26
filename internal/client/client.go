package client

import ldapi "github.com/launchdarkly/api-client-go/v14"

// New creates an LD API client. It's not set as a field on the struct because the CLI flags
// are evaluated when running the command, not when executing the program. That means we don't have
// the flag values until the command's RunE method is called.
func New(accessToken string, baseURI string) *ldapi.APIClient {
	config := ldapi.NewConfiguration()
	config.AddDefaultHeader("Authorization", accessToken)
	config.Servers[0].URL = baseURI

	return ldapi.NewAPIClient(config)
}
