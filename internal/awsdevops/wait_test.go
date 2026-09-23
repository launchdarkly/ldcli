package awsdevops_test

import (
	"context"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	agenttypes "github.com/aws/aws-sdk-go-v2/service/devopsagent/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/launchdarkly/ldcli/internal/awsdevops"
)

func TestWaitForGitHubServiceReturnsTheRegisteredService(t *testing.T) {
	agent := &fakeAgent{
		services: []agenttypes.RegisteredService{
			{ServiceId: aws.String("gh-1"), ServiceType: agenttypes.ServiceGithub},
		},
	}

	serviceID, err := awsdevops.WaitForGitHubService(context.Background(), newTestClients(agent, newFakeIAM()), time.Millisecond)
	require.NoError(t, err)

	assert.Equal(t, "gh-1", serviceID)
}

func TestWaitForGitHubServicePollsUntilCancelled(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	agent := &fakeAgent{}
	_, err := awsdevops.WaitForGitHubService(ctx, newTestClients(agent, newFakeIAM()), time.Millisecond)

	assert.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Contains(t, agent.calls, "ListServices")
}
