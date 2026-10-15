package awsdevops_test

import (
	"context"
	"errors"
	"strings"
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
	assert.NotEmpty(t, iamClient.trustPolicies[awsdevops.AgentSpaceRoleName])
	assert.Equal(t, awsdevops.AgentSpacePolicyARN, iamClient.attachedPolicy[awsdevops.AgentSpaceRoleName])
	assert.NotEmpty(t, iamClient.inlinePolicies[awsdevops.AgentSpaceRoleName])
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
	assert.Empty(t, iamClient.createdRoles)
	assert.Empty(t, iamClient.attachedPolicy)
	assert.Empty(t, iamClient.inlinePolicies)
}

var managedTags = []iamtypes.Tag{{
	Key:   aws.String(awsdevops.ManagedTagKey),
	Value: aws.String(awsdevops.ManagedTagValue),
}}

// A tagged role that races in is done.
func TestRunPlanContinuesWhenTaggedRoleRacesCreate(t *testing.T) {
	agent, iamClient := &fakeAgent{}, newFakeIAM()
	iamClient.appearOnCreate = map[string][]iamtypes.Tag{awsdevops.AgentSpaceRoleName: managedTags}

	err := planThenRun(t, agent, iamClient, func() {})

	require.NoError(t, err)
	assert.NotEmpty(t, iamClient.trustPolicies[awsdevops.AgentSpaceRoleName])
	assert.Equal(t, awsdevops.AgentSpacePolicyARN, iamClient.attachedPolicy[awsdevops.AgentSpaceRoleName])
	assert.NotEmpty(t, iamClient.inlinePolicies[awsdevops.AgentSpaceRoleName])
}

func TestRunPlanStopsWhenUntaggedRoleRacesCreate(t *testing.T) {
	agent, iamClient := &fakeAgent{}, newFakeIAM()
	iamClient.appearOnCreate = map[string][]iamtypes.Tag{awsdevops.AgentSpaceRoleName: nil}

	err := planThenRun(t, agent, iamClient, func() {})

	require.ErrorIs(t, err, awsdevops.ErrPlanChanged)
	assert.Contains(t, err.Error(), "appeared")
	assert.Empty(t, iamClient.attachedPolicy)
	assert.NotContains(t, agent.calls, "CreateAgentSpace")
}

// existsLines counts "already exists" lines.
func existsLines(t *testing.T, setup, change func(*fakeIAM)) (int, error) {
	t.Helper()
	agent, iamClient := &fakeAgent{}, newFakeIAM()
	setup(iamClient)
	clients := newTestClients(agent, iamClient)
	opts := runPlanOptions()
	logged := collectLogs(&opts)
	plan, err := awsdevops.PlanSetup(context.Background(), clients, opts, true)
	require.NoError(t, err)
	change(iamClient)

	_, err = awsdevops.RunPlan(context.Background(), clients, opts, plan)

	count := 0
	for _, line := range *logged {
		if strings.Contains(line, "already exists") {
			count++
		}
	}

	return count, err
}

func none(*fakeIAM) {}

// One line per role found on create.
func TestRunPlanReportsRoleAlreadyThere(t *testing.T) {
	for _, role := range planRoles {
		for name, appear := range map[string]func(*fakeIAM){
			"before the check": func(f *fakeIAM) { f.addManagedRole(role) },
			"during create": func(f *fakeIAM) {
				f.appearOnCreate = map[string][]iamtypes.Tag{role: managedTags}
			},
		} {
			t.Run(role+" "+name, func(t *testing.T) {
				count, err := existsLines(t, none, appear)

				require.NoError(t, err)
				assert.Equal(t, 1, count)
			})
		}
	}
}

func TestRunPlanSilentForCreatedAndLeftRoles(t *testing.T) {
	unmanaged := func(f *fakeIAM) {
		for _, role := range planRoles {
			f.existingRoles[role] = true
		}
	}
	managed := func(f *fakeIAM) {
		for _, role := range planRoles {
			f.addManagedRole(role)
		}
	}
	for name, setup := range map[string]func(*fakeIAM){"created": none, "left": unmanaged, "updated": managed} {
		t.Run(name, func(t *testing.T) {
			count, err := existsLines(t, setup, none)

			require.NoError(t, err)
			assert.Zero(t, count)
		})
	}
}

// No line when a role write fails.
func TestRunPlanSilentWhenUpdateFails(t *testing.T) {
	calls := map[string]string{
		"UpdateAssumeRolePolicy " + awsdevops.AgentSpaceRoleName:  "trust policy",
		"AttachRolePolicy " + awsdevops.AgentSpaceRoleName:        "attach",
		"PutRolePolicy " + awsdevops.AgentSpaceRoleName:           awsdevops.ServiceLinkedRolePolicyName,
		"UpdateAssumeRolePolicy " + awsdevops.OperatorAppRoleName: "trust policy",
		"AttachRolePolicy " + awsdevops.OperatorAppRoleName:       "attach",
	}
	appear := map[string]func(*fakeIAM, string){
		"before the check": func(f *fakeIAM, role string) { f.addManagedRole(role) },
		"during create": func(f *fakeIAM, role string) {
			f.appearOnCreate = map[string][]iamtypes.Tag{role: managedTags}
		},
	}
	for call, want := range calls {
		for when, makeRole := range appear {
			t.Run(call+" "+when, func(t *testing.T) {
				role := call[strings.Index(call, " ")+1:]
				count, err := existsLines(t, none, func(f *fakeIAM) {
					makeRole(f, role)
					f.failOn = map[string]error{call: errors.New("denied")}
				})

				require.Error(t, err)
				assert.Contains(t, err.Error(), want)
				assert.Zero(t, count)
			})
		}
	}
}
