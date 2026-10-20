package awsdevops_test

import (
	"context"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	iamtypes "github.com/aws/aws-sdk-go-v2/service/iam/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/launchdarkly/ldcli/internal/awsdevops"
)

var planRoles = []string{awsdevops.AgentSpaceRoleName, awsdevops.OperatorAppRoleName}

func planWith(t *testing.T, iamClient *fakeIAM, provision bool) awsdevops.SetupPlan {
	t.Helper()
	plan, err := awsdevops.PlanSetup(context.Background(), newTestClients(&fakeAgent{}, iamClient), awsdevops.SetupOptions{
		AgentSpaceName: "launchdarkly",
		AgentSpaceID:   map[bool]string{true: "", false: "space-2"}[provision],
	}, provision)
	require.NoError(t, err)

	return plan
}

// roleLine returns the plan line naming role.
func roleLine(t *testing.T, plan awsdevops.SetupPlan, role string) string {
	t.Helper()
	for _, line := range plan.Roles {
		if strings.Contains(line, role+" ") || strings.HasSuffix(line, role) {
			return strings.ToLower(line)
		}
	}
	t.Fatalf("no plan line names role %s in %q", role, plan.Roles)

	return ""
}

func TestPlanSetupCreatesMissingRoles(t *testing.T) {
	plan := planWith(t, newFakeIAM(), true)

	for _, role := range planRoles {
		assert.Contains(t, roleLine(t, plan, role), "create")
	}
}

func TestPlanSetupUpdatesManagedRoles(t *testing.T) {
	iamClient := newFakeIAM()
	for _, role := range planRoles {
		iamClient.addManagedRole(role)
	}

	plan := planWith(t, iamClient, true)

	for _, role := range planRoles {
		line := roleLine(t, plan, role)
		assert.Contains(t, line, "update")
		assert.NotContains(t, line, "create")
	}
}

// A tag with the right key but wrong value is not ours.
func TestPlanSetupLeavesUnmanagedRoles(t *testing.T) {
	iamClient := newFakeIAM()
	for _, role := range planRoles {
		iamClient.existingRoles[role] = true
		iamClient.roleTags[role] = []iamtypes.Tag{{
			Key:   aws.String(awsdevops.ManagedTagKey),
			Value: aws.String("something-else"),
		}}
	}

	plan := planWith(t, iamClient, true)

	for _, role := range planRoles {
		line := roleLine(t, plan, role)
		assert.Contains(t, line, "unchanged")
		assert.NotContains(t, line, "update")
		assert.NotContains(t, line, "create")
	}
}

func TestPlanSetupEnablesTheOperatorApp(t *testing.T) {
	plan := planWith(t, newFakeIAM(), true)

	assert.Contains(t, strings.ToLower(plan.OperatorApp), "enable")
}

// Connecting never touches roles or the app.
func TestPlanConnectLeavesRolesAndAppAlone(t *testing.T) {
	plan := planWith(t, newFakeIAM(), false)

	require.NotEmpty(t, plan.Roles)
	for _, line := range plan.Roles {
		lower := strings.ToLower(line)
		assert.NotContains(t, lower, "create")
		assert.NotContains(t, lower, "update")
	}
	require.NotEmpty(t, plan.OperatorApp)
	assert.NotContains(t, strings.ToLower(plan.OperatorApp), "enable")
}

func TestPlanSetupMakesNoIAMWrites(t *testing.T) {
	iamClient := newFakeIAM()
	iamClient.addManagedRole(awsdevops.AgentSpaceRoleName)

	_ = planWith(t, iamClient, true)

	assert.Empty(t, iamClient.createdRoles)
	assert.Empty(t, iamClient.trustPolicies)
	assert.Empty(t, iamClient.inlinePolicies)
	assert.Empty(t, iamClient.attachedPolicy)
}
