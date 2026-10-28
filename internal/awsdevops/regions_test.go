package awsdevops_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/launchdarkly/ldcli/internal/awsdevops"
)

func disableAWSCredentials(t *testing.T) {
	t.Helper()
	t.Setenv("AWS_ACCESS_KEY_ID", "")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "")
	t.Setenv("AWS_SESSION_TOKEN", "")
	t.Setenv("AWS_PROFILE", "")
	t.Setenv("AWS_DEFAULT_PROFILE", "")
	t.Setenv("AWS_CONFIG_FILE", "/dev/null")
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", "/dev/null")
	t.Setenv("AWS_EC2_METADATA_DISABLED", "true")
}

func TestNewClientsAcceptsTheNewRegions(t *testing.T) {
	disableAWSCredentials(t)
	for _, region := range []string{"us-east-2", "ap-northeast-2", "eu-west-3", "eu-north-1"} {
		t.Run(region, func(t *testing.T) {
			_, err := awsdevops.NewClients(context.Background(), region, "")

			// Past the region check, so credentials fail.
			assert.ErrorIs(t, err, awsdevops.ErrNoCredentials)
			assert.NotContains(t, err.Error(), "not available")
		})
	}
}

func TestNewClientsRejectsNearMissRegions(t *testing.T) {
	disableAWSCredentials(t)
	for _, region := range []string{"us-east-3", "eu-north-2", "ap-northeast-4", "eu-west-4"} {
		t.Run(region, func(t *testing.T) {
			_, err := awsdevops.NewClients(context.Background(), region, "")

			assert.ErrorContains(t, err, "AWS DevOps Agent is not available in "+region)
		})
	}
}

func TestSupportedRegionsMatchTheAWSList(t *testing.T) {
	assert.Equal(t, []string{
		"us-east-1", "us-east-2", "us-west-2", "ca-central-1", "sa-east-1",
		"ap-south-1", "ap-southeast-1", "ap-southeast-2", "ap-northeast-1", "ap-northeast-2",
		"eu-central-1", "eu-west-1", "eu-west-2", "eu-west-3", "eu-north-1",
	}, awsdevops.SupportedRegions)
}
