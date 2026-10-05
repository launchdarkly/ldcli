package awsdevops_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/devopsagent"
	agenttypes "github.com/aws/aws-sdk-go-v2/service/devopsagent/types"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	iamtypes "github.com/aws/aws-sdk-go-v2/service/iam/types"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/launchdarkly/ldcli/internal/awsdevops"
)

const testAccountID = "123456789012"

type fakeAgent struct {
	calls              []string
	services           []agenttypes.RegisteredService
	associationInputs  []*devopsagent.AssociateServiceInput
	registerOutput     *devopsagent.RegisterServiceOutput
	associations       []agenttypes.Association
	agentSpaces        []agenttypes.AgentSpace
	assets             []agenttypes.Asset
	disassociatedIDs   []string
	deletedAssetIDs    []string
	deregisteredIDs    []string
	deletedAgentSpaces []string
	associateFailures  int
	// paginate makes every list API return one item per page, so callers are
	// exercised against the NextToken AWS sends for a long list.
	paginate bool
}

func page[T any](items []T, token *string, paginate bool) ([]T, *string) {
	if !paginate {
		return items, nil
	}

	first := 0
	if token != nil {
		first, _ = strconv.Atoi(*token)
	}
	if first >= len(items) {
		return nil, nil
	}
	if next := first + 1; next < len(items) {
		return items[first:next], aws.String(strconv.Itoa(next))
	}

	return items[first:], nil
}

func (f *fakeAgent) AssociateService(_ context.Context, in *devopsagent.AssociateServiceInput, _ ...func(*devopsagent.Options)) (*devopsagent.AssociateServiceOutput, error) {
	f.calls = append(f.calls, "AssociateService")
	f.associationInputs = append(f.associationInputs, in)

	if f.associateFailures > 0 {
		f.associateFailures--

		return nil, errors.New(
			"ValidationException: Invalid STS role configuration for session monitorVerificationAssociationRoleSession",
		)
	}

	return &devopsagent.AssociateServiceOutput{
		Association: &agenttypes.Association{AssociationId: aws.String("assoc-" + aws.ToString(in.ServiceId))},
	}, nil
}

func (f *fakeAgent) CreateAgentSpace(_ context.Context, _ *devopsagent.CreateAgentSpaceInput, _ ...func(*devopsagent.Options)) (*devopsagent.CreateAgentSpaceOutput, error) {
	f.calls = append(f.calls, "CreateAgentSpace")

	return &devopsagent.CreateAgentSpaceOutput{
		AgentSpace: &agenttypes.AgentSpace{AgentSpaceId: aws.String("space-1")},
	}, nil
}

func (f *fakeAgent) DeleteAgentSpace(_ context.Context, in *devopsagent.DeleteAgentSpaceInput, _ ...func(*devopsagent.Options)) (*devopsagent.DeleteAgentSpaceOutput, error) {
	f.calls = append(f.calls, "DeleteAgentSpace")
	f.deletedAgentSpaces = append(f.deletedAgentSpaces, aws.ToString(in.AgentSpaceId))

	return &devopsagent.DeleteAgentSpaceOutput{}, nil
}

func (f *fakeAgent) DeleteAsset(_ context.Context, in *devopsagent.DeleteAssetInput, _ ...func(*devopsagent.Options)) (*devopsagent.DeleteAssetOutput, error) {
	f.calls = append(f.calls, "DeleteAsset")
	f.deletedAssetIDs = append(f.deletedAssetIDs, aws.ToString(in.AssetId))

	return &devopsagent.DeleteAssetOutput{}, nil
}

func (f *fakeAgent) DeregisterService(_ context.Context, in *devopsagent.DeregisterServiceInput, _ ...func(*devopsagent.Options)) (*devopsagent.DeregisterServiceOutput, error) {
	f.calls = append(f.calls, "DeregisterService")
	f.deregisteredIDs = append(f.deregisteredIDs, aws.ToString(in.ServiceId))

	return &devopsagent.DeregisterServiceOutput{}, nil
}

func (f *fakeAgent) DisassociateService(_ context.Context, in *devopsagent.DisassociateServiceInput, _ ...func(*devopsagent.Options)) (*devopsagent.DisassociateServiceOutput, error) {
	f.calls = append(f.calls, "DisassociateService")
	f.disassociatedIDs = append(f.disassociatedIDs, aws.ToString(in.AssociationId))

	return &devopsagent.DisassociateServiceOutput{}, nil
}

func (f *fakeAgent) EnableOperatorApp(_ context.Context, _ *devopsagent.EnableOperatorAppInput, _ ...func(*devopsagent.Options)) (*devopsagent.EnableOperatorAppOutput, error) {
	f.calls = append(f.calls, "EnableOperatorApp")

	return &devopsagent.EnableOperatorAppOutput{
		OperatorAppUrl: aws.String("https://space-1.aidevops.global.app.aws"),
	}, nil
}

func (f *fakeAgent) GetAgentSpace(_ context.Context, in *devopsagent.GetAgentSpaceInput, _ ...func(*devopsagent.Options)) (*devopsagent.GetAgentSpaceOutput, error) {
	return &devopsagent.GetAgentSpaceOutput{
		AgentSpace: &agenttypes.AgentSpace{
			AgentSpaceId: in.AgentSpaceId,
			Name:         aws.String("launchdarkly"),
		},
	}, nil
}

func (f *fakeAgent) ListAgentSpaces(_ context.Context, in *devopsagent.ListAgentSpacesInput, _ ...func(*devopsagent.Options)) (*devopsagent.ListAgentSpacesOutput, error) {
	spaces := f.agentSpaces
	if spaces == nil {
		spaces = []agenttypes.AgentSpace{{AgentSpaceId: aws.String("space-1")}}
	}
	items, next := page(spaces, in.NextToken, f.paginate)

	return &devopsagent.ListAgentSpacesOutput{AgentSpaces: items, NextToken: next}, nil
}

func (f *fakeAgent) ListServices(_ context.Context, in *devopsagent.ListServicesInput, _ ...func(*devopsagent.Options)) (*devopsagent.ListServicesOutput, error) {
	f.calls = append(f.calls, "ListServices")
	items, next := page(f.services, in.NextToken, f.paginate)

	return &devopsagent.ListServicesOutput{Services: items, NextToken: next}, nil
}

func (f *fakeAgent) ListAssets(_ context.Context, in *devopsagent.ListAssetsInput, _ ...func(*devopsagent.Options)) (*devopsagent.ListAssetsOutput, error) {
	items, next := page(f.assets, in.NextToken, f.paginate)

	return &devopsagent.ListAssetsOutput{Items: items, NextToken: next}, nil
}

func (f *fakeAgent) ListAssociations(_ context.Context, in *devopsagent.ListAssociationsInput, _ ...func(*devopsagent.Options)) (*devopsagent.ListAssociationsOutput, error) {
	items, next := page(f.associations, in.NextToken, f.paginate)

	return &devopsagent.ListAssociationsOutput{Associations: items, NextToken: next}, nil
}

func (f *fakeAgent) RegisterService(_ context.Context, _ *devopsagent.RegisterServiceInput, _ ...func(*devopsagent.Options)) (*devopsagent.RegisterServiceOutput, error) {
	f.calls = append(f.calls, "RegisterService")
	if f.registerOutput != nil {
		return f.registerOutput, nil
	}

	return &devopsagent.RegisterServiceOutput{ServiceId: aws.String("mcp-1")}, nil
}

type fakeIAM struct {
	existingRoles   map[string]bool
	createdRoles    []string
	attachedPolicy  map[string]string
	inlinePolicies  map[string]string
	trustPolicies   map[string]string
	deletedRoles    []string
	detachedPolicy  []string
	deletedInlinePs []string
}

func newFakeIAM() *fakeIAM {
	return &fakeIAM{
		existingRoles:  map[string]bool{},
		attachedPolicy: map[string]string{},
		inlinePolicies: map[string]string{},
		trustPolicies:  map[string]string{},
	}
}

func (f *fakeIAM) AttachRolePolicy(_ context.Context, in *iam.AttachRolePolicyInput, _ ...func(*iam.Options)) (*iam.AttachRolePolicyOutput, error) {
	f.attachedPolicy[aws.ToString(in.RoleName)] = aws.ToString(in.PolicyArn)

	return &iam.AttachRolePolicyOutput{}, nil
}

func (f *fakeIAM) CreateRole(_ context.Context, in *iam.CreateRoleInput, _ ...func(*iam.Options)) (*iam.CreateRoleOutput, error) {
	name := aws.ToString(in.RoleName)
	if f.existingRoles[name] {
		return nil, &iamtypes.EntityAlreadyExistsException{}
	}
	f.existingRoles[name] = true
	f.createdRoles = append(f.createdRoles, name)
	f.trustPolicies[name] = aws.ToString(in.AssumeRolePolicyDocument)

	return &iam.CreateRoleOutput{
		Role: &iamtypes.Role{Arn: aws.String("arn:aws:iam::" + testAccountID + ":role/" + name)},
	}, nil
}

func (f *fakeIAM) DeleteRole(_ context.Context, in *iam.DeleteRoleInput, _ ...func(*iam.Options)) (*iam.DeleteRoleOutput, error) {
	f.deletedRoles = append(f.deletedRoles, aws.ToString(in.RoleName))

	return &iam.DeleteRoleOutput{}, nil
}

func (f *fakeIAM) DeleteRolePolicy(_ context.Context, in *iam.DeleteRolePolicyInput, _ ...func(*iam.Options)) (*iam.DeleteRolePolicyOutput, error) {
	f.deletedInlinePs = append(f.deletedInlinePs, aws.ToString(in.PolicyName))

	return &iam.DeleteRolePolicyOutput{}, nil
}

func (f *fakeIAM) DetachRolePolicy(_ context.Context, in *iam.DetachRolePolicyInput, _ ...func(*iam.Options)) (*iam.DetachRolePolicyOutput, error) {
	f.detachedPolicy = append(f.detachedPolicy, aws.ToString(in.PolicyArn))

	return &iam.DetachRolePolicyOutput{}, nil
}

func (f *fakeIAM) GetRole(_ context.Context, in *iam.GetRoleInput, _ ...func(*iam.Options)) (*iam.GetRoleOutput, error) {
	name := aws.ToString(in.RoleName)
	if !f.existingRoles[name] {
		return nil, &iamtypes.NoSuchEntityException{}
	}

	return &iam.GetRoleOutput{
		Role: &iamtypes.Role{Arn: aws.String("arn:aws:iam::" + testAccountID + ":role/" + name)},
	}, nil
}

func (f *fakeIAM) UpdateAssumeRolePolicy(_ context.Context, in *iam.UpdateAssumeRolePolicyInput, _ ...func(*iam.Options)) (*iam.UpdateAssumeRolePolicyOutput, error) {
	f.trustPolicies[aws.ToString(in.RoleName)] = aws.ToString(in.PolicyDocument)

	return &iam.UpdateAssumeRolePolicyOutput{}, nil
}

func (f *fakeIAM) PutRolePolicy(_ context.Context, in *iam.PutRolePolicyInput, _ ...func(*iam.Options)) (*iam.PutRolePolicyOutput, error) {
	f.inlinePolicies[aws.ToString(in.RoleName)] = aws.ToString(in.PolicyDocument)

	return &iam.PutRolePolicyOutput{}, nil
}

type fakeSTS struct {
	account string
}

func (f fakeSTS) GetCallerIdentity(_ context.Context, _ *sts.GetCallerIdentityInput, _ ...func(*sts.Options)) (*sts.GetCallerIdentityOutput, error) {
	return &sts.GetCallerIdentityOutput{Account: aws.String(f.account)}, nil
}

func newTestClients(agent *fakeAgent, iamClient *fakeIAM) awsdevops.Clients {
	return awsdevops.Clients{
		Agent:  agent,
		IAM:    iamClient,
		STS:    fakeSTS{account: testAccountID},
		Region: "us-east-1",
	}
}

func TestSetupCreatesRolesAgentSpaceAndAssociations(t *testing.T) {
	agent := &fakeAgent{}
	iamClient := newFakeIAM()

	result, err := awsdevops.Setup(context.Background(), newTestClients(agent, iamClient), awsdevops.SetupOptions{
		AgentSpaceName: "launchdarkly",
		AuthFlow:       "iam",
		LDAccessToken:  "api-token",
	})
	require.NoError(t, err)

	assert.Equal(t, testAccountID, result.AccountID)
	assert.Equal(t, "space-1", result.AgentSpaceID)
	assert.Equal(t, "mcp-1", result.MCPServiceID)
	assert.Equal(t, "assoc-mcp-1", result.MCPAssociationID)
	assert.Equal(t, "https://space-1.aidevops.global.app.aws", result.OperatorAppURL)
	assert.Equal(t, []string{awsdevops.AgentSpaceRoleName, awsdevops.OperatorAppRoleName}, iamClient.createdRoles)
	assert.Equal(t, awsdevops.AgentSpacePolicyARN, iamClient.attachedPolicy[awsdevops.AgentSpaceRoleName])
	assert.Equal(t, awsdevops.OperatorAppPolicyARN, iamClient.attachedPolicy[awsdevops.OperatorAppRoleName])
	assert.Contains(t, iamClient.inlinePolicies[awsdevops.AgentSpaceRoleName], "iam:CreateServiceLinkedRole")
	assert.Equal(
		t,
		[]string{"CreateAgentSpace", "AssociateService", "EnableOperatorApp", "ListServices", "RegisterService", "AssociateService"},
		agent.calls,
	)
}

func TestSetupReusesExistingRolesAndAgentSpace(t *testing.T) {
	agent := &fakeAgent{}
	iamClient := newFakeIAM()
	iamClient.existingRoles[awsdevops.AgentSpaceRoleName] = true
	iamClient.existingRoles[awsdevops.OperatorAppRoleName] = true

	result, err := awsdevops.Setup(context.Background(), newTestClients(agent, iamClient), awsdevops.SetupOptions{
		AgentSpaceID: "space-existing",
		AuthFlow:     "iam",
	})
	require.NoError(t, err)

	assert.Equal(t, "space-existing", result.AgentSpaceID)
	assert.NotContains(t, agent.calls, "CreateAgentSpace")
	assert.Empty(t, iamClient.createdRoles)
	assert.Equal(t, "arn:aws:iam::"+testAccountID+":role/"+awsdevops.AgentSpaceRoleName, result.AgentSpaceRoleARN)
}

func TestSetupRetriesTheAccountAssociationUntilTheRoleIsAssumable(t *testing.T) {
	agent := &fakeAgent{associateFailures: 1}

	result, err := awsdevops.Setup(context.Background(), newTestClients(agent, newFakeIAM()), awsdevops.SetupOptions{
		AgentSpaceName: "launchdarkly",
		AuthFlow:       "iam",
	})
	require.NoError(t, err)

	assert.Equal(t, "assoc-aws", result.AWSAssociationID)
	assert.Len(t, agent.associationInputs, 2)
}

func TestSetupReusesAgentSpaceAndAssociations(t *testing.T) {
	agent := &fakeAgent{
		agentSpaces: []agenttypes.AgentSpace{{
			AgentSpaceId: aws.String("space-existing"),
			Name:         aws.String("launchdarkly"),
		}},
		associations: []agenttypes.Association{{
			AssociationId: aws.String("assoc-aws"),
			ServiceId:     aws.String("aws"),
		}},
	}

	result, err := awsdevops.Setup(context.Background(), newTestClients(agent, newFakeIAM()), awsdevops.SetupOptions{
		AgentSpaceName: "launchdarkly",
		AuthFlow:       "iam",
	})
	require.NoError(t, err)

	assert.Equal(t, "space-existing", result.AgentSpaceID)
	assert.Equal(t, "assoc-aws", result.AWSAssociationID)
	assert.NotContains(t, agent.calls, "CreateAgentSpace")
	assert.Empty(t, agent.associationInputs)
}

func TestSetupRefreshesTheTrustPolicyOnAReusedRole(t *testing.T) {
	agent := &fakeAgent{}
	iamClient := newFakeIAM()
	iamClient.existingRoles[awsdevops.AgentSpaceRoleName] = true
	iamClient.existingRoles[awsdevops.OperatorAppRoleName] = true

	_, err := awsdevops.Setup(context.Background(), newTestClients(agent, iamClient), awsdevops.SetupOptions{
		AgentSpaceID: "space-existing",
		AuthFlow:     "iam",
	})
	require.NoError(t, err)

	for _, name := range []string{awsdevops.AgentSpaceRoleName, awsdevops.OperatorAppRoleName} {
		assert.Contains(t, iamClient.trustPolicies[name], "arn:aws:aidevops:us-east-1:"+testAccountID+":agentspace/*")
	}
}

func TestSetupReusesAnExistingMCPServerWithoutAToken(t *testing.T) {
	agent := &fakeAgent{
		services: []agenttypes.RegisteredService{{
			ServiceId: aws.String("mcp-existing"),
			Name:      aws.String("LaunchDarkly"),
		}},
	}

	result, err := awsdevops.Setup(context.Background(), newTestClients(agent, newFakeIAM()), awsdevops.SetupOptions{
		AgentSpaceName: "launchdarkly",
		AuthFlow:       "iam",
	})
	require.NoError(t, err)

	assert.Equal(t, "mcp-existing", result.MCPServiceID)
	assert.Equal(t, "assoc-mcp-existing", result.MCPAssociationID)
	assert.NotContains(t, agent.calls, "RegisterService")
}

func TestSetupWithoutATokenLeavesTheMCPServerUnconnected(t *testing.T) {
	agent := &fakeAgent{}

	result, err := awsdevops.Setup(context.Background(), newTestClients(agent, newFakeIAM()), awsdevops.SetupOptions{
		AgentSpaceName: "launchdarkly",
		AuthFlow:       "iam",
	})
	require.NoError(t, err)

	assert.Empty(t, result.MCPServiceID)
	assert.NotContains(t, agent.calls, "RegisterService")
}

func TestSetupDisassociatesTheMCPServerBeforeReplacingItsToken(t *testing.T) {
	agent := &fakeAgent{
		services: []agenttypes.RegisteredService{{
			ServiceId: aws.String("mcp-existing"),
			Name:      aws.String("LaunchDarkly"),
		}},
		associations: []agenttypes.Association{{
			AssociationId: aws.String("assoc-mcp-existing"),
			ServiceId:     aws.String("mcp-existing"),
		}},
	}

	result, err := awsdevops.Setup(context.Background(), newTestClients(agent, newFakeIAM()), awsdevops.SetupOptions{
		AgentSpaceName:  "launchdarkly",
		AuthFlow:        "iam",
		LDAccessToken:   "api-token",
		ReplaceMCPToken: true,
	})
	require.NoError(t, err)

	assert.Equal(t, []string{"assoc-mcp-existing"}, agent.disassociatedIDs)
	assert.Equal(t, []string{"mcp-existing"}, agent.deregisteredIDs)
	assert.Less(t, indexOf(agent.calls, "DisassociateService"), indexOf(agent.calls, "DeregisterService"))
	assert.Equal(t, "mcp-1", result.MCPServiceID)
	assert.Equal(t, "assoc-mcp-1", result.MCPAssociationID)
}

func TestTeardownDisassociatesAServiceBeforeDeregisteringIt(t *testing.T) {
	agent := &fakeAgent{
		agentSpaces: []agenttypes.AgentSpace{{AgentSpaceId: aws.String("space-1")}},
		associations: []agenttypes.Association{{
			AssociationId: aws.String("assoc-mcp-1"),
			ServiceId:     aws.String("mcp-1"),
		}},
	}

	require.NoError(t, awsdevops.Teardown(context.Background(), newTestClients(agent, newFakeIAM()), awsdevops.TeardownOptions{
		ServiceIDs: []string{"mcp-1"},
	}))

	assert.Equal(t, []string{"assoc-mcp-1"}, agent.disassociatedIDs)
	assert.Equal(t, []string{"mcp-1"}, agent.deregisteredIDs)
	assert.Less(t, indexOf(agent.calls, "DisassociateService"), indexOf(agent.calls, "DeregisterService"))
}

func TestTeardownFollowsEveryListPage(t *testing.T) {
	agent := &fakeAgent{
		paginate: true,
		agentSpaces: []agenttypes.AgentSpace{
			{AgentSpaceId: aws.String("space-1")},
			{AgentSpaceId: aws.String("space-2")},
		},
		services: []agenttypes.RegisteredService{
			{ServiceId: aws.String("svc-1")},
			{ServiceId: aws.String("mcp-1")},
		},
		associations: []agenttypes.Association{
			{AssociationId: aws.String("assoc-1"), ServiceId: aws.String("aws")},
			{AssociationId: aws.String("assoc-2"), ServiceId: aws.String("mcp-1")},
		},
		assets: []agenttypes.Asset{
			{AssetId: aws.String("asset-1"), AssetType: aws.String("skill")},
			{AssetId: aws.String("asset-2"), AssetType: aws.String("skill")},
		},
	}

	require.NoError(t, awsdevops.Teardown(context.Background(), newTestClients(agent, newFakeIAM()), awsdevops.TeardownOptions{}))

	assert.Equal(t, []string{"space-1", "space-2"}, agent.deletedAgentSpaces)
	assert.Equal(t, []string{"svc-1", "mcp-1"}, agent.deregisteredIDs)
	assert.Equal(t, []string{"asset-1", "asset-2", "asset-1", "asset-2"}, agent.deletedAssetIDs)
}

func TestStatusFollowsEveryListPage(t *testing.T) {
	agent := &fakeAgent{
		paginate: true,
		agentSpaces: []agenttypes.AgentSpace{
			{AgentSpaceId: aws.String("space-1")},
			{AgentSpaceId: aws.String("space-2")},
		},
		services: []agenttypes.RegisteredService{
			{ServiceId: aws.String("svc-1")},
			{ServiceId: aws.String("mcp-1")},
		},
		associations: []agenttypes.Association{
			{AssociationId: aws.String("assoc-1"), ServiceId: aws.String("aws")},
			{AssociationId: aws.String("assoc-2"), ServiceId: aws.String("mcp-1")},
		},
	}

	status, err := awsdevops.GetStatus(context.Background(), newTestClients(agent, newFakeIAM()), "")
	require.NoError(t, err)

	assert.Len(t, status.Services, 2)
	require.Len(t, status.AgentSpaces, 2)
	assert.Len(t, status.AgentSpaces[0].Associations, 2)
}

func indexOf(calls []string, name string) int {
	for i, call := range calls {
		if call == name {
			return i
		}
	}

	return -1
}

func TestTeardownEmptiesAgentSpaceBeforeDeletingIt(t *testing.T) {
	agent := &fakeAgent{
		associations: []agenttypes.Association{{AssociationId: aws.String("assoc-1"), ServiceId: aws.String("aws")}},
		assets:       []agenttypes.Asset{{AssetId: aws.String("asset-1")}},
	}
	iamClient := newFakeIAM()

	require.NoError(t, awsdevops.Teardown(context.Background(), newTestClients(agent, iamClient), awsdevops.TeardownOptions{
		AgentSpaceID: "space-1",
		ServiceIDs:   []string{"mcp-1"},
		DeleteRoles:  true,
	}))

	assert.Equal(
		t,
		[]string{"DisassociateService", "DeleteAsset", "DeleteAgentSpace", "DeregisterService"},
		agent.calls,
	)
	assert.Equal(t, []string{"mcp-1"}, agent.deregisteredIDs)
	assert.Equal(t, []string{awsdevops.AgentSpaceRoleName, awsdevops.OperatorAppRoleName}, iamClient.deletedRoles)
	assert.Equal(t, []string{awsdevops.ServiceLinkedRolePolicyName}, iamClient.deletedInlinePs)
}

func TestTeardownDeletesMemoryStoresLast(t *testing.T) {
	agent := &fakeAgent{
		assets: []agenttypes.Asset{
			{AssetId: aws.String("store-1"), AssetType: aws.String("memory_store")},
			{AssetId: aws.String("memory-1"), AssetType: aws.String("memory")},
			{AssetId: aws.String("skill-1"), AssetType: aws.String("skill")},
		},
	}

	require.NoError(t, awsdevops.Teardown(context.Background(), newTestClients(agent, newFakeIAM()), awsdevops.TeardownOptions{
		AgentSpaceID: "space-1",
	}))

	assert.Equal(t, []string{"memory-1", "skill-1", "store-1"}, agent.deletedAssetIDs)
}

func TestTeardownWithoutOptionsRemovesEverything(t *testing.T) {
	agent := &fakeAgent{
		agentSpaces: []agenttypes.AgentSpace{
			{AgentSpaceId: aws.String("space-1")},
			{AgentSpaceId: aws.String("space-2")},
		},
		services: []agenttypes.RegisteredService{
			{ServiceId: aws.String("svc-1"), ServiceType: agenttypes.ServiceGitlab},
			{ServiceId: aws.String("mcp-1"), ServiceType: agenttypes.ServiceMcpServer},
		},
	}
	iamClient := newFakeIAM()

	require.NoError(t, awsdevops.Teardown(context.Background(), newTestClients(agent, iamClient), awsdevops.TeardownOptions{}))

	assert.Equal(t, []string{"space-1", "space-2"}, agent.deletedAgentSpaces)
	assert.Equal(t, []string{"svc-1", "mcp-1"}, agent.deregisteredIDs)
	assert.Equal(t, []string{awsdevops.AgentSpaceRoleName, awsdevops.OperatorAppRoleName}, iamClient.deletedRoles)
}

func TestTeardownStillRemovesEverythingWithDeleteRoles(t *testing.T) {
	agent := &fakeAgent{
		agentSpaces: []agenttypes.AgentSpace{{AgentSpaceId: aws.String("space-1")}},
		services:    []agenttypes.RegisteredService{{ServiceId: aws.String("mcp-1")}},
	}

	require.NoError(t, awsdevops.Teardown(context.Background(), newTestClients(agent, newFakeIAM()), awsdevops.TeardownOptions{
		DeleteRoles: true,
	}))

	assert.Equal(t, []string{"space-1"}, agent.deletedAgentSpaces)
	assert.Equal(t, []string{"mcp-1"}, agent.deregisteredIDs)
}

func TestStatusReportsMissingRoles(t *testing.T) {
	agent := &fakeAgent{
		associations: []agenttypes.Association{{AssociationId: aws.String("assoc-1"), ServiceId: aws.String("aws")}},
		assets:       []agenttypes.Asset{{AssetId: aws.String("asset-1"), AssetType: aws.String("skill")}},
	}
	iamClient := newFakeIAM()
	iamClient.existingRoles[awsdevops.AgentSpaceRoleName] = true

	status, err := awsdevops.GetStatus(context.Background(), newTestClients(agent, iamClient), "")
	require.NoError(t, err)

	assert.True(t, status.Roles[0].Exists)
	assert.False(t, status.Roles[1].Exists)
	assert.Equal(t, []string{"IAM role " + awsdevops.OperatorAppRoleName + " does not exist"}, status.MissingSetup)
	require.Len(t, status.AgentSpaces, 1)
	assert.Equal(t, "space-1", status.AgentSpaces[0].AgentSpaceID)
	assert.Equal(t, "aws", status.AgentSpaces[0].Associations[0].ServiceID)
}

func TestManualStepURLsPointAtRegistrationPages(t *testing.T) {
	assert.Equal(
		t,
		"https://eu-west-1.console.aws.amazon.com/aidevops/home?region=eu-west-1",
		awsdevops.ConsoleURL("eu-west-1"),
	)
	assert.Equal(
		t,
		"https://app.launchdarkly.com/settings/authorization",
		awsdevops.AccessTokenURL(""),
	)
}

func TestNewClientsRejectsUnsupportedRegion(t *testing.T) {
	_, err := awsdevops.NewClients(context.Background(), "us-east-2", "")

	assert.ErrorContains(t, err, "AWS DevOps Agent is not available in us-east-2")
}

func TestNewClientsRequiresCredentials(t *testing.T) {
	t.Setenv("AWS_ACCESS_KEY_ID", "")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "")
	t.Setenv("AWS_SESSION_TOKEN", "")
	t.Setenv("AWS_PROFILE", "")
	t.Setenv("AWS_CONFIG_FILE", "/dev/null")
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", "/dev/null")
	t.Setenv("AWS_EC2_METADATA_DISABLED", "true")

	_, err := awsdevops.NewClients(context.Background(), "us-east-1", "")

	assert.ErrorIs(t, err, awsdevops.ErrNoCredentials)
	assert.ErrorContains(t, err, "Authenticate first")
}

func TestNewClientsSuggestsSelectingAProfile(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config")
	require.NoError(t, os.WriteFile(configPath, []byte(
		"[profile dev]\nregion = us-east-1\n\n[profile prod]\nregion = us-west-2\n\n[sso-session corp]\nsso_region = us-east-1\n",
	), 0o600))

	t.Setenv("AWS_ACCESS_KEY_ID", "")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "")
	t.Setenv("AWS_SESSION_TOKEN", "")
	t.Setenv("AWS_PROFILE", "")
	t.Setenv("AWS_CONFIG_FILE", configPath)
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", "/dev/null")
	t.Setenv("AWS_EC2_METADATA_DISABLED", "true")

	_, err := awsdevops.NewClients(context.Background(), "us-east-1", "")

	assert.ErrorIs(t, err, awsdevops.ErrNoCredentials)
	assert.ErrorContains(t, err, "no AWS profile selected")
	assert.ErrorContains(t, err, "--profile")
	assert.ErrorContains(t, err, "dev")
	assert.ErrorContains(t, err, "prod")
	assert.NotContains(t, err.Error(), "corp")
}

func TestNewClientsNamesTheSelectedProfile(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config")
	// The profile exists but has no usable credentials (no static keys, no
	// SSO config), so the SDK loads it and then fails to resolve credentials.
	require.NoError(t, os.WriteFile(configPath, []byte(
		"[profile my-profile]\nregion = us-east-1\n",
	), 0o600))

	t.Setenv("AWS_ACCESS_KEY_ID", "")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "")
	t.Setenv("AWS_SESSION_TOKEN", "")
	t.Setenv("AWS_PROFILE", "")
	t.Setenv("AWS_CONFIG_FILE", configPath)
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", "/dev/null")
	t.Setenv("AWS_EC2_METADATA_DISABLED", "true")

	_, err := awsdevops.NewClients(context.Background(), "us-east-1", "my-profile")

	assert.ErrorIs(t, err, awsdevops.ErrNoCredentials)
	assert.ErrorContains(t, err, `profile "my-profile"`)
	assert.ErrorContains(t, err, "aws sso login --profile my-profile")
}
