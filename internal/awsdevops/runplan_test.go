package awsdevops_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/devopsagent"
	agenttypes "github.com/aws/aws-sdk-go-v2/service/devopsagent/types"
	iamtypes "github.com/aws/aws-sdk-go-v2/service/iam/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/launchdarkly/ldcli/internal/awsdevops"
)

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

func TestRunPlanStopsWhenPlannedSpaceAppears(t *testing.T) {
	agent, iamClient := &fakeAgent{}, newFakeIAM()

	err := planThenRun(t, agent, iamClient, func() {
		agent.agentSpaces = []agenttypes.AgentSpace{namedSpace("space-9", "launchdarkly")}
	})

	require.ErrorIs(t, err, awsdevops.ErrPlanChanged)
	assert.NotContains(t, agent.calls, "CreateAgentSpace")
	assert.Empty(t, iamClient.createdRoles)
}

func TestRunPlanStopsWhenReusedSpaceIsGone(t *testing.T) {
	agent := &fakeAgent{agentSpaces: []agenttypes.AgentSpace{namedSpace("space-7", "launchdarkly")}}
	iamClient := newFakeIAM()

	err := planThenRun(t, agent, iamClient, func() {
		agent.goneSpaces = map[string]bool{"space-7": true}
	})

	require.ErrorIs(t, err, awsdevops.ErrPlanChanged)
	assert.NotContains(t, agent.calls, "CreateAgentSpace")
	assert.Empty(t, iamClient.createdRoles)
}

// No second search by name.
func TestRunPlanKeepsThePlannedSpace(t *testing.T) {
	agent := &fakeAgent{agentSpaces: []agenttypes.AgentSpace{namedSpace("space-7", "launchdarkly")}}
	clients := newTestClients(agent, newFakeIAM())
	plan, err := awsdevops.PlanSetup(context.Background(), clients, runPlanOptions(), true)
	require.NoError(t, err)
	agent.agentSpaces = append([]agenttypes.AgentSpace{namedSpace("space-8", "LaunchDarkly")}, agent.agentSpaces...)

	result, err := awsdevops.RunPlan(context.Background(), clients, runPlanOptions(), plan)

	require.NoError(t, err)
	assert.Equal(t, "space-7", result.AgentSpaceID)
	assert.NotContains(t, agent.calls, "CreateAgentSpace")
}

// The reuse line uses the AWS name.
func TestRunPlanReuseLineUsesAWSName(t *testing.T) {
	agent := &fakeAgent{agentSpaces: []agenttypes.AgentSpace{namedSpace("space-7", "LaunchDarkly")}}
	clients := newTestClients(agent, newFakeIAM())
	opts := runPlanOptions()
	logged := collectLogs(&opts)

	_, err := awsdevops.Setup(context.Background(), clients, opts)

	require.NoError(t, err)
	assert.Contains(t, strings.Join(*logged, "\n"), "LaunchDarkly (space-7)")
}

// Two spaces with one name make the next run unable to pick.
func TestRunPlanNewSpaceRefusesAUsedName(t *testing.T) {
	agent := &fakeAgent{agentSpaces: []agenttypes.AgentSpace{namedSpace("space-7", "LaunchDarkly")}}
	iamClient := newFakeIAM()
	opts := runPlanOptions()
	opts.NewAgentSpace = true

	_, err := awsdevops.Setup(context.Background(), newTestClients(agent, iamClient), opts)

	require.Error(t, err)
	assert.NotErrorIs(t, err, awsdevops.ErrPlanChanged)
	assert.Contains(t, err.Error(), "space-7")
	assert.Contains(t, err.Error(), "--agent-space-name")
	assert.NotContains(t, agent.calls, "CreateAgentSpace")
	assert.Empty(t, iamClient.createdRoles)
}

func TestRunPlanNewSpaceStopsWhenTheNameAppears(t *testing.T) {
	agent := &fakeAgent{}
	clients := newTestClients(agent, newFakeIAM())
	opts := runPlanOptions()
	opts.NewAgentSpace = true
	plan, err := awsdevops.PlanSetup(context.Background(), clients, opts, true)
	require.NoError(t, err)
	agent.agentSpaces = append(agent.agentSpaces, namedSpace("space-8", "LaunchDarkly"))

	_, err = awsdevops.RunPlan(context.Background(), clients, opts, plan)

	require.ErrorIs(t, err, awsdevops.ErrPlanChanged)
	assert.NotContains(t, agent.calls, "CreateAgentSpace")
}

// Names match without regard to case.
func TestRunPlanStopsWhenSpaceAppearsInOtherCase(t *testing.T) {
	agent, iamClient := &fakeAgent{}, newFakeIAM()

	err := planThenRun(t, agent, iamClient, func() {
		agent.agentSpaces = []agenttypes.AgentSpace{namedSpace("space-9", "LaunchDarkly")}
	})

	require.ErrorIs(t, err, awsdevops.ErrPlanChanged)
}

// Read errors are not plan changes.
func TestRunPlanWrapsSpaceReadError(t *testing.T) {
	agent := &fakeAgent{agentSpaces: []agenttypes.AgentSpace{namedSpace("space-7", "launchdarkly")}}

	err := planThenRun(t, agent, newFakeIAM(), func() {
		agent.getSpaceErr = errors.New("throttled")
	})

	require.Error(t, err)
	assert.NotErrorIs(t, err, awsdevops.ErrPlanChanged)
	assert.Contains(t, err.Error(), "throttled")
}

func TestRunPlanHintOnlyForNameMatch(t *testing.T) {
	agent := &fakeAgent{agentSpaces: []agenttypes.AgentSpace{namedSpace("space-7", "launchdarkly")}}
	clients := newTestClients(agent, newFakeIAM())
	opts := runPlanOptions()
	opts.AgentSpaceID = "space-7"
	logged := collectLogs(&opts)

	_, err := awsdevops.Setup(context.Background(), clients, opts)

	require.NoError(t, err)
	assert.NotContains(t, strings.Join(*logged, "\n"), "--new-agent-space")
}

func TestRunPlanRejectsConnectStep(t *testing.T) {
	agent := &fakeAgent{agentSpaces: []agenttypes.AgentSpace{namedSpace("space-7", "launchdarkly")}}
	iamClient := newFakeIAM()
	clients := newTestClients(agent, iamClient)
	opts := runPlanOptions()
	opts.AgentSpaceID = "space-7"
	plan, err := awsdevops.PlanSetup(context.Background(), clients, opts, false)
	require.NoError(t, err)

	_, err = awsdevops.RunPlan(context.Background(), clients, opts, plan)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "ConnectPlan")
	assert.Empty(t, iamClient.createdRoles)
}

// The create re-checks after role writes.
func TestRunPlanStopsWhenSpaceAppearsDuringRoles(t *testing.T) {
	agent, iamClient := &fakeAgent{}, newFakeIAM()
	iamClient.onCreateRole = func(name string) {
		if name == awsdevops.OperatorAppRoleName {
			agent.agentSpaces = []agenttypes.AgentSpace{namedSpace("space-9", "launchdarkly")}
		}
	}

	err := planThenRun(t, agent, iamClient, func() {})

	require.ErrorIs(t, err, awsdevops.ErrPlanChanged)
	assert.NotContains(t, agent.calls, "CreateAgentSpace")
}

// A space listed after planning stops setup.
func TestRunPlanStopsWhenSpaceAppearsDuringPlan(t *testing.T) {
	agent := &fakeAgent{agentSpaces: []agenttypes.AgentSpace{namedSpace("space-1", "other")}}
	agent.afterListSpaces = func() {
		agent.agentSpaces = []agenttypes.AgentSpace{namedSpace("space-9", "launchdarkly")}
		agent.afterListSpaces = nil
	}
	clients := newTestClients(agent, newFakeIAM())
	plan, err := awsdevops.PlanSetup(context.Background(), clients, runPlanOptions(), true)
	require.NoError(t, err)

	_, err = awsdevops.RunPlan(context.Background(), clients, runPlanOptions(), plan)

	require.ErrorIs(t, err, awsdevops.ErrPlanChanged)
	assert.NotContains(t, agent.calls, "CreateAgentSpace")
}

func TestRunPlanStopsWhenKeptAssociationIsGone(t *testing.T) {
	agent := &fakeAgent{
		agentSpaces:  []agenttypes.AgentSpace{namedSpace("space-7", "launchdarkly")},
		associations: awsLink(),
	}
	iamClient := newFakeIAM()

	err := planThenRun(t, agent, iamClient, func() { agent.associations = nil })

	require.ErrorIs(t, err, awsdevops.ErrPlanChanged)
	assert.Empty(t, iamClient.createdRoles)
	assert.NotContains(t, agent.calls, "AssociateService")
}

// A space that reads only another account still gets this account. Setup
// must not keep the association of the other account as its own.
func TestRunPlanAssociatesBesideAnotherAccount(t *testing.T) {
	agent := &fakeAgent{
		agentSpaces:  []agenttypes.AgentSpace{namedSpace("space-7", "launchdarkly")},
		associations: []agenttypes.Association{sourceAssociation("assoc-source", "210987654321")},
	}
	clients := newTestClients(agent, newFakeIAM())
	plan, err := awsdevops.PlanSetup(context.Background(), clients, runPlanOptions(), true)
	require.NoError(t, err)

	result, err := awsdevops.RunPlan(context.Background(), clients, runPlanOptions(), plan)

	require.NoError(t, err)
	assert.Equal(t, "assoc-aws", result.AWSAssociationID)
	var accounts []string
	for _, in := range agent.associationInputs {
		if config, ok := in.Configuration.(*agenttypes.ServiceConfigurationMemberAws); ok {
			accounts = append(accounts, aws.ToString(config.Value.AccountId))
		}
	}
	assert.Equal(t, []string{testAccountID}, accounts)
}

// An association that appeared is already done.
func TestRunPlanReportsAssociationAlreadyThere(t *testing.T) {
	agent := &fakeAgent{agentSpaces: []agenttypes.AgentSpace{namedSpace("space-7", "launchdarkly")}}
	clients := newTestClients(agent, newFakeIAM())
	opts := runPlanOptions()
	logged := collectLogs(&opts)
	plan, err := awsdevops.PlanSetup(context.Background(), clients, opts, true)
	require.NoError(t, err)
	agent.associations = awsLink()

	_, err = awsdevops.RunPlan(context.Background(), clients, opts, plan)

	require.NoError(t, err)
	for _, in := range agent.associationInputs {
		assert.NotEqual(t, "aws", aws.ToString(in.ServiceId))
	}
	assert.Contains(t, strings.Join(*logged, "\n"), "already associated")
}

// Without --auth-flow, the sign-in stays.
func TestRunPlanKeepsAppSignIn(t *testing.T) {
	agent := reuseWithApp(idcApp())
	clients := newTestClients(agent, newFakeIAM())
	plan, err := awsdevops.PlanSetup(context.Background(), clients, runPlanOptions(), true)
	require.NoError(t, err)

	result, err := awsdevops.RunPlan(context.Background(), clients, runPlanOptions(), plan)

	require.NoError(t, err)
	assert.Contains(t, strings.ToLower(plan.Text().OperatorApp), "keep")
	assert.NotContains(t, agent.calls, "EnableOperatorApp")
	assert.Equal(t, "https://space-7.aidevops.global.app.aws", result.OperatorAppURL)
}

// An app that is on must never look off to the caller.
func TestRunPlanKeptAppWithoutAURLGetsOne(t *testing.T) {
	app := idcApp()
	app.OperatorAppUrl = nil
	agent := reuseWithApp(app)
	clients := newTestClients(agent, newFakeIAM())
	plan, err := awsdevops.PlanSetup(context.Background(), clients, runPlanOptions(), true)
	require.NoError(t, err)

	result, err := awsdevops.RunPlan(context.Background(), clients, runPlanOptions(), plan)

	require.NoError(t, err)
	assert.Equal(t, awsdevops.OperatorAppURL("space-7"), result.OperatorAppURL)
	assert.True(t, result.OperatorAppEnabled)
}

func TestRunPlanChangesSignInWithAuthFlow(t *testing.T) {
	agent := reuseWithApp(idcApp())
	clients := newTestClients(agent, newFakeIAM())
	opts := runPlanOptions()
	opts.AuthFlowSet = true
	plan, err := awsdevops.PlanSetup(context.Background(), clients, opts, true)
	require.NoError(t, err)

	_, err = awsdevops.RunPlan(context.Background(), clients, opts, plan)

	require.NoError(t, err)
	assert.Contains(t, strings.ToLower(plan.Text().OperatorApp), "change")
	assert.Contains(t, agent.calls, "EnableOperatorApp")
}

// Unverified: a disabled app reads as not found.
func TestRunPlanEnablesAppWhenOff(t *testing.T) {
	for name, agent := range map[string]*fakeAgent{"reused space": reuseWithApp(nil), "new space": {}} {
		t.Run(name, func(t *testing.T) {
			clients := newTestClients(agent, newFakeIAM())
			plan, err := awsdevops.PlanSetup(context.Background(), clients, runPlanOptions(), true)
			require.NoError(t, err)

			_, err = awsdevops.RunPlan(context.Background(), clients, runPlanOptions(), plan)

			require.NoError(t, err)
			assert.Contains(t, strings.ToLower(plan.Text().OperatorApp), "enable")
			assert.Contains(t, agent.calls, "EnableOperatorApp")
		})
	}
}

func TestRunPlanStopsWhenKeptAppTurnsOff(t *testing.T) {
	agent, iamClient := reuseWithApp(idcApp()), newFakeIAM()

	err := planThenRun(t, agent, iamClient, func() { agent.operatorApps = nil })

	require.ErrorIs(t, err, awsdevops.ErrPlanChanged)
	assert.Empty(t, iamClient.createdRoles)
}

func TestRunPlanStopsWhenAppTurnsOn(t *testing.T) {
	agent, iamClient := reuseWithApp(nil), newFakeIAM()

	err := planThenRun(t, agent, iamClient, func() {
		agent.operatorApps = map[string]*devopsagent.GetOperatorAppOutput{"space-7": idcApp()}
	})

	require.ErrorIs(t, err, awsdevops.ErrPlanChanged)
	assert.Empty(t, iamClient.createdRoles)
	assert.NotContains(t, agent.calls, "EnableOperatorApp")
}

// A connect run must not print a URL for an app that is off.
func TestConnectPlanReportsAnAppThatIsOff(t *testing.T) {
	agent := reuseWithApp(nil)
	clients := newTestClients(agent, newFakeIAM())
	opts := runPlanOptions()
	opts.AgentSpaceID = "space-7"

	plan, err := awsdevops.PlanSetup(context.Background(), clients, opts, false)
	require.NoError(t, err)
	assert.Equal(t, "No change. The operator app is off", plan.Text().OperatorApp)

	result, err := awsdevops.ConnectPlan(context.Background(), clients, opts, plan)
	require.NoError(t, err)
	assert.Empty(t, result.OperatorAppURL)
	assert.False(t, result.OperatorAppEnabled)
	assert.NotContains(t, agent.calls, "EnableOperatorApp")

	encoded, err := json.Marshal(result)
	require.NoError(t, err)
	assert.Contains(t, string(encoded), `"operatorAppEnabled":false`)
}

// A connect run reports the app as it is at run time.
func TestConnectPlanReadsTheAppAgain(t *testing.T) {
	agent := reuseWithApp(&devopsagent.GetOperatorAppOutput{Iam: &agenttypes.IamAuthConfiguration{}})
	clients := newTestClients(agent, newFakeIAM())
	opts := runPlanOptions()
	plan, err := awsdevops.PlanSetup(context.Background(), clients, opts, false)
	require.NoError(t, err)
	require.Equal(t, "iam", plan.App.Flow)
	agent.operatorApps = nil

	result, err := awsdevops.ConnectPlan(context.Background(), clients, opts, plan)

	require.NoError(t, err)
	assert.False(t, result.OperatorAppEnabled)
	assert.Empty(t, result.OperatorAppURL)
}

func TestConnectPlanUsesTheURLThatAWSReturns(t *testing.T) {
	app := idcApp()
	app.OperatorAppUrl = aws.String("https://custom.example")
	agent := reuseWithApp(app)
	clients := newTestClients(agent, newFakeIAM())
	opts := runPlanOptions()
	opts.AgentSpaceID = "space-7"

	plan, err := awsdevops.PlanSetup(context.Background(), clients, opts, false)
	require.NoError(t, err)
	result, err := awsdevops.ConnectPlan(context.Background(), clients, opts, plan)
	require.NoError(t, err)

	assert.Equal(t, "https://custom.example", result.OperatorAppURL)
	assert.True(t, result.OperatorAppEnabled)
}

func TestRunPlanStopsWhenReusedMCPServerIsGone(t *testing.T) {
	agent := linkedSpace("mcp-old")
	iamClient := newFakeIAM()
	clients := newTestClients(agent, iamClient)
	opts := runPlanOptions()
	plan, err := awsdevops.PlanSetup(context.Background(), clients, opts, true)
	require.NoError(t, err)
	agent.services = nil

	_, err = awsdevops.RunPlan(context.Background(), clients, opts, plan)

	require.ErrorIs(t, err, awsdevops.ErrPlanChanged)
	assert.Empty(t, iamClient.createdRoles)
	assert.NotContains(t, agent.calls, "RegisterService")
}

func TestRunPlanStopsWhenMCPServerAppears(t *testing.T) {
	agent, iamClient := &fakeAgent{}, newFakeIAM()

	agent.agentSpaces = []agenttypes.AgentSpace{namedSpace("space-1", "launchdarkly")}

	err := planThenRun(t, agent, iamClient, func() {
		agent.services = []agenttypes.RegisteredService{launchDarklyMCPService("mcp-new")}
		agent.associations = mcpLink("mcp-new")
	})

	require.ErrorIs(t, err, awsdevops.ErrPlanChanged)
	assert.Empty(t, iamClient.createdRoles)
	assert.NotContains(t, agent.calls, "RegisterService")
}

// Setup uses the planned server ID.
func TestRunPlanKeepsThePlannedMCPServer(t *testing.T) {
	agent := linkedSpace("mcp-old")
	clients := newTestClients(agent, newFakeIAM())
	plan, err := awsdevops.PlanSetup(context.Background(), clients, runPlanOptions(), true)
	require.NoError(t, err)
	agent.services = append([]agenttypes.RegisteredService{launchDarklyMCPService("mcp-other")}, agent.services...)

	result, err := awsdevops.RunPlan(context.Background(), clients, runPlanOptions(), plan)

	require.NoError(t, err)
	assert.Equal(t, "mcp-old", result.MCPServiceID)
}

func TestConnectPlanStopsWhenServerAppears(t *testing.T) {
	agent := &fakeAgent{agentSpaces: []agenttypes.AgentSpace{namedSpace("space-7", "platform")}}
	clients := newTestClients(agent, newFakeIAM())
	opts := runPlanOptions()
	opts.AgentSpaceID = "space-7"
	plan, err := awsdevops.PlanSetup(context.Background(), clients, opts, false)
	require.NoError(t, err)
	agent.services = []agenttypes.RegisteredService{launchDarklyMCPService("mcp-someone")}
	agent.associations = mcpLink("mcp-someone")

	_, err = awsdevops.ConnectPlan(context.Background(), clients, opts, plan)

	require.ErrorIs(t, err, awsdevops.ErrPlanChanged)
	assert.NotContains(t, agent.calls, "AssociateService")
}

func TestConnectPlanStopsWhenSpaceIsGone(t *testing.T) {
	agent := &fakeAgent{agentSpaces: []agenttypes.AgentSpace{namedSpace("space-7", "platform")}}
	clients := newTestClients(agent, newFakeIAM())
	opts := runPlanOptions()
	opts.AgentSpaceID = "space-7"
	plan, err := awsdevops.PlanSetup(context.Background(), clients, opts, false)
	require.NoError(t, err)
	agent.goneSpaces = map[string]bool{"space-7": true}

	_, err = awsdevops.ConnectPlan(context.Background(), clients, opts, plan)

	require.ErrorIs(t, err, awsdevops.ErrPlanChanged)
	assert.NotContains(t, agent.calls, "RegisterService")
}

func TestConnectPlanRejectsSetupPlan(t *testing.T) {
	agent := &fakeAgent{agentSpaces: []agenttypes.AgentSpace{namedSpace("space-7", "launchdarkly")}}
	clients := newTestClients(agent, newFakeIAM())
	plan, err := awsdevops.PlanSetup(context.Background(), clients, runPlanOptions(), true)
	require.NoError(t, err)

	_, err = awsdevops.ConnectPlan(context.Background(), clients, runPlanOptions(), plan)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "RunPlan")
	assert.NotContains(t, agent.calls, "RegisterService")
}

func TestPlanRejectsBadMCPServiceID(t *testing.T) {
	github := agenttypes.RegisteredService{ServiceId: aws.String("svc-gh"), ServiceType: agenttypes.ServiceGithub}
	account := agenttypes.RegisteredService{ServiceId: aws.String("aws"), ServiceType: agenttypes.Service("aws")}
	for name, id := range map[string]string{"unknown": "mcp-9", "aws": "aws", "not an MCP server": "svc-gh"} {
		t.Run(name, func(t *testing.T) {
			agent := &fakeAgent{services: []agenttypes.RegisteredService{launchDarklyMCPService("mcp-1"), github, account}}
			opts := runPlanOptions()
			opts.MCPServiceID = id

			_, err := awsdevops.PlanSetup(context.Background(), newTestClients(agent, newFakeIAM()), opts, true)

			require.Error(t, err)
			assert.NotErrorIs(t, err, awsdevops.ErrPlanChanged)
			assert.Contains(t, err.Error(), id)
		})
	}
}

// The role writes can take minutes. A kept association that goes in that
// time stops the run, so setup does not create a new one.
func TestRunPlanStopsWhenKeptAssociationGoesDuringRoleWrites(t *testing.T) {
	agent := &fakeAgent{
		agentSpaces:  []agenttypes.AgentSpace{namedSpace("space-7", "launchdarkly")},
		associations: awsLink(),
	}
	iamClient := newFakeIAM()
	clients := newTestClients(agent, iamClient)
	plan, err := awsdevops.PlanSetup(context.Background(), clients, runPlanOptions(), true)
	require.NoError(t, err)
	iamClient.onCreateRole = func(string) { agent.associations = nil }

	_, err = awsdevops.RunPlan(context.Background(), clients, runPlanOptions(), plan)

	require.ErrorIs(t, err, awsdevops.ErrPlanChanged)
	assert.NotContains(t, agent.calls, "AssociateService")
}

// A server that appears during the role writes stops a register plan.
func TestRunPlanStopsWhenMCPServerAppearsDuringRoleWrites(t *testing.T) {
	agent := &fakeAgent{agentSpaces: []agenttypes.AgentSpace{namedSpace("space-7", "launchdarkly")}}
	iamClient := newFakeIAM()
	clients := newTestClients(agent, iamClient)
	plan, err := awsdevops.PlanSetup(context.Background(), clients, runPlanOptions(), true)
	require.NoError(t, err)
	require.Equal(t, awsdevops.MCPRegister, plan.MCP.Action)
	iamClient.onCreateRole = func(string) {
		agent.services = []agenttypes.RegisteredService{launchDarklyMCPService("mcp-new")}
		agent.associations = mcpLink("mcp-new")
	}

	_, err = awsdevops.RunPlan(context.Background(), clients, runPlanOptions(), plan)

	require.ErrorIs(t, err, awsdevops.ErrPlanChanged)
	assert.NotContains(t, agent.calls, "RegisterService")
}

// A reused server that goes during the role writes stops the run.
func TestRunPlanStopsWhenReusedMCPServerGoesDuringRoleWrites(t *testing.T) {
	agent := &fakeAgent{
		agentSpaces:  []agenttypes.AgentSpace{namedSpace("space-7", "launchdarkly")},
		services:     []agenttypes.RegisteredService{launchDarklyMCPService("mcp-1")},
		associations: mcpLink("mcp-1"),
	}
	iamClient := newFakeIAM()
	clients := newTestClients(agent, iamClient)
	plan, err := awsdevops.PlanSetup(context.Background(), clients, runPlanOptions(), true)
	require.NoError(t, err)
	require.Equal(t, awsdevops.MCPReuse, plan.MCP.Action)
	iamClient.onCreateRole = func(string) { agent.services = nil }

	_, err = awsdevops.RunPlan(context.Background(), clients, runPlanOptions(), plan)

	require.ErrorIs(t, err, awsdevops.ErrPlanChanged)
	for _, in := range agent.associationInputs {
		assert.NotEqual(t, "mcp-1", aws.ToString(in.ServiceId))
	}
}

func TestRunPlanStopsWhenAppSignInChanges(t *testing.T) {
	agent := &fakeAgent{
		agentSpaces:  []agenttypes.AgentSpace{{AgentSpaceId: aws.String("space-1"), Name: aws.String("launchdarkly")}},
		operatorApps: map[string]*devopsagent.GetOperatorAppOutput{"space-1": {Iam: &agenttypes.IamAuthConfiguration{}}},
	}

	err := planThenRun(t, agent, newFakeIAM(), func() {
		agent.operatorApps["space-1"] = &devopsagent.GetOperatorAppOutput{Idc: &agenttypes.IdcAuthConfiguration{}}
	})

	require.ErrorIs(t, err, awsdevops.ErrPlanChanged)
	assert.Contains(t, err.Error(), "sign-in changed")
	assert.NotContains(t, agent.calls, "EnableOperatorApp")
}

// The role step can take minutes, so the app step checks the app again.
func TestRunPlanStopsWhenAppSignInChangesDuringRoles(t *testing.T) {
	agent := &fakeAgent{
		agentSpaces:  []agenttypes.AgentSpace{namedSpace("space-1", "launchdarkly")},
		operatorApps: map[string]*devopsagent.GetOperatorAppOutput{"space-1": {Iam: &agenttypes.IamAuthConfiguration{}}},
	}
	iamClient := newFakeIAM()
	iamClient.onCreateRole = func(string) {
		agent.operatorApps["space-1"] = &devopsagent.GetOperatorAppOutput{Idc: &agenttypes.IdcAuthConfiguration{}}
	}

	err := planThenRun(t, agent, iamClient, func() {})

	require.ErrorIs(t, err, awsdevops.ErrPlanChanged)
	assert.Contains(t, err.Error(), "sign-in changed")
	assert.NotContains(t, agent.calls, "EnableOperatorApp")
}

// An app turned off during the run must not get a dead URL.
func TestRunPlanStopsWhenAppTurnsOffDuringRoles(t *testing.T) {
	agent := &fakeAgent{
		agentSpaces:  []agenttypes.AgentSpace{namedSpace("space-1", "launchdarkly")},
		operatorApps: map[string]*devopsagent.GetOperatorAppOutput{"space-1": {Iam: &agenttypes.IamAuthConfiguration{}}},
	}
	iamClient := newFakeIAM()
	iamClient.onCreateRole = func(string) { delete(agent.operatorApps, "space-1") }

	err := planThenRun(t, agent, iamClient, func() {})

	require.ErrorIs(t, err, awsdevops.ErrPlanChanged)
	assert.Contains(t, err.Error(), "disabled after the plan")
}

// linkedSpace is the agent space space-1, named launchdarkly, connected to
// the LaunchDarkly MCP server serviceID.
func linkedSpace(serviceID string) *fakeAgent {
	return &fakeAgent{
		agentSpaces:  []agenttypes.AgentSpace{namedSpace("space-1", "launchdarkly")},
		services:     []agenttypes.RegisteredService{launchDarklyMCPService(serviceID)},
		associations: mcpLink(serviceID),
	}
}

// --mcp-service-id must not give a space a second LaunchDarkly server.
func TestPlanStopsWhenMCPServiceIDIsNotTheServerOfTheSpace(t *testing.T) {
	agent := linkedSpace("mcp-own")
	agent.services = append(agent.services, launchDarklyMCPService("mcp-chosen"))
	opts := runPlanOptions()
	opts.MCPServiceID = "mcp-chosen"

	_, err := awsdevops.PlanSetup(context.Background(), newTestClients(agent, newFakeIAM()), opts, true)

	require.Error(t, err)
	assert.ErrorContains(t, err, "agent space space-1 already uses MCP server LaunchDarkly (mcp-own)")
}

// Setup cannot pick between two LaunchDarkly servers on one space.
func TestPlanStopsOnASpaceWithTwoLaunchDarklyServers(t *testing.T) {
	agent := linkedSpace("mcp-a")
	agent.services = append(agent.services, launchDarklyMCPService("mcp-b"))
	agent.associations = append(agent.associations, mcpLink("mcp-b")...)

	_, err := awsdevops.PlanSetup(context.Background(), newTestClients(agent, newFakeIAM()), runPlanOptions(), true)

	require.Error(t, err)
	assert.ErrorContains(t, err, "connected to 2 LaunchDarkly MCP servers")
}

// The plan names the other spaces that use the server.
func TestPlanWarnsWhenTheReusedServerIsShared(t *testing.T) {
	opts := runPlanOptions()

	plan, err := awsdevops.PlanSetup(context.Background(), newTestClients(sharedAcrossSpaces(), newFakeIAM()), opts, true)

	require.NoError(t, err)
	require.Equal(t, awsdevops.MCPReuse, plan.MCP.Action)
	assert.Equal(t,
		"Reuse MCP server LaunchDarkly (mcp-old). Agent spaces space-3 also use it",
		plan.Text().MCPServer)
}

func oauthMCPService(serviceID string) agenttypes.RegisteredService {
	service := mcpService(serviceID, awsdevops.MCPOAuthServerName, awsdevops.MCPServerEndpoint)
	details := service.AdditionalServiceDetails.(*agenttypes.AdditionalServiceDetailsMemberMcpserver)
	details.Value.AuthorizationMethod = agenttypes.MCPServerAuthorizationMethodOauth3lo

	return service
}

func TestOAuthRegistersAfterApproval(t *testing.T) {
	defer awsdevops.SetOAuthPollInterval(time.Millisecond)()
	agent := &fakeAgent{registerOutput: &devopsagent.RegisterServiceOutput{
		AdditionalStep: &agenttypes.AdditionalServiceRegistrationStepMemberOauth{
			Value: agenttypes.OAuthAdditionalStepDetails{AuthorizationUrl: aws.String("https://example.com/approve")},
		},
	}}
	clients := newTestClients(agent, newFakeIAM())
	opts := runPlanOptions()
	var opened string
	opts.OpenURL = func(url string) error {
		opened = url
		// AWS registers the server after the approval.
		agent.services = append(agent.services, oauthMCPService("mcp-account"))

		return nil
	}
	plan, err := awsdevops.PlanSetup(context.Background(), clients, opts, true)
	require.NoError(t, err)
	require.Equal(t, awsdevops.MCPRegister, plan.MCP.Action)

	result, err := awsdevops.RunPlan(context.Background(), clients, opts, plan)

	require.NoError(t, err)
	assert.Equal(t, "https://example.com/approve", opened)
	assert.Equal(t, "mcp-account", result.MCPServiceID)
	require.Len(t, agent.registerInputs, 1)
	details := agent.registerInputs[0].ServiceDetails.(*agenttypes.ServiceDetailsMemberMcpserver).Value
	assert.Equal(t, awsdevops.MCPOAuthServerName, aws.ToString(details.Name))
	assert.IsType(t, &agenttypes.MCPServerAuthorizationConfigMemberAuthorizationDiscovery{}, details.AuthorizationConfig)
	assert.Contains(t, agent.calls, "AssociateService")
}

func TestOAuthReusesTheServerOfTheAccount(t *testing.T) {
	agent := &fakeAgent{
		agentSpaces: []agenttypes.AgentSpace{namedSpace("space-7", "platform")},
		services:    []agenttypes.RegisteredService{oauthMCPService("mcp-account")},
	}
	clients := newTestClients(agent, newFakeIAM())
	opts := runPlanOptions()
	opts.AgentSpaceID = "space-7"
	plan, err := awsdevops.PlanSetup(context.Background(), clients, opts, false)
	require.NoError(t, err)
	require.Equal(t, awsdevops.MCPReuse, plan.MCP.Action)

	result, err := awsdevops.ConnectPlan(context.Background(), clients, opts, plan)

	require.NoError(t, err)
	assert.Equal(t, "mcp-account", result.MCPServiceID)
	assert.NotContains(t, agent.calls, "RegisterService")
}

// A server with a token is never the OAuth server of the account, even with
// the name that the console guide uses.
func TestOAuthDoesNotReuseAServerWithAToken(t *testing.T) {
	token := launchDarklyMCPService("mcp-token")
	details := token.AdditionalServiceDetails.(*agenttypes.AdditionalServiceDetailsMemberMcpserver)
	details.Value.AuthorizationMethod = agenttypes.MCPServerAuthorizationMethodBearerToken
	agent := &fakeAgent{services: []agenttypes.RegisteredService{token}}
	clients := newTestClients(agent, newFakeIAM())

	plan, err := awsdevops.PlanSetup(context.Background(), clients, runPlanOptions(), true)

	require.NoError(t, err)
	assert.Equal(t, awsdevops.MCPRegister, plan.MCP.Action)
}
