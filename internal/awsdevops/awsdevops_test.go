package awsdevops_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
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
	calls             []string
	services          []agenttypes.RegisteredService
	associationInputs []*devopsagent.AssociateServiceInput
	registerOutput    *devopsagent.RegisterServiceOutput
	registerErr       error
	registerInputs    []*devopsagent.RegisterServiceInput
	associations      []agenttypes.Association
	agentSpaces       []agenttypes.AgentSpace
	disassociatedIDs  []string
	deregisteredIDs   []string
	associateFailures int
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

func (f *fakeAgent) ListAssociations(_ context.Context, in *devopsagent.ListAssociationsInput, _ ...func(*devopsagent.Options)) (*devopsagent.ListAssociationsOutput, error) {
	items, next := page(f.associations, in.NextToken, f.paginate)

	return &devopsagent.ListAssociationsOutput{Associations: items, NextToken: next}, nil
}

func (f *fakeAgent) RegisterService(_ context.Context, in *devopsagent.RegisterServiceInput, _ ...func(*devopsagent.Options)) (*devopsagent.RegisterServiceOutput, error) {
	f.calls = append(f.calls, "RegisterService")
	f.registerInputs = append(f.registerInputs, in)
	if f.registerErr != nil {
		return nil, f.registerErr
	}
	if f.registerOutput != nil {
		return f.registerOutput, nil
	}

	return &devopsagent.RegisterServiceOutput{ServiceId: aws.String("mcp-1")}, nil
}

type fakeIAM struct {
	existingRoles  map[string]bool
	roleTags       map[string][]iamtypes.Tag
	createdRoles   []string
	attachedPolicy map[string]string
	inlinePolicies map[string]string
	trustPolicies  map[string]string
}

func newFakeIAM() *fakeIAM {
	return &fakeIAM{
		existingRoles:  map[string]bool{},
		roleTags:       map[string][]iamtypes.Tag{},
		attachedPolicy: map[string]string{},
		inlinePolicies: map[string]string{},
		trustPolicies:  map[string]string{},
	}
}

// addManagedRole registers a role this CLI created in an earlier run.
func (f *fakeIAM) addManagedRole(name string) {
	f.existingRoles[name] = true
	f.roleTags[name] = []iamtypes.Tag{{
		Key:   aws.String(awsdevops.ManagedTagKey),
		Value: aws.String(awsdevops.ManagedTagValue),
	}}
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
	f.roleTags[name] = in.Tags

	return &iam.CreateRoleOutput{
		Role: &iamtypes.Role{Arn: aws.String("arn:aws:iam::" + testAccountID + ":role/" + name)},
	}, nil
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

func (f *fakeIAM) ListRoleTags(_ context.Context, in *iam.ListRoleTagsInput, _ ...func(*iam.Options)) (*iam.ListRoleTagsOutput, error) {
	return &iam.ListRoleTagsOutput{Tags: f.roleTags[aws.ToString(in.RoleName)]}, nil
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
		LDBaseURI:      stubLaunchDarkly(t, http.StatusOK),
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
	iamClient.addManagedRole(awsdevops.AgentSpaceRoleName)
	iamClient.addManagedRole(awsdevops.OperatorAppRoleName)

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

func TestSetupTrustsAgentSpacesInEveryRegion(t *testing.T) {
	agent := &fakeAgent{}
	iamClient := newFakeIAM()
	iamClient.addManagedRole(awsdevops.AgentSpaceRoleName)
	iamClient.addManagedRole(awsdevops.OperatorAppRoleName)

	_, err := awsdevops.Setup(context.Background(), newTestClients(agent, iamClient), awsdevops.SetupOptions{
		AgentSpaceID: "space-existing",
		AuthFlow:     "iam",
	})
	require.NoError(t, err)

	// A region-pinned trust policy would lock out the agent spaces in every
	// other region, since the role names are shared across the account.
	for _, name := range []string{awsdevops.AgentSpaceRoleName, awsdevops.OperatorAppRoleName} {
		assert.Contains(t, iamClient.trustPolicies[name], "arn:aws:aidevops:*:"+testAccountID+":agentspace/*")
	}
}

func TestSetupLeavesARoleItDidNotCreateAlone(t *testing.T) {
	agent := &fakeAgent{}
	iamClient := newFakeIAM()
	iamClient.existingRoles[awsdevops.AgentSpaceRoleName] = true
	iamClient.trustPolicies[awsdevops.AgentSpaceRoleName] = "customer-owned"
	iamClient.addManagedRole(awsdevops.OperatorAppRoleName)

	var logged []string
	result, err := awsdevops.Setup(context.Background(), newTestClients(agent, iamClient), awsdevops.SetupOptions{
		AgentSpaceID: "space-existing",
		AuthFlow:     "iam",
		Logf: func(format string, args ...any) {
			logged = append(logged, fmt.Sprintf(format, args...))
		},
	})
	require.NoError(t, err)

	assert.Equal(t, "arn:aws:iam::"+testAccountID+":role/"+awsdevops.AgentSpaceRoleName, result.AgentSpaceRoleARN)
	assert.Equal(t, "customer-owned", iamClient.trustPolicies[awsdevops.AgentSpaceRoleName])
	assert.Empty(t, iamClient.attachedPolicy[awsdevops.AgentSpaceRoleName])
	assert.Empty(t, iamClient.inlinePolicies[awsdevops.AgentSpaceRoleName])
	assert.Contains(t, strings.Join(logged, "\n"), "was not created by this CLI")
}

// launchDarklyMCPService is how AWS reports our registration: the endpoint is
// what identifies it, and it lives in the type-specific details.
func launchDarklyMCPService(serviceID string) agenttypes.RegisteredService {
	return mcpService(serviceID, awsdevops.MCPServerName, awsdevops.MCPServerEndpoint)
}

func mcpService(serviceID, name, endpoint string) agenttypes.RegisteredService {
	return agenttypes.RegisteredService{
		ServiceId:   aws.String(serviceID),
		ServiceType: agenttypes.ServiceMcpServer,
		Name:        aws.String(name),
		AdditionalServiceDetails: &agenttypes.AdditionalServiceDetailsMemberMcpserver{
			Value: agenttypes.RegisteredMCPServerDetails{
				Name:     aws.String(name),
				Endpoint: aws.String(endpoint),
			},
		},
	}
}

// mintedToken is the service token stubLaunchDarkly issues.
const mintedToken = "minted-service-token"

// stubLaunchDarkly stands in for the LaunchDarkly API that checks access
// tokens and issues service tokens, and returns its base URI. status is what
// it answers both with, so a test can make the caller's token unusable.
func stubLaunchDarkly(t *testing.T, status int) string {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "api-token", r.Header.Get("Authorization"))

		switch {
		case r.URL.Path == "/api/v2/caller-identity":
			w.WriteHeader(status)
		case r.URL.Path == "/api/v2/tokens" && status == http.StatusOK:
			var body map[string]any
			require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
			assert.Equal(t, true, body["serviceToken"])
			assert.Contains(t, body["name"], "AWS DevOps Agent")

			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"_id":"token-1","name":"` + body["name"].(string) + `","token":"` + mintedToken + `"}`))
		case r.URL.Path == "/api/v2/tokens":
			w.WriteHeader(status)
		default:
			t.Errorf("unexpected request to %s", r.URL.Path)
		}
	}))
	t.Cleanup(server.Close)

	return server.URL
}

func TestSetupReusesAnExistingMCPServerWithoutAToken(t *testing.T) {
	agent := &fakeAgent{
		services: []agenttypes.RegisteredService{launchDarklyMCPService("mcp-existing")},
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
		services: []agenttypes.RegisteredService{launchDarklyMCPService("mcp-existing")},
		associations: []agenttypes.Association{{
			AssociationId: aws.String("assoc-mcp-existing"),
			ServiceId:     aws.String("mcp-existing"),
		}},
	}

	result, err := awsdevops.Setup(context.Background(), newTestClients(agent, newFakeIAM()), awsdevops.SetupOptions{
		AgentSpaceName:  "launchdarkly",
		AuthFlow:        "iam",
		LDBaseURI:       stubLaunchDarkly(t, http.StatusOK),
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

func TestSetupReconnectsOtherSpacesAfterReplacingTheMCPToken(t *testing.T) {
	agent := &fakeAgent{
		agentSpaces: []agenttypes.AgentSpace{
			{AgentSpaceId: aws.String("space-1")},
			{AgentSpaceId: aws.String("space-2")},
		},
		services: []agenttypes.RegisteredService{launchDarklyMCPService("mcp-existing")},
		associations: []agenttypes.Association{{
			AssociationId: aws.String("assoc-mcp-existing"),
			ServiceId:     aws.String("mcp-existing"),
		}},
	}

	_, err := awsdevops.Setup(context.Background(), newTestClients(agent, newFakeIAM()), awsdevops.SetupOptions{
		AgentSpaceID:    "space-1",
		AuthFlow:        "iam",
		LDBaseURI:       stubLaunchDarkly(t, http.StatusOK),
		LDAccessToken:   "api-token",
		ReplaceMCPToken: true,
	})
	require.NoError(t, err)

	associated := []string{}
	for _, in := range agent.associationInputs {
		if aws.ToString(in.ServiceId) == "mcp-1" {
			associated = append(associated, aws.ToString(in.AgentSpaceId))
		}
	}
	assert.Equal(t, []string{"space-1", "space-2"}, associated)
}

func TestSetupKeepsTheMCPServerWhenLaunchDarklyRejectsTheNewToken(t *testing.T) {
	agent := &fakeAgent{
		services: []agenttypes.RegisteredService{launchDarklyMCPService("mcp-existing")},
		associations: []agenttypes.Association{{
			AssociationId: aws.String("assoc-mcp-existing"),
			ServiceId:     aws.String("mcp-existing"),
		}},
	}

	_, err := awsdevops.Setup(context.Background(), newTestClients(agent, newFakeIAM()), awsdevops.SetupOptions{
		AgentSpaceID:    "space-1",
		AuthFlow:        "iam",
		LDBaseURI:       stubLaunchDarkly(t, http.StatusUnauthorized),
		LDAccessToken:   "api-token",
		ReplaceMCPToken: true,
	})

	assert.ErrorIs(t, err, awsdevops.ErrInvalidAccessToken)
	assert.Empty(t, agent.disassociatedIDs)
	assert.Empty(t, agent.deregisteredIDs)
	assert.NotContains(t, agent.calls, "RegisterService")
}

func TestSetupNamesTheSpacesLeftWithoutAnMCPServer(t *testing.T) {
	agent := &fakeAgent{
		registerErr: errors.New("ValidationException: something broke"),
		agentSpaces: []agenttypes.AgentSpace{
			{AgentSpaceId: aws.String("space-1")},
			{AgentSpaceId: aws.String("space-2")},
		},
		services: []agenttypes.RegisteredService{launchDarklyMCPService("mcp-existing")},
		associations: []agenttypes.Association{{
			AssociationId: aws.String("assoc-mcp-existing"),
			ServiceId:     aws.String("mcp-existing"),
		}},
	}

	_, err := awsdevops.Setup(context.Background(), newTestClients(agent, newFakeIAM()), awsdevops.SetupOptions{
		AgentSpaceID:    "space-1",
		AuthFlow:        "iam",
		LDBaseURI:       stubLaunchDarkly(t, http.StatusOK),
		LDAccessToken:   "api-token",
		ReplaceMCPToken: true,
	})

	assert.ErrorContains(t, err, "agent spaces space-1, space-2 have no LaunchDarkly MCP server")
	assert.ErrorContains(t, err, "--access-token")
}

func TestSetupIgnoresAnMCPServerAtAnotherEndpoint(t *testing.T) {
	agent := &fakeAgent{
		services: []agenttypes.RegisteredService{
			mcpService("mcp-self-hosted", awsdevops.MCPServerName, "https://mcp.example.internal/mcp"),
		},
		associations: []agenttypes.Association{{
			AssociationId: aws.String("assoc-mcp-self-hosted"),
			ServiceId:     aws.String("mcp-self-hosted"),
		}},
	}

	result, err := awsdevops.Setup(context.Background(), newTestClients(agent, newFakeIAM()), awsdevops.SetupOptions{
		AgentSpaceID:    "space-1",
		AuthFlow:        "iam",
		LDBaseURI:       stubLaunchDarkly(t, http.StatusOK),
		LDAccessToken:   "api-token",
		ReplaceMCPToken: true,
	})
	require.NoError(t, err)

	assert.Equal(t, "mcp-1", result.MCPServiceID)
	assert.Empty(t, agent.deregisteredIDs)
	assert.Empty(t, agent.disassociatedIDs)
}

func TestSetupUsesTheMCPServerItIsPointedAt(t *testing.T) {
	agent := &fakeAgent{
		services: []agenttypes.RegisteredService{
			mcpService("mcp-renamed", "LaunchDarkly (prod)", "https://mcp.example.internal/mcp"),
		},
	}

	result, err := awsdevops.Setup(context.Background(), newTestClients(agent, newFakeIAM()), awsdevops.SetupOptions{
		AgentSpaceID: "space-1",
		AuthFlow:     "iam",
		MCPServiceID: "mcp-renamed",
	})
	require.NoError(t, err)

	assert.Equal(t, "mcp-renamed", result.MCPServiceID)
	assert.NotContains(t, agent.calls, "RegisterService")
}

func TestSetupRegistersAtTheEndpointItIsGiven(t *testing.T) {
	agent := &fakeAgent{}

	_, err := awsdevops.Setup(context.Background(), newTestClients(agent, newFakeIAM()), awsdevops.SetupOptions{
		AgentSpaceID:  "space-1",
		AuthFlow:      "iam",
		LDBaseURI:     stubLaunchDarkly(t, http.StatusOK),
		LDAccessToken: "api-token",
		MCPEndpoint:   "https://mcp.example.internal/mcp",
	})
	require.NoError(t, err)

	require.Len(t, agent.registerInputs, 1)
	details, ok := agent.registerInputs[0].ServiceDetails.(*agenttypes.ServiceDetailsMemberMcpserver)
	require.True(t, ok)
	assert.Equal(t, "https://mcp.example.internal/mcp", aws.ToString(details.Value.Endpoint))
}

// registeredToken is the bearer token AWS was told to store for the MCP
// server registered by the only RegisterService call.
func registeredToken(t *testing.T, agent *fakeAgent) string {
	t.Helper()

	require.Len(t, agent.registerInputs, 1)
	details, ok := agent.registerInputs[0].ServiceDetails.(*agenttypes.ServiceDetailsMemberMcpserver)
	require.True(t, ok)
	auth, ok := details.Value.AuthorizationConfig.(*agenttypes.MCPServerAuthorizationConfigMemberBearerToken)
	require.True(t, ok)

	return aws.ToString(auth.Value.TokenValue)
}

func TestSetupRegistersAServiceTokenItCreatesRatherThanTheCallers(t *testing.T) {
	agent := &fakeAgent{}

	_, err := awsdevops.Setup(context.Background(), newTestClients(agent, newFakeIAM()), awsdevops.SetupOptions{
		AgentSpaceID:  "space-1",
		AuthFlow:      "iam",
		LDBaseURI:     stubLaunchDarkly(t, http.StatusOK),
		LDAccessToken: "api-token",
	})
	require.NoError(t, err)

	assert.Equal(t, mintedToken, registeredToken(t, agent))
}

func TestSetupRegistersTheTokenItIsGivenWithoutCreatingOne(t *testing.T) {
	agent := &fakeAgent{}
	baseURI := stubLaunchDarkly(t, http.StatusInternalServerError)

	_, err := awsdevops.Setup(context.Background(), newTestClients(agent, newFakeIAM()), awsdevops.SetupOptions{
		AgentSpaceID:   "space-1",
		AuthFlow:       "iam",
		LDBaseURI:      baseURI,
		LDAccessToken:  "api-token",
		MCPAccessToken: "my-own-token",
	})
	require.NoError(t, err)

	// The stub fails every request, so reaching AWS at all shows setup never
	// called LaunchDarkly.
	assert.Equal(t, "my-own-token", registeredToken(t, agent))
}

func TestSetupKeepsTheMCPServerWhenItCannotCreateAServiceToken(t *testing.T) {
	agent := &fakeAgent{
		services: []agenttypes.RegisteredService{launchDarklyMCPService("mcp-existing")},
		associations: []agenttypes.Association{{
			AssociationId: aws.String("assoc-mcp-existing"),
			ServiceId:     aws.String("mcp-existing"),
		}},
	}

	_, err := awsdevops.Setup(context.Background(), newTestClients(agent, newFakeIAM()), awsdevops.SetupOptions{
		AgentSpaceID:    "space-1",
		AuthFlow:        "iam",
		LDBaseURI:       stubLaunchDarkly(t, http.StatusForbidden),
		LDAccessToken:   "api-token",
		ReplaceMCPToken: true,
	})

	assert.ErrorContains(t, err, "not allowed to create access tokens")
	assert.Empty(t, agent.disassociatedIDs)
	assert.Empty(t, agent.deregisteredIDs)
	assert.NotContains(t, agent.calls, "RegisterService")
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

func TestStatusReportsMissingRoles(t *testing.T) {
	agent := &fakeAgent{
		associations: []agenttypes.Association{{AssociationId: aws.String("assoc-1"), ServiceId: aws.String("aws")}},
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
