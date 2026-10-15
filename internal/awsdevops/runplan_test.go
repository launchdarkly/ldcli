package awsdevops_test

import (
	"context"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	iamtypes "github.com/aws/aws-sdk-go-v2/service/iam/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/launchdarkly/ldcli/internal/awsdevops"
)

func runPlanOptions() awsdevops.SetupOptions {
	return awsdevops.SetupOptions{AgentSpaceName: "launchdarkly", AuthFlow: "iam", LDAccessToken: "api-token"}
}

// planThenRun plans, applies change, then runs.
func planThenRun(t *testing.T, agent *fakeAgent, iamClient *fakeIAM, change func()) error {
	t.Helper()
	clients := newTestClients(agent, iamClient)
	plan, err := awsdevops.PlanSetup(context.Background(), clients, runPlanOptions(), true)
	require.NoError(t, err)
	change()

	_, err = awsdevops.RunPlan(context.Background(), clients, runPlanOptions(), plan)

	return err
}

func TestRunPlanStopsWhenPlannedRoleAppears(t *testing.T) {
	agent, iamClient := &fakeAgent{}, newFakeIAM()

	err := planThenRun(t, agent, iamClient, func() {
		iamClient.existingRoles[awsdevops.AgentSpaceRoleName] = true
	})

	require.ErrorIs(t, err, awsdevops.ErrPlanChanged)
	assert.Empty(t, iamClient.attachedPolicy)
	assert.Empty(t, iamClient.trustPolicies)
	assert.NotContains(t, agent.calls, "CreateAgentSpace")
}

func TestRunPlanStopsWhenUpdatedRoleIsGone(t *testing.T) {
	agent, iamClient := &fakeAgent{}, newFakeIAM()
	for _, role := range planRoles {
		iamClient.addManagedRole(role)
	}

	err := planThenRun(t, agent, iamClient, func() {
		delete(iamClient.existingRoles, awsdevops.AgentSpaceRoleName)
	})

	require.ErrorIs(t, err, awsdevops.ErrPlanChanged)
	assert.Empty(t, iamClient.createdRoles)
	assert.NotContains(t, agent.calls, "CreateAgentSpace")
}

// A role left alone must stay untagged.
func TestRunPlanStopsWhenLeftRoleGetsTag(t *testing.T) {
	agent, iamClient := &fakeAgent{}, newFakeIAM()
	for _, role := range planRoles {
		iamClient.existingRoles[role] = true
	}

	err := planThenRun(t, agent, iamClient, func() {
		iamClient.roleTags[awsdevops.AgentSpaceRoleName] = []iamtypes.Tag{{
			Key:   aws.String(awsdevops.ManagedTagKey),
			Value: aws.String(awsdevops.ManagedTagValue),
		}}
	})

	require.ErrorIs(t, err, awsdevops.ErrPlanChanged)
	assert.Empty(t, iamClient.trustPolicies)
	assert.Empty(t, iamClient.attachedPolicy)
}

func TestRunPlanRunsWhenNothingChanged(t *testing.T) {
	agent, iamClient := &fakeAgent{}, newFakeIAM()

	err := planThenRun(t, agent, iamClient, func() {})

	require.NoError(t, err)
	assert.Contains(t, agent.calls, "CreateAgentSpace")
}

// A tagged role that appeared is already done.
func TestRunPlanContinuesWhenManagedRoleAppears(t *testing.T) {
	agent, iamClient := &fakeAgent{}, newFakeIAM()

	err := planThenRun(t, agent, iamClient, func() {
		iamClient.addManagedRole(awsdevops.AgentSpaceRoleName)
	})

	require.NoError(t, err)
	assert.Contains(t, agent.calls, "CreateAgentSpace")
}

// No role write before every role checks out.
func TestRunPlanChecksAllRolesFirst(t *testing.T) {
	agent, iamClient := &fakeAgent{}, newFakeIAM()

	err := planThenRun(t, agent, iamClient, func() {
		iamClient.existingRoles[awsdevops.OperatorAppRoleName] = true
	})

	require.ErrorIs(t, err, awsdevops.ErrPlanChanged)
	assert.Empty(t, iamClient.createdRoles)
	assert.Empty(t, iamClient.attachedPolicy)
	assert.Empty(t, iamClient.inlinePolicies)
}

func TestRunPlanStopsWhenUpdatedRoleLosesTag(t *testing.T) {
	agent, iamClient := &fakeAgent{}, newFakeIAM()
	for _, role := range planRoles {
		iamClient.addManagedRole(role)
	}

	err := planThenRun(t, agent, iamClient, func() {
		iamClient.roleTags[awsdevops.OperatorAppRoleName] = nil
	})

	require.ErrorIs(t, err, awsdevops.ErrPlanChanged)
	assert.Empty(t, iamClient.trustPolicies)
	assert.Empty(t, iamClient.attachedPolicy)
}

func TestRunPlanStopsWhenLeftRoleIsGone(t *testing.T) {
	agent, iamClient := &fakeAgent{}, newFakeIAM()
	for _, role := range planRoles {
		iamClient.existingRoles[role] = true
	}

	err := planThenRun(t, agent, iamClient, func() {
		delete(iamClient.existingRoles, awsdevops.OperatorAppRoleName)
	})

	require.ErrorIs(t, err, awsdevops.ErrPlanChanged)
}
