//go:build awslive

// This smoke test runs against a real AWS account to catch upstream changes the
// unit tests cannot see: a new required field, a stricter trust policy, a
// renamed service type. It is idempotent rather than self-cleaning, so every
// run reuses the same agent space and also proves re-running setup is a no-op.
//
//	AWS_REGION=us-east-1 LD_ACCESS_TOKEN=<service token> \
//	  go test -tags awslive -timeout 20m ./internal/awsdevops/
//
// LD_ACCESS_TOKEN is optional: without it the MCP registration is skipped
// instead of failing.
package awsdevops

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const liveAgentSpaceName = "ldcli-smoke-test"

func TestLiveSetup(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()

	clients, err := NewClients(ctx, os.Getenv("AWS_REGION"), "")
	require.NoError(t, err)

	accountID, err := clients.AccountID(ctx)
	require.NoError(t, err)
	t.Logf("account %s in %s", accountID, clients.Region)

	opts := SetupOptions{
		AgentSpaceName: liveAgentSpaceName,
		LDAccessToken:  os.Getenv("LD_ACCESS_TOKEN"),
		Logf:           t.Logf,
	}

	result, err := Setup(ctx, clients, opts)
	require.NoError(t, err)

	assert.Equal(t, accountID, result.AccountID)
	assert.NotEmpty(t, result.AgentSpaceID)
	assert.NotEmpty(t, result.AgentSpaceRoleARN)
	assert.NotEmpty(t, result.OperatorAppRoleARN)
	assert.NotEmpty(t, result.AWSAssociationID)
	assert.Equal(t, OperatorAppURL(result.AgentSpaceID), result.OperatorAppURL)
	if opts.LDAccessToken != "" {
		assert.NotEmpty(t, result.MCPServiceID)
		assert.NotEmpty(t, result.MCPAssociationID)
	}

	for _, name := range []string{AgentSpaceRoleName, OperatorAppRoleName} {
		_, err := clients.IAM.GetRole(ctx, &iam.GetRoleInput{RoleName: aws.String(name)})
		require.NoError(t, err)
	}

	// Re-running has to reuse everything rather than provision a second copy.
	again, err := Setup(ctx, clients, opts)
	require.NoError(t, err)
	assert.Equal(t, result.AgentSpaceID, again.AgentSpaceID)
	assert.Equal(t, result.AWSAssociationID, again.AWSAssociationID)
	assert.Equal(t, result.MCPServiceID, again.MCPServiceID)

	status, err := GetStatus(ctx, clients, result.AgentSpaceID)
	require.NoError(t, err)
	assert.Empty(t, status.MissingSetup)
	require.Len(t, status.AgentSpaces, 1)
	assert.Equal(t, liveAgentSpaceName, status.AgentSpaces[0].Name)

	associated := make([]string, 0, len(status.AgentSpaces[0].Associations))
	for _, association := range status.AgentSpaces[0].Associations {
		associated = append(associated, association.ServiceID)
	}
	assert.Contains(t, associated, awsServiceID)
	if result.MCPServiceID != "" {
		assert.Contains(t, associated, result.MCPServiceID)
	}
}

// TestLiveSupportedRegions catches the hardcoded region list going stale: the
// agent space the smoke test provisions has to be visible from every region we
// claim to support.
func TestLiveSupportedRegions(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	for _, region := range SupportedRegions {
		t.Run(region, func(t *testing.T) {
			clients, err := NewClients(ctx, region, "")
			require.NoError(t, err)

			_, err = listAgentSpaces(ctx, clients)
			require.NoError(t, err)
		})
	}
}
