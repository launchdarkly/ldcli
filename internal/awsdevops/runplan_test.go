package awsdevops_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/devopsagent"
	agenttypes "github.com/aws/aws-sdk-go-v2/service/devopsagent/types"
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

func namedSpace(id, name string) agenttypes.AgentSpace {
	return agenttypes.AgentSpace{AgentSpaceId: aws.String(id), Name: aws.String(name)}
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

// A same-name space seen at plan is fine.
func TestRunPlanNewSpaceBesideSameName(t *testing.T) {
	agent := &fakeAgent{agentSpaces: []agenttypes.AgentSpace{namedSpace("space-7", "launchdarkly")}}
	clients := newTestClients(agent, newFakeIAM())
	opts := runPlanOptions()
	opts.NewAgentSpace = true

	_, err := awsdevops.Setup(context.Background(), clients, opts)

	require.NoError(t, err)
	assert.Contains(t, agent.calls, "CreateAgentSpace")
}

func TestRunPlanNewSpaceStopsOnSecondSameName(t *testing.T) {
	agent := &fakeAgent{agentSpaces: []agenttypes.AgentSpace{namedSpace("space-7", "launchdarkly")}}
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
	assert.Contains(t, err.Error(), "connect plan")
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

func awsLink() []agenttypes.Association {
	return []agenttypes.Association{{AssociationId: aws.String("assoc-aws"), ServiceId: aws.String("aws")}}
}

func TestPlanNamesTheAccountAssociation(t *testing.T) {
	for name, links := range map[string][]agenttypes.Association{"keep": awsLink(), "associate": nil} {
		t.Run(name, func(t *testing.T) {
			agent := &fakeAgent{
				agentSpaces:  []agenttypes.AgentSpace{namedSpace("space-7", "launchdarkly")},
				associations: links,
			}

			plan, err := awsdevops.PlanSetup(context.Background(), newTestClients(agent, newFakeIAM()), runPlanOptions(), true)

			require.NoError(t, err)
			assert.Contains(t, strings.ToLower(plan.Association), name)
			assert.Contains(t, plan.Association, testAccountID)
		})
	}
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

func reuseWithApp(app *devopsagent.GetOperatorAppOutput) *fakeAgent {
	agent := &fakeAgent{agentSpaces: []agenttypes.AgentSpace{namedSpace("space-7", "launchdarkly")}}
	if app != nil {
		agent.operatorApps = map[string]*devopsagent.GetOperatorAppOutput{"space-7": app}
	}

	return agent
}

func idcApp() *devopsagent.GetOperatorAppOutput {
	return &devopsagent.GetOperatorAppOutput{
		Idc:            &agenttypes.IdcAuthConfiguration{},
		OperatorAppUrl: aws.String("https://space-7.aidevops.global.app.aws"),
	}
}

// Without --auth-flow, the sign-in stays.
func TestRunPlanKeepsAppSignIn(t *testing.T) {
	agent := reuseWithApp(idcApp())
	clients := newTestClients(agent, newFakeIAM())
	plan, err := awsdevops.PlanSetup(context.Background(), clients, runPlanOptions(), true)
	require.NoError(t, err)

	result, err := awsdevops.RunPlan(context.Background(), clients, runPlanOptions(), plan)

	require.NoError(t, err)
	assert.Contains(t, strings.ToLower(plan.OperatorApp), "keep")
	assert.NotContains(t, agent.calls, "EnableOperatorApp")
	assert.Equal(t, "https://space-7.aidevops.global.app.aws", result.OperatorAppURL)
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
	assert.Contains(t, strings.ToLower(plan.OperatorApp), "change")
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
			assert.Contains(t, strings.ToLower(plan.OperatorApp), "enable")
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

func TestPlanConnectChangesNothingElse(t *testing.T) {
	agent := reuseWithApp(idcApp())
	opts := runPlanOptions()
	opts.AgentSpaceID = "space-7"

	plan, err := awsdevops.PlanSetup(context.Background(), newTestClients(agent, newFakeIAM()), opts, false)

	require.NoError(t, err)
	assert.Equal(t, "No change", plan.Association)
	assert.Equal(t, "No change", plan.OperatorApp)
	assert.NotContains(t, agent.calls, "GetOperatorApp")
}

// An app with no sign-in is off.
func TestPlanEnablesAppWithNoSignIn(t *testing.T) {
	agent := reuseWithApp(&devopsagent.GetOperatorAppOutput{})

	plan, err := awsdevops.PlanSetup(context.Background(), newTestClients(agent, newFakeIAM()), runPlanOptions(), true)

	require.NoError(t, err)
	assert.Contains(t, strings.ToLower(plan.OperatorApp), "enable")
}

// The same flow is not a change.
func TestPlanSameFlowIsNotAChange(t *testing.T) {
	agent := reuseWithApp(&devopsagent.GetOperatorAppOutput{
		Iam:            &agenttypes.IamAuthConfiguration{},
		OperatorAppUrl: aws.String("https://space-7.aidevops.global.app.aws"),
	})
	opts := runPlanOptions()
	opts.AuthFlowSet = true

	plan, err := awsdevops.PlanSetup(context.Background(), newTestClients(agent, newFakeIAM()), opts, true)

	require.NoError(t, err)
	assert.NotContains(t, strings.ToLower(plan.OperatorApp), "change")
	assert.Contains(t, plan.OperatorApp, "IAM")
}

// Same IdC flow may hold new settings.
func TestPlanSameIdcFlowSaysUpdate(t *testing.T) {
	agent := reuseWithApp(idcApp())
	opts := runPlanOptions()
	opts.AuthFlow, opts.AuthFlowSet = "idc", true

	plan, err := awsdevops.PlanSetup(context.Background(), newTestClients(agent, newFakeIAM()), opts, true)

	require.NoError(t, err)
	assert.NotContains(t, plan.OperatorApp, "again")
	assert.Contains(t, strings.ToLower(plan.OperatorApp), "update")
}
