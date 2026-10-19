package awsdevops_test

import (
	"context"
	"errors"
	"net/http"
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
	assert.Contains(t, agent.calls, "GetOperatorApp")
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
	assert.Equal(t, "No change. The operator app is off", plan.OperatorApp)

	result, err := awsdevops.ConnectPlan(context.Background(), clients, opts, plan)
	require.NoError(t, err)
	assert.Empty(t, result.OperatorAppURL)
	assert.NotContains(t, agent.calls, "EnableOperatorApp")
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

// Same IdC or IdP flow changes settings.
func TestPlanSameIdcFlowSaysUpdate(t *testing.T) {
	apps := map[string]*devopsagent.GetOperatorAppOutput{
		"idc": idcApp(),
		"idp": {Idp: &agenttypes.IdpAuthConfiguration{}, OperatorAppUrl: aws.String("https://space-7.aidevops.global.app.aws")},
	}
	for flow, app := range apps {
		t.Run(flow, func(t *testing.T) {
			agent := reuseWithApp(app)
			opts := runPlanOptions()
			opts.AuthFlow, opts.AuthFlowSet = flow, true

			plan, err := awsdevops.PlanSetup(context.Background(), newTestClients(agent, newFakeIAM()), opts, true)

			require.NoError(t, err)
			assert.NotContains(t, plan.OperatorApp, "again")
			assert.Contains(t, strings.ToLower(plan.OperatorApp), "update")
		})
	}
}

func TestRunPlanStopsWhenReusedMCPServerIsGone(t *testing.T) {
	for name, replace := range map[string]bool{"reuse": false, "replace": true} {
		t.Run(name, func(t *testing.T) {
			agent := &fakeAgent{services: []agenttypes.RegisteredService{launchDarklyMCPService("mcp-old")}}
			iamClient := newFakeIAM()
			clients := newTestClients(agent, iamClient)
			opts := runPlanOptions()
			opts.ReplaceMCPToken = replace
			plan, err := awsdevops.PlanSetup(context.Background(), clients, opts, true)
			require.NoError(t, err)
			agent.services = nil

			_, err = awsdevops.RunPlan(context.Background(), clients, opts, plan)

			require.ErrorIs(t, err, awsdevops.ErrPlanChanged)
			assert.Empty(t, iamClient.createdRoles)
			assert.NotContains(t, agent.calls, "RegisterService")
			assert.NotContains(t, agent.calls, "DeregisterService")
		})
	}
}

func TestRunPlanStopsWhenMCPServerAppears(t *testing.T) {
	agent, iamClient := &fakeAgent{}, newFakeIAM()

	err := planThenRun(t, agent, iamClient, func() {
		agent.services = []agenttypes.RegisteredService{launchDarklyMCPService("mcp-new")}
	})

	require.ErrorIs(t, err, awsdevops.ErrPlanChanged)
	assert.Empty(t, iamClient.createdRoles)
	assert.NotContains(t, agent.calls, "RegisterService")
}

// Setup uses the planned server ID.
func TestRunPlanKeepsThePlannedMCPServer(t *testing.T) {
	agent := &fakeAgent{services: []agenttypes.RegisteredService{launchDarklyMCPService("mcp-old")}}
	clients := newTestClients(agent, newFakeIAM())
	plan, err := awsdevops.PlanSetup(context.Background(), clients, runPlanOptions(), true)
	require.NoError(t, err)
	agent.services = append([]agenttypes.RegisteredService{launchDarklyMCPService("mcp-other")}, agent.services...)

	result, err := awsdevops.RunPlan(context.Background(), clients, runPlanOptions(), plan)

	require.NoError(t, err)
	assert.Equal(t, "mcp-old", result.MCPServiceID)
}

// The pasted token uses the shown plan.
func TestRegisterMCPServerStopsWhenServerAppears(t *testing.T) {
	agent := &fakeAgent{}
	clients := newTestClients(agent, newFakeIAM())
	opts := runPlanOptions()
	opts.LDAccessToken = ""
	plan, err := awsdevops.PlanSetup(context.Background(), clients, opts, true)
	require.NoError(t, err)
	result, err := awsdevops.RunPlan(context.Background(), clients, opts, plan)
	require.NoError(t, err)
	agent.services = []agenttypes.RegisteredService{launchDarklyMCPService("mcp-someone")}
	opts.LDAccessToken, opts.ReplaceMCPToken = "pasted-token", true

	err = awsdevops.RegisterMCPServer(context.Background(), clients, opts, plan.MCP, &result)

	require.ErrorIs(t, err, awsdevops.ErrPlanChanged)
	assert.NotContains(t, agent.calls, "DeregisterService")
	assert.NotContains(t, agent.calls, "RegisterService")
}

func TestRegisterMCPServerRegistersAfterAsk(t *testing.T) {
	agent := &fakeAgent{}
	clients := newTestClients(agent, newFakeIAM())
	opts := runPlanOptions()
	opts.LDAccessToken = ""
	plan, err := awsdevops.PlanSetup(context.Background(), clients, opts, true)
	require.NoError(t, err)
	result, err := awsdevops.RunPlan(context.Background(), clients, opts, plan)
	require.NoError(t, err)
	opts.LDAccessToken = "pasted-token"

	require.NoError(t, awsdevops.RegisterMCPServer(context.Background(), clients, opts, plan.MCP, &result))

	assert.Contains(t, agent.calls, "RegisterService")
}

func TestConnectPlanStopsWhenServerAppears(t *testing.T) {
	agent := &fakeAgent{agentSpaces: []agenttypes.AgentSpace{namedSpace("space-7", "platform")}}
	clients := newTestClients(agent, newFakeIAM())
	opts := runPlanOptions()
	opts.AgentSpaceID = "space-7"
	plan, err := awsdevops.PlanSetup(context.Background(), clients, opts, false)
	require.NoError(t, err)
	agent.services = []agenttypes.RegisteredService{launchDarklyMCPService("mcp-someone")}

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

func TestPlanAcceptsRegisteredMCPServiceID(t *testing.T) {
	agent := &fakeAgent{services: []agenttypes.RegisteredService{launchDarklyMCPService("mcp-1")}}
	opts := runPlanOptions()
	opts.MCPServiceID = "mcp-1"

	plan, err := awsdevops.PlanSetup(context.Background(), newTestClients(agent, newFakeIAM()), opts, true)

	require.NoError(t, err)
	assert.Equal(t, awsdevops.MCPReuse, plan.MCP.Action)
}

func rejectingOptions(t *testing.T, status int) awsdevops.SetupOptions {
	t.Helper()
	opts := runPlanOptions()
	opts.LDBaseURI = stubLaunchDarkly(t, status)

	return opts
}

// Setup checks a sent token first.
func TestRunPlanChecksTokenBeforeWrites(t *testing.T) {
	for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			agent, iamClient := &fakeAgent{}, newFakeIAM()
			clients := newTestClients(agent, iamClient)
			opts := rejectingOptions(t, status)
			plan, err := awsdevops.PlanSetup(context.Background(), clients, opts, true)
			require.NoError(t, err)

			_, err = awsdevops.RunPlan(context.Background(), clients, opts, plan)

			require.ErrorIs(t, err, awsdevops.ErrInvalidAccessToken)
			assert.Empty(t, iamClient.createdRoles)
			assert.NotContains(t, agent.calls, "CreateAgentSpace")
		})
	}
}

func TestRunPlanChecksReplaceTokenBeforeWrites(t *testing.T) {
	agent := &fakeAgent{services: []agenttypes.RegisteredService{launchDarklyMCPService("mcp-old")}}
	iamClient := newFakeIAM()
	clients := newTestClients(agent, iamClient)
	opts := rejectingOptions(t, http.StatusUnauthorized)
	opts.ReplaceMCPToken = true
	plan, err := awsdevops.PlanSetup(context.Background(), clients, opts, true)
	require.NoError(t, err)

	_, err = awsdevops.RunPlan(context.Background(), clients, opts, plan)

	require.ErrorIs(t, err, awsdevops.ErrInvalidAccessToken)
	assert.Empty(t, iamClient.createdRoles)
}

// A reused server keeps its token.
func TestRunPlanSkipsTokenItWillNotSend(t *testing.T) {
	agent := &fakeAgent{services: []agenttypes.RegisteredService{launchDarklyMCPService("mcp-old")}}
	clients := newTestClients(agent, newFakeIAM())
	opts := runPlanOptions()
	opts.LDBaseURI = "http://127.0.0.1:1"
	plan, err := awsdevops.PlanSetup(context.Background(), clients, opts, true)
	require.NoError(t, err)

	_, err = awsdevops.RunPlan(context.Background(), clients, opts, plan)

	require.NoError(t, err)
}

func TestConnectPlanChecksTokenBeforeWrites(t *testing.T) {
	agent := &fakeAgent{agentSpaces: []agenttypes.AgentSpace{namedSpace("space-7", "platform")}}
	clients := newTestClients(agent, newFakeIAM())
	opts := rejectingOptions(t, http.StatusUnauthorized)
	opts.AgentSpaceID = "space-7"
	plan, err := awsdevops.PlanSetup(context.Background(), clients, opts, false)
	require.NoError(t, err)

	_, err = awsdevops.ConnectPlan(context.Background(), clients, opts, plan)

	require.ErrorIs(t, err, awsdevops.ErrInvalidAccessToken)
	assert.NotContains(t, agent.calls, "RegisterService")
}

func TestRegisterMCPServerChecksPastedToken(t *testing.T) {
	agent := &fakeAgent{}
	clients := newTestClients(agent, newFakeIAM())
	opts := runPlanOptions()
	opts.LDAccessToken = ""
	plan, err := awsdevops.PlanSetup(context.Background(), clients, opts, true)
	require.NoError(t, err)
	result, err := awsdevops.RunPlan(context.Background(), clients, opts, plan)
	require.NoError(t, err)
	opts.LDAccessToken = "api-token"
	opts.LDBaseURI = stubLaunchDarkly(t, http.StatusUnauthorized)

	err = awsdevops.RegisterMCPServer(context.Background(), clients, opts, plan.MCP, &result)

	require.ErrorIs(t, err, awsdevops.ErrInvalidAccessToken)
	assert.NotContains(t, agent.calls, "RegisterService")
}

func mcpLink(serviceID string) []agenttypes.Association {
	return []agenttypes.Association{{AssociationId: aws.String("assoc-" + serviceID), ServiceId: aws.String(serviceID)}}
}

func replaceAcrossSpaces() *fakeAgent {
	return &fakeAgent{
		agentSpaces: []agenttypes.AgentSpace{
			namedSpace("space-1", "launchdarkly"), namedSpace("space-2", "other"), namedSpace("space-3", "third"),
		},
		services:   []agenttypes.RegisteredService{launchDarklyMCPService("mcp-old")},
		spaceLinks: map[string][]agenttypes.Association{"space-1": mcpLink("mcp-old"), "space-3": mcpLink("mcp-old")},
	}
}

// The replace plan names touched spaces.
func TestPlanReplaceNamesEachSpace(t *testing.T) {
	opts := runPlanOptions()
	opts.ReplaceMCPToken = true

	plan, err := awsdevops.PlanSetup(context.Background(), newTestClients(replaceAcrossSpaces(), newFakeIAM()), opts, true)

	require.NoError(t, err)
	assert.Contains(t, plan.MCPServer, "space-1")
	assert.Contains(t, plan.MCPServer, "space-3")
	assert.NotContains(t, plan.MCPServer, "space-2")
}

func TestRunPlanStopsWhenReplaceSpacesChange(t *testing.T) {
	agent, iamClient := replaceAcrossSpaces(), newFakeIAM()
	clients := newTestClients(agent, iamClient)
	opts := runPlanOptions()
	opts.ReplaceMCPToken = true
	plan, err := awsdevops.PlanSetup(context.Background(), clients, opts, true)
	require.NoError(t, err)
	agent.spaceLinks["space-2"] = mcpLink("mcp-old")

	_, err = awsdevops.RunPlan(context.Background(), clients, opts, plan)

	require.ErrorIs(t, err, awsdevops.ErrPlanChanged)
	assert.NotContains(t, agent.calls, "DisassociateService")
	assert.Empty(t, iamClient.createdRoles)
}

// List order does not change the set.
func TestRunPlanReplaceIgnoresListOrder(t *testing.T) {
	agent, iamClient := replaceAcrossSpaces(), newFakeIAM()
	agent.afterListSpaces = func() {
		agent.agentSpaces = []agenttypes.AgentSpace{agent.agentSpaces[2], agent.agentSpaces[1], agent.agentSpaces[0]}
	}
	clients := newTestClients(agent, iamClient)
	opts := rejectingOptions(t, http.StatusOK)
	opts.ReplaceMCPToken = true
	plan, err := awsdevops.PlanSetup(context.Background(), clients, opts, true)
	require.NoError(t, err)

	_, err = awsdevops.RunPlan(context.Background(), clients, opts, plan)

	require.NoError(t, err)
}

// A link added during writes stops replace.
func TestRunPlanStopsWhenSpaceLinksDuringRoles(t *testing.T) {
	agent, iamClient := replaceAcrossSpaces(), newFakeIAM()
	iamClient.onCreateRole = func(string) { agent.spaceLinks["space-2"] = mcpLink("mcp-old") }
	clients := newTestClients(agent, iamClient)
	opts := rejectingOptions(t, http.StatusOK)
	opts.ReplaceMCPToken = true
	plan, err := awsdevops.PlanSetup(context.Background(), clients, opts, true)
	require.NoError(t, err)

	_, err = awsdevops.RunPlan(context.Background(), clients, opts, plan)

	require.ErrorIs(t, err, awsdevops.ErrPlanChanged)
	assert.NotContains(t, agent.calls, "DisassociateService")
}
