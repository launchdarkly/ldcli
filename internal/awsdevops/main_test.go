package awsdevops_test

import (
	"context"
	"os"
	"testing"

	"github.com/launchdarkly/ldcli/internal/awsdevops"
)

// Never call the real LaunchDarkly API.
func TestMain(m *testing.M) {
	awsdevops.CheckToken = func(ctx context.Context, baseURI, token string) error {
		if baseURI == "" {
			return nil
		}

		return awsdevops.ValidateAccessToken(ctx, baseURI, token)
	}
	os.Exit(m.Run())
}
