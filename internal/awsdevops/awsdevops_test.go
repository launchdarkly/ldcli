package awsdevops_test

import (
	"context"
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
	"github.com/aws/aws-sdk-go-v2/service/devopsagent/document"
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
	// tags holds tags by resource ARN.
	tags    map[string]map[string]string
	tagsErr error
	// tagsErrFor fails the tag lookup of one resource ARN.
	tagsErrFor map[string]error
	// createdTags holds the tags of the created space.
	createdTags map[string]string
	// goneSpaces makes GetAgentSpace report not found.
	goneSpaces      map[string]bool
	getSpaceErr     error
	afterListSpaces func()
	// spaceLinks holds associations by space ID.
	spaceLinks map[string][]agenttypes.Association
	// operatorApps holds enabled apps by space.
	operatorApps map[string]*devopsagent.GetOperatorAppOutput
	// operatorAppErr fails every GetOperatorApp call.
	operatorAppErr    error
	calls             []string
	services          []agenttypes.RegisteredService
	associationInputs []*devopsagent.AssociateServiceInput
	registerOutput    *devopsagent.RegisterServiceOutput
	registerErr       error
	deregisterErr     error
	disassociateErr   error
	// onDisassociate runs before DisassociateService returns.
	onDisassociate    func()
	registerInputs    []*devopsagent.RegisterServiceInput
	associations      []agenttypes.Association
	agentSpaces       []agenttypes.AgentSpace
	disassociatedIDs  []string
	deregisteredIDs   []string
	associateFailures int
	// failAssociateFor fails the MCP server association of these spaces.
	failAssociateFor map[string]bool
	// paginate makes every list API return one item per page, so callers are
	// exercised against the NextToken AWS sends for a long list.
	paginate bool
	// assets holds the skill assets of every space.
	assets []agenttypes.Asset
	// skillFiles holds the SKILL.md text by asset ID.
	skillFiles     map[string]string
	listAssetsErr  error
	createAssetErr error
	createAssets   []*devopsagent.CreateAssetInput
	updateAssets   []*devopsagent.UpdateAssetInput
}

// skillAsset is a skill asset named name.
func skillAsset(id, name string) agenttypes.Asset {
	return agenttypes.Asset{
		AssetId:   aws.String(id),
		AssetType: aws.String("skill"),
		Metadata:  document.NewLazyDocument(map[string]any{"name": name, "agent_types": []string{"GENERIC"}}),
	}
}

func (f *fakeAgent) CreateAsset(_ context.Context, in *devopsagent.CreateAssetInput, _ ...func(*devopsagent.Options)) (*devopsagent.CreateAssetOutput, error) {
	f.calls = append(f.calls, "CreateAsset")
	f.createAssets = append(f.createAssets, in)
	if f.createAssetErr != nil {
		return nil, f.createAssetErr
	}
	asset := agenttypes.Asset{AssetId: aws.String("ki-created"), AssetType: in.AssetType, Metadata: in.Metadata}
	f.assets = append(f.assets, asset)

	return &devopsagent.CreateAssetOutput{Asset: &asset}, nil
}

func (f *fakeAgent) GetAssetFile(_ context.Context, in *devopsagent.GetAssetFileInput, _ ...func(*devopsagent.Options)) (*devopsagent.GetAssetFileOutput, error) {
	f.calls = append(f.calls, "GetAssetFile")
	text, ok := f.skillFiles[aws.ToString(in.AssetId)]
	if !ok || aws.ToString(in.Path) != "SKILL.md" {
		return nil, &agenttypes.ResourceNotFoundException{}
	}

	return &devopsagent.GetAssetFileOutput{File: &agenttypes.AssetFile{
		Path:    in.Path,
		Content: &agenttypes.AssetFileBodyMemberText{Value: text},
	}}, nil
}

func (f *fakeAgent) ListAssets(_ context.Context, in *devopsagent.ListAssetsInput, _ ...func(*devopsagent.Options)) (*devopsagent.ListAssetsOutput, error) {
	f.calls = append(f.calls, "ListAssets")
	if f.listAssetsErr != nil {
		return nil, f.listAssetsErr
	}
	items, next := page(f.assets, in.NextToken, f.paginate)

	return &devopsagent.ListAssetsOutput{Items: items, NextToken: next}, nil
}

func (f *fakeAgent) UpdateAsset(_ context.Context, in *devopsagent.UpdateAssetInput, _ ...func(*devopsagent.Options)) (*devopsagent.UpdateAssetOutput, error) {
	f.calls = append(f.calls, "UpdateAsset")
	f.updateAssets = append(f.updateAssets, in)

	return &devopsagent.UpdateAssetOutput{Asset: &agenttypes.Asset{AssetId: in.AssetId}}, nil
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

	if f.failAssociateFor[aws.ToString(in.AgentSpaceId)] && aws.ToString(in.ServiceId) != "aws" {
		return nil, errors.New("AccessDeniedException: not allowed")
	}
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

func (f *fakeAgent) CreateAgentSpace(_ context.Context, in *devopsagent.CreateAgentSpaceInput, _ ...func(*devopsagent.Options)) (*devopsagent.CreateAgentSpaceOutput, error) {
	f.calls = append(f.calls, "CreateAgentSpace")
	f.createdTags = in.Tags

	return &devopsagent.CreateAgentSpaceOutput{
		AgentSpace: &agenttypes.AgentSpace{AgentSpaceId: aws.String("space-1")},
	}, nil
}

func (f *fakeAgent) DeregisterService(_ context.Context, in *devopsagent.DeregisterServiceInput, _ ...func(*devopsagent.Options)) (*devopsagent.DeregisterServiceOutput, error) {
	f.calls = append(f.calls, "DeregisterService")
	if f.deregisterErr != nil {
		return nil, f.deregisterErr
	}
	f.deregisteredIDs = append(f.deregisteredIDs, aws.ToString(in.ServiceId))

	return &devopsagent.DeregisterServiceOutput{}, nil
}

func (f *fakeAgent) DisassociateService(_ context.Context, in *devopsagent.DisassociateServiceInput, _ ...func(*devopsagent.Options)) (*devopsagent.DisassociateServiceOutput, error) {
	f.calls = append(f.calls, "DisassociateService")
	if f.onDisassociate != nil {
		f.onDisassociate()
	}
	if f.disassociateErr != nil {
		return nil, f.disassociateErr
	}
	f.disassociatedIDs = append(f.disassociatedIDs, aws.ToString(in.AssociationId))

	return &devopsagent.DisassociateServiceOutput{}, nil
}

func (f *fakeAgent) GetOperatorApp(_ context.Context, in *devopsagent.GetOperatorAppInput, _ ...func(*devopsagent.Options)) (*devopsagent.GetOperatorAppOutput, error) {
	f.calls = append(f.calls, "GetOperatorApp")
	if f.operatorAppErr != nil {
		return nil, f.operatorAppErr
	}
	app, ok := f.operatorApps[aws.ToString(in.AgentSpaceId)]
	if !ok {
		return nil, &agenttypes.ResourceNotFoundException{}
	}

	return app, nil
}

func (f *fakeAgent) ListTagsForResource(_ context.Context, in *devopsagent.ListTagsForResourceInput, _ ...func(*devopsagent.Options)) (*devopsagent.ListTagsForResourceOutput, error) {
	f.calls = append(f.calls, "ListTagsForResource")
	if f.tagsErr != nil {
		return nil, f.tagsErr
	}
	if err := f.tagsErrFor[aws.ToString(in.ResourceArn)]; err != nil {
		return nil, err
	}

	return &devopsagent.ListTagsForResourceOutput{Tags: f.tags[aws.ToString(in.ResourceArn)]}, nil
}

func (f *fakeAgent) EnableOperatorApp(_ context.Context, _ *devopsagent.EnableOperatorAppInput, _ ...func(*devopsagent.Options)) (*devopsagent.EnableOperatorAppOutput, error) {
	f.calls = append(f.calls, "EnableOperatorApp")

	return &devopsagent.EnableOperatorAppOutput{
		OperatorAppUrl: aws.String("https://space-1.aidevops.global.app.aws"),
	}, nil
}

func (f *fakeAgent) GetAgentSpace(_ context.Context, in *devopsagent.GetAgentSpaceInput, _ ...func(*devopsagent.Options)) (*devopsagent.GetAgentSpaceOutput, error) {
	id := aws.ToString(in.AgentSpaceId)
	if f.goneSpaces[id] {
		return nil, &agenttypes.ResourceNotFoundException{}
	}
	if f.getSpaceErr != nil {
		return nil, f.getSpaceErr
	}
	name := "launchdarkly"
	var description *string
	for _, space := range f.agentSpaces {
		if aws.ToString(space.AgentSpaceId) == id && space.Name != nil {
			name = aws.ToString(space.Name)
			description = space.Description
		}
	}

	return &devopsagent.GetAgentSpaceOutput{
		AgentSpace: &agenttypes.AgentSpace{AgentSpaceId: in.AgentSpaceId, Name: aws.String(name), Description: description},
	}, nil
}

func (f *fakeAgent) ListAgentSpaces(_ context.Context, in *devopsagent.ListAgentSpacesInput, _ ...func(*devopsagent.Options)) (*devopsagent.ListAgentSpacesOutput, error) {
	spaces := f.agentSpaces
	if spaces == nil {
		spaces = []agenttypes.AgentSpace{{AgentSpaceId: aws.String("space-1")}}
	}
	items, next := page(spaces, in.NextToken, f.paginate)
	if f.afterListSpaces != nil {
		f.afterListSpaces()
	}

	return &devopsagent.ListAgentSpacesOutput{AgentSpaces: items, NextToken: next}, nil
}

func (f *fakeAgent) ListServices(_ context.Context, in *devopsagent.ListServicesInput, _ ...func(*devopsagent.Options)) (*devopsagent.ListServicesOutput, error) {
	f.calls = append(f.calls, "ListServices")
	items, next := page(f.services, in.NextToken, f.paginate)

	return &devopsagent.ListServicesOutput{Services: items, NextToken: next}, nil
}

func (f *fakeAgent) ListAssociations(_ context.Context, in *devopsagent.ListAssociationsInput, _ ...func(*devopsagent.Options)) (*devopsagent.ListAssociationsOutput, error) {
	links := f.associations
	if f.spaceLinks != nil {
		links = f.spaceLinks[aws.ToString(in.AgentSpaceId)]
	}
	items, next := page(links, in.NextToken, f.paginate)

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
	// appearOnCreate makes a role appear on CreateRole.
	appearOnCreate map[string][]iamtypes.Tag
	// failOn fails a call, keyed "Call role".
	failOn         map[string]error
	onCreateRole   func(name string)
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
	if err := f.failOn["AttachRolePolicy "+aws.ToString(in.RoleName)]; err != nil {
		return nil, err
	}
	f.attachedPolicy[aws.ToString(in.RoleName)] = aws.ToString(in.PolicyArn)

	return &iam.AttachRolePolicyOutput{}, nil
}

func (f *fakeIAM) CreateRole(_ context.Context, in *iam.CreateRoleInput, _ ...func(*iam.Options)) (*iam.CreateRoleOutput, error) {
	name := aws.ToString(in.RoleName)
	if f.onCreateRole != nil {
		f.onCreateRole(name)
	}
	if tags, ok := f.appearOnCreate[name]; ok {
		f.existingRoles[name] = true
		f.roleTags[name] = tags
	}
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
	if err := f.failOn["UpdateAssumeRolePolicy "+aws.ToString(in.RoleName)]; err != nil {
		return nil, err
	}
	f.trustPolicies[aws.ToString(in.RoleName)] = aws.ToString(in.PolicyDocument)

	return &iam.UpdateAssumeRolePolicyOutput{}, nil
}

func (f *fakeIAM) PutRolePolicy(_ context.Context, in *iam.PutRolePolicyInput, _ ...func(*iam.Options)) (*iam.PutRolePolicyOutput, error) {
	if err := f.failOn["PutRolePolicy "+aws.ToString(in.RoleName)]; err != nil {
		return nil, err
	}
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
	assert.Equal(t, "ki-created", result.SkillAssetID)
	assert.Equal(t, map[string]string{awsdevops.ManagedTagKey: awsdevops.ManagedTagValue}, agent.createdTags)
	assert.Equal(t, []string{awsdevops.AgentSpaceRoleName, awsdevops.OperatorAppRoleName}, iamClient.createdRoles)
	assert.Equal(t, awsdevops.AgentSpacePolicyARN, iamClient.attachedPolicy[awsdevops.AgentSpaceRoleName])
	assert.Equal(t, awsdevops.OperatorAppPolicyARN, iamClient.attachedPolicy[awsdevops.OperatorAppRoleName])
	assert.Contains(t, iamClient.inlinePolicies[awsdevops.AgentSpaceRoleName], "iam:CreateServiceLinkedRole")
	assert.Equal(
		t,
		// The plan does not look for the server of a new space. The check
		// before the MCP step looks, because the space exists by then. The
		// skill goes before the MCP step.
		[]string{
			"CreateAgentSpace", "AssociateService", "EnableOperatorApp",
			"ListAssets", "CreateAsset", "ListServices", "RegisterService", "AssociateService",
		},
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
		associations: []agenttypes.Association{monitorAssociation("assoc-aws", testAccountID)},
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
	assert.Contains(t, strings.Join(logged, "\n"), "ldcli did not change it")
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

// stubLaunchDarkly stands in for the LaunchDarkly API the access token is
// checked against, and returns its base URI.
func stubLaunchDarkly(t *testing.T, status int) string {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v2/caller-identity", r.URL.Path)
		assert.Equal(t, "api-token", r.Header.Get("Authorization"))
		w.WriteHeader(status)
	}))
	t.Cleanup(server.Close)

	return server.URL
}

func TestSetupReusesAnExistingMCPServerWithoutAToken(t *testing.T) {
	agent := &fakeAgent{
		services:     []agenttypes.RegisteredService{launchDarklyMCPService("mcp-existing")},
		associations: mcpLink("mcp-existing"),
	}

	result, err := awsdevops.Setup(context.Background(), newTestClients(agent, newFakeIAM()), awsdevops.SetupOptions{
		AgentSpaceID: "space-1",
		AuthFlow:     "iam",
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
		AgentSpaceID:    "space-1",
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

func TestSetupNamesTheSpaceLeftWithoutAnMCPServer(t *testing.T) {
	agent := &fakeAgent{
		registerErr: errors.New("ValidationException: something broke"),
		services:    []agenttypes.RegisteredService{launchDarklyMCPService("mcp-existing")},
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

	assert.ErrorContains(t, err, "agent spaces space-1 have no LaunchDarkly MCP server")
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
		MCPEndpoint:  "https://mcp.example.internal/mcp",
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
		LDAccessToken: "api-token",
		MCPEndpoint:   "https://mcp.example.internal/mcp",
	})
	require.NoError(t, err)

	require.Len(t, agent.registerInputs, 1)
	details, ok := agent.registerInputs[0].ServiceDetails.(*agenttypes.ServiceDetailsMemberMcpserver)
	require.True(t, ok)
	assert.Equal(t, "https://mcp.example.internal/mcp", aws.ToString(details.Value.Endpoint))
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

// The primary account and another account both have the service ID "aws".
// Only the configuration tells them apart.
func TestStatusReadsTheAccountOfEachAWSAssociation(t *testing.T) {
	agent := &fakeAgent{
		associations: []agenttypes.Association{
			{
				AssociationId: aws.String("assoc-1"), ServiceId: aws.String("aws"),
				Configuration: &agenttypes.ServiceConfigurationMemberAws{Value: agenttypes.AWSConfiguration{
					AccountId: aws.String(testAccountID), AccountType: agenttypes.MonitorAccountTypeMonitor,
				}},
			},
			{
				AssociationId: aws.String("assoc-2"), ServiceId: aws.String("aws"),
				Configuration: &agenttypes.ServiceConfigurationMemberSourceAws{Value: agenttypes.SourceAwsConfiguration{
					AccountId: aws.String("210987654321"), AccountType: agenttypes.SourceAccountTypeSource,
				}},
			},
			{AssociationId: aws.String("assoc-3"), ServiceId: aws.String("mcp-1")},
		},
	}

	status, err := awsdevops.GetStatus(context.Background(), newTestClients(agent, newFakeIAM()), "")
	require.NoError(t, err)

	require.Len(t, status.AgentSpaces, 1)
	associations := status.AgentSpaces[0].Associations
	require.Len(t, associations, 3)
	assert.Equal(t, testAccountID, associations[0].AccountID)
	assert.Equal(t, "monitor", associations[0].AccountType)
	assert.Equal(t, "210987654321", associations[1].AccountID)
	assert.Equal(t, "source", associations[1].AccountType)
	assert.Empty(t, associations[2].AccountID)
	assert.Empty(t, associations[2].AccountType)
}

func TestStatusReadsTheToolsOfTheMCPServerAssociation(t *testing.T) {
	agent := &fakeAgent{
		associations: []agenttypes.Association{
			{
				AssociationId: aws.String("assoc-1"), ServiceId: aws.String("mcp-1"),
				Configuration: &agenttypes.ServiceConfigurationMemberMcpserver{Value: agenttypes.MCPServerConfiguration{
					Tools: []string{"list-flags", "get-flag"},
				}},
			},
			{AssociationId: aws.String("assoc-2"), ServiceId: aws.String("aws")},
		},
	}

	status, err := awsdevops.GetStatus(context.Background(), newTestClients(agent, newFakeIAM()), "")
	require.NoError(t, err)

	require.Len(t, status.AgentSpaces, 1)
	associations := status.AgentSpaces[0].Associations
	require.Len(t, associations, 2)
	assert.Equal(t, []string{"list-flags", "get-flag"}, associations[0].Tools)
	assert.Empty(t, associations[1].Tools)
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
	_, err := awsdevops.NewClients(context.Background(), "us-east-3", "")

	assert.ErrorContains(t, err, "AWS DevOps Agent is not available in us-east-3")
}

func TestNewClientsRequiresCredentials(t *testing.T) {
	t.Setenv("AWS_ACCESS_KEY_ID", "")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "")
	t.Setenv("AWS_SESSION_TOKEN", "")
	t.Setenv("AWS_PROFILE", "")
	t.Setenv("AWS_DEFAULT_PROFILE", "")
	t.Setenv("AWS_CONFIG_FILE", "/dev/null")
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", "/dev/null")
	t.Setenv("AWS_EC2_METADATA_DISABLED", "true")

	_, err := awsdevops.NewClients(context.Background(), "us-east-1", "")

	assert.ErrorIs(t, err, awsdevops.ErrNoCredentials)
	assert.ErrorContains(t, err, "aws sso login")
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
	t.Setenv("AWS_DEFAULT_PROFILE", "")
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
	t.Setenv("AWS_DEFAULT_PROFILE", "")
	t.Setenv("AWS_CONFIG_FILE", configPath)
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", "/dev/null")
	t.Setenv("AWS_EC2_METADATA_DISABLED", "true")

	_, err := awsdevops.NewClients(context.Background(), "us-east-1", "my-profile")

	assert.ErrorIs(t, err, awsdevops.ErrNoCredentials)
	assert.ErrorContains(t, err, `profile "my-profile" has no AWS credentials`)
	assert.NotContains(t, err.Error(), "sso login")
	assert.NotContains(t, err.Error(), "ec2imds")
}

// The agent calls a read-only tool with no approval, so every tool that
// changes LaunchDarkly state must be mutative.
func TestMCPAssociationClassifiesTheSkillTools(t *testing.T) {
	agent := &fakeAgent{}

	_, err := awsdevops.Setup(context.Background(), newTestClients(agent, newFakeIAM()), awsdevops.SetupOptions{
		AgentSpaceName: "launchdarkly",
		AuthFlow:       "iam",
		LDAccessToken:  "api-token",
	})
	require.NoError(t, err)

	var config agenttypes.MCPServerConfiguration
	for _, in := range agent.associationInputs {
		if mcp, ok := in.Configuration.(*agenttypes.ServiceConfigurationMemberMcpserver); ok {
			config = mcp.Value
		}
	}
	classes := map[string]agenttypes.ToolClassification{}
	for _, detail := range config.ToolDetails {
		classes[aws.ToString(detail.Name)] = detail.ToolClassification
	}
	assert.Len(t, classes, len(config.Tools), "a tool is in both lists")
	for _, tool := range config.Tools {
		assert.Contains(t, classes, tool)
	}
	for _, tool := range []string{
		"toggle-flag", "create-flag", "update-rollout", "archive-flag", "create-experiment",
		"start-experiment-iteration", "stop-experiment-iteration", "start-guarded-rollout", "stop-guarded-rollout",
		"update-targeting-rules", "update-individual-targets", "create-approval-request",
	} {
		assert.Equal(t, agenttypes.ToolClassificationMutative, classes[tool], tool)
	}
	for _, tool := range []string{"get-flag", "list-experiments", "get-experiment-results", "query-change-history"} {
		assert.Equal(t, agenttypes.ToolClassificationReadOnly, classes[tool], tool)
	}
}
