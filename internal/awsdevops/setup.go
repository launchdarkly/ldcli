package awsdevops

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/devopsagent"
	"github.com/aws/aws-sdk-go-v2/service/devopsagent/document"
	agenttypes "github.com/aws/aws-sdk-go-v2/service/devopsagent/types"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	iamtypes "github.com/aws/aws-sdk-go-v2/service/iam/types"
)

const (
	AgentSpaceRoleName  = "DevOpsAgentRole-AgentSpace"
	OperatorAppRoleName = "DevOpsAgentRole-WebappAdmin"

	AgentSpacePolicyARN  = "arn:aws:iam::aws:policy/AIDevOpsAgentAccessPolicy"
	OperatorAppPolicyARN = "arn:aws:iam::aws:policy/AIDevOpsOperatorAppAccessPolicy"

	ServiceLinkedRolePolicyName = "AllowCreateServiceLinkedRoles"

	// awsServiceID is the well-known service identifier for the agent's own
	// AWS account association.
	awsServiceID = "aws"

	KiroPortalURL = "https://kiro.dev"

	DefaultLDBaseURI = "https://app.launchdarkly.com"

	roleAssumableTimeout     = 2 * time.Minute
	roleAssumableFirstWait   = time.Second
	roleAssumableMaxInterval = 10 * time.Second

	MCPServerName     = "LaunchDarkly"
	MCPServerEndpoint = "https://mcp.launchdarkly.com/mcp/launchdarkly"

	releaseReadinessCapability        = "RELEASE_READINESS_REVIEW"
	releaseReadinessTestingCapability = "RELEASE_READINESS_REVIEW_AUTOMATED_TESTING"

	skillAssetType       = "skill"
	customAgentAssetType = "custom_agent"
	memoryStoreAssetType = "memory_store"
)

// DefaultMCPReadOnlyTools and DefaultMCPMutativeTools are the LaunchDarkly MCP
// tools enabled when the caller does not choose their own. The agent invokes
// read-only tools without asking for approval, so anything that changes flag
// state has to be classified explicitly.
var (
	DefaultMCPReadOnlyTools = []string{"list-projects", "list-flags", "get-flag"}
	DefaultMCPMutativeTools = []string{"toggle-flag"}
)

// SetupOptions describes what to provision. Zero values mean "skip that step".
type SetupOptions struct {
	AgentSpaceID          string
	AgentSpaceName        string
	AgentSpaceDescription string
	NewAgentSpace         bool

	AuthFlow        string
	IdcInstanceARN  string
	IssuerURL       string
	IdpClientID     string
	IdpClientSecret string
	SkipOperatorApp bool

	LDBaseURI        string
	LDAccessToken    string
	SkipMCPServer    bool
	ReplaceMCPToken  bool
	MCPServiceID     string
	MCPReadOnlyTools []string
	MCPMutativeTools []string

	SkillName        string
	SkillBody        string
	CustomAgentName  string
	CustomAgentTools []string
	Schedule         string

	GitHubServiceID      string
	GitHubOwner          string
	GitHubOwnerType      string
	GitHubRepo           string
	GitHubRepoID         string
	GitHubTargetBranches []string

	// Logf, when set, reports progress as each step completes.
	Logf func(format string, args ...any)
}

// SetupResult holds the identifiers of everything the setup touched. Callers
// need these to tear the environment back down.
type SetupResult struct {
	AccountID            string       `json:"accountId"`
	Region               string       `json:"region"`
	AgentSpaceID         string       `json:"agentSpaceId,omitempty"`
	AgentSpaceRoleARN    string       `json:"agentSpaceRoleArn,omitempty"`
	OperatorAppRoleARN   string       `json:"operatorAppRoleArn,omitempty"`
	AWSAssociationID     string       `json:"awsAssociationId,omitempty"`
	OperatorAppURL       string       `json:"operatorAppUrl,omitempty"`
	MCPServiceID         string       `json:"mcpServiceId,omitempty"`
	MCPAssociationID     string       `json:"mcpAssociationId,omitempty"`
	MCPAuthorizationURL  string       `json:"mcpAuthorizationUrl,omitempty"`
	GitHubServiceID      string       `json:"githubServiceId,omitempty"`
	GitHubAssociationID  string       `json:"githubAssociationId,omitempty"`
	SkillAssetID         string       `json:"skillAssetId,omitempty"`
	CustomAgentAssetID   string       `json:"customAgentAssetId,omitempty"`
	TriggerID            string       `json:"triggerId,omitempty"`
	RemainingManualSteps []ManualStep `json:"remainingManualSteps,omitempty"`
}

// ManualStep pairs a step AWS only exposes in a browser with the page to open.
type ManualStep struct {
	Kind        ManualStepKind `json:"kind"`
	Description string         `json:"description"`
	URL         string         `json:"url"`
}

type ManualStepKind string

const (
	ManualStepOAuthConsent ManualStepKind = "oauthConsent"
	ManualStepMCPServer    ManualStepKind = "mcpServer"
	ManualStepGitHubApp    ManualStepKind = "githubApp"
	ManualStepKiroAPIKey   ManualStepKind = "kiroApiKey"
)

// Setup provisions the IAM roles, agent space, service associations and assets
// the AWS DevOps Agent needs to manage LaunchDarkly flags.
func Setup(ctx context.Context, clients Clients, opts SetupOptions) (SetupResult, error) {
	logf := opts.Logf
	if logf == nil {
		logf = func(string, ...any) {}
	}

	accountID, err := clients.AccountID(ctx)
	if err != nil {
		return SetupResult{}, err
	}

	result := SetupResult{AccountID: accountID, Region: clients.Region}

	result.AgentSpaceRoleARN, err = ensureAgentSpaceRole(ctx, clients.IAM, accountID, clients.Region)
	if err != nil {
		return result, err
	}
	logf("Agent space role ready: %s", result.AgentSpaceRoleARN)

	if !opts.SkipOperatorApp {
		result.OperatorAppRoleARN, err = ensureOperatorAppRole(ctx, clients.IAM, accountID, clients.Region)
		if err != nil {
			return result, err
		}
		logf("Operator app role ready: %s", result.OperatorAppRoleARN)
	}

	result.AgentSpaceID = opts.AgentSpaceID
	if result.AgentSpaceID == "" && !opts.NewAgentSpace {
		existing, err := FindAgentSpace(ctx, clients, opts.AgentSpaceName)
		if err != nil {
			return result, err
		}
		if existing != "" {
			result.AgentSpaceID = existing
			logf("Reusing agent space %s (%s); pass --new-agent-space to create another", opts.AgentSpaceName, existing)
		}
	}
	if result.AgentSpaceID == "" {
		space, err := clients.Agent.CreateAgentSpace(ctx, &devopsagent.CreateAgentSpaceInput{
			Name:        aws.String(opts.AgentSpaceName),
			Description: aws.String(opts.AgentSpaceDescription),
		})
		if err != nil {
			return result, fmt.Errorf("unable to create the agent space: %w", err)
		}
		result.AgentSpaceID = aws.ToString(space.AgentSpace.AgentSpaceId)
		logf("Created agent space %s (%s)", opts.AgentSpaceName, result.AgentSpaceID)
	}

	result.AWSAssociationID, err = FindAssociation(ctx, clients, result.AgentSpaceID, awsServiceID)
	if err != nil {
		return result, err
	}
	if result.AWSAssociationID != "" {
		logf("Account %s is already associated with the agent space", accountID)
	} else {
		awsAssociation, err := associateAWSAccount(ctx, clients, accountID, result, logf)
		if err != nil {
			return result, fmt.Errorf("unable to associate the AWS account with the agent space: %w", err)
		}
		result.AWSAssociationID = aws.ToString(awsAssociation.Association.AssociationId)
		logf("Associated account %s with the agent space", accountID)
	}

	if !opts.SkipOperatorApp {
		operatorApp, err := clients.Agent.EnableOperatorApp(ctx, operatorAppInput(result.AgentSpaceID, result.OperatorAppRoleARN, opts))
		if err != nil {
			return result, fmt.Errorf("unable to enable the operator app: %w", err)
		}
		result.OperatorAppURL = aws.ToString(operatorApp.OperatorAppUrl)
		logf("Operator app available at %s", result.OperatorAppURL)
	}

	if !opts.SkipMCPServer && (opts.LDAccessToken != "" || opts.MCPServiceID != "") {
		if err := registerMCPServer(ctx, clients, opts, &result, logf); err != nil {
			return result, err
		}
	}

	result.GitHubServiceID = opts.GitHubServiceID
	if result.GitHubServiceID == "" {
		result.GitHubServiceID, err = FindGitHubService(ctx, clients)
		if err != nil {
			return result, err
		}
	}
	if result.GitHubServiceID != "" {
		logf("GitHub is already registered (service %s)", result.GitHubServiceID)
	}
	if result.GitHubServiceID != "" && opts.GitHubRepo != "" {
		opts.GitHubServiceID = result.GitHubServiceID
		result.GitHubAssociationID, err = AssociateGitHub(ctx, clients, result.AgentSpaceID, opts)
		if err != nil {
			return result, err
		}
		logf("Associated %s/%s with release readiness review enabled", opts.GitHubOwner, opts.GitHubRepo)
	}

	if opts.SkillBody != "" {
		skill, err := clients.Agent.CreateAsset(ctx, &devopsagent.CreateAssetInput{
			AgentSpaceId: aws.String(result.AgentSpaceID),
			AssetType:    aws.String(skillAssetType),
			Metadata: document.NewLazyDocument(map[string]any{
				"name":        opts.SkillName,
				"agent_types": []string{"GENERIC"},
			}),
			Content: &agenttypes.AssetContentMemberFile{
				Value: agenttypes.AssetFileContent{
					Path: aws.String("SKILL.md"),
					Body: &agenttypes.AssetFileBodyMemberText{Value: opts.SkillBody},
				},
			},
		})
		if err != nil {
			return result, fmt.Errorf("unable to create the skill asset: %w", err)
		}
		result.SkillAssetID = aws.ToString(skill.Asset.AssetId)
		logf("Created skill %s (%s)", opts.SkillName, result.SkillAssetID)
	}

	if opts.CustomAgentName != "" {
		metadata := map[string]any{"name": opts.CustomAgentName}
		if result.SkillAssetID != "" {
			metadata["skills"] = []string{result.SkillAssetID}
		}
		if len(opts.CustomAgentTools) > 0 {
			metadata["tools"] = opts.CustomAgentTools
		}
		agent, err := clients.Agent.CreateAsset(ctx, &devopsagent.CreateAssetInput{
			AgentSpaceId: aws.String(result.AgentSpaceID),
			AssetType:    aws.String(customAgentAssetType),
			Metadata:     document.NewLazyDocument(metadata),
			Content: &agenttypes.AssetContentMemberFile{
				Value: agenttypes.AssetFileContent{
					Path: aws.String("AGENT.md"),
					Body: &agenttypes.AssetFileBodyMemberText{
						Value: fmt.Sprintf("# %s\n", opts.CustomAgentName),
					},
				},
			},
		})
		if err != nil {
			return result, fmt.Errorf("unable to create the custom agent asset: %w", err)
		}
		result.CustomAgentAssetID = aws.ToString(agent.Asset.AssetId)
		logf("Created custom agent %s (%s)", opts.CustomAgentName, result.CustomAgentAssetID)
	}

	if opts.Schedule != "" {
		if result.CustomAgentAssetID == "" {
			return result, errors.New("--schedule requires --custom-agent-name so the trigger has an agent to run")
		}
		trigger, err := clients.Agent.CreateTrigger(ctx, &devopsagent.CreateTriggerInput{
			AgentSpaceId: aws.String(result.AgentSpaceID),
			Type:         aws.String("TIME_BASED"),
			Status:       aws.String("Active"),
			Condition: &agenttypes.TriggerConditionMemberSchedule{
				Value: agenttypes.ScheduleCondition{Expression: aws.String(opts.Schedule)},
			},
			Action: document.NewLazyDocument(map[string]any{
				"actionType": "create:task",
				"task":       map[string]any{"agent": "custom:" + result.CustomAgentAssetID},
			}),
		})
		if err != nil {
			return result, fmt.Errorf("unable to schedule the custom agent: %w", err)
		}
		result.TriggerID = aws.ToString(trigger.Trigger.TriggerId)
		logf("Scheduled %s on %s", opts.CustomAgentName, opts.Schedule)
	}

	result.RemainingManualSteps = remainingManualSteps(clients.Region, opts, result)

	return result, nil
}

// associateAWSAccount retries while AWS still rejects the agent space role:
// a freshly created role takes a while to become assumable.
func associateAWSAccount(
	ctx context.Context,
	clients Clients,
	accountID string,
	result SetupResult,
	logf func(string, ...any),
) (*devopsagent.AssociateServiceOutput, error) {
	input := &devopsagent.AssociateServiceInput{
		AgentSpaceId: aws.String(result.AgentSpaceID),
		ServiceId:    aws.String(awsServiceID),
		Configuration: &agenttypes.ServiceConfigurationMemberAws{
			Value: agenttypes.AWSConfiguration{
				AccountId:        aws.String(accountID),
				AccountType:      agenttypes.MonitorAccountTypeMonitor,
				AssumableRoleArn: aws.String(result.AgentSpaceRoleARN),
			},
		},
	}

	deadline := time.Now().Add(roleAssumableTimeout)
	wait := roleAssumableFirstWait
	for first := true; ; first = false {
		association, err := clients.Agent.AssociateService(ctx, input)
		if err == nil || !isRoleNotAssumableYet(err) || time.Now().After(deadline) {
			return association, err
		}
		if first {
			logf("Waiting for %s to become assumable", result.AgentSpaceRoleARN)
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(wait):
		}
		if wait < roleAssumableMaxInterval {
			wait *= 2
		}
	}
}

// isRoleNotAssumableYet matches how AWS reports a role IAM has not finished
// propagating, which reads as a trust policy problem.
func isRoleNotAssumableYet(err error) bool {
	return strings.Contains(err.Error(), "Invalid STS role configuration")
}

// RegisterMCPServer registers and associates the LaunchDarkly MCP server on
// its own, for a token collected after Setup has run.
func RegisterMCPServer(ctx context.Context, clients Clients, opts SetupOptions, result *SetupResult) error {
	logf := opts.Logf
	if logf == nil {
		logf = func(string, ...any) {}
	}

	return registerMCPServer(ctx, clients, opts, result, logf)
}

func registerMCPServer(
	ctx context.Context,
	clients Clients,
	opts SetupOptions,
	result *SetupResult,
	logf func(string, ...any),
) error {
	existing := opts.MCPServiceID
	if existing == "" {
		var err error
		existing, err = FindMCPServer(ctx, clients)
		if err != nil {
			return err
		}
	}
	replacingToken := opts.ReplaceMCPToken && opts.LDAccessToken != ""
	if existing != "" && !replacingToken {
		result.MCPServiceID = existing
		logf(
			"Reusing the %s MCP server already registered on this account (%s); it keeps the access "+
				"token it was registered with, so pass --access-token --replace-mcp-token to change it",
			MCPServerName,
			existing,
		)

		return associateMCPServer(ctx, clients, opts, result, logf)
	}
	if existing != "" {
		if _, err := clients.Agent.DeregisterService(ctx, &devopsagent.DeregisterServiceInput{
			ServiceId: aws.String(existing),
		}); err != nil {
			return fmt.Errorf("unable to deregister the existing LaunchDarkly MCP server %s: %w", existing, err)
		}
		logf("Deregistered the %s MCP server (%s) so it can be re-registered with the new access token",
			MCPServerName, existing)
	}

	registration, err := clients.Agent.RegisterService(ctx, &devopsagent.RegisterServiceInput{
		Service: agenttypes.PostRegisterServiceSupportedServiceMcpServer,
		ServiceDetails: &agenttypes.ServiceDetailsMemberMcpserver{
			Value: agenttypes.MCPServerDetails{
				Name:        aws.String(MCPServerName),
				Endpoint:    aws.String(MCPServerEndpoint),
				Description: aws.String("LaunchDarkly feature flag management MCP server"),
				AuthorizationConfig: &agenttypes.MCPServerAuthorizationConfigMemberBearerToken{
					Value: agenttypes.MCPServerBearerTokenConfig{
						TokenName:  aws.String("launchdarkly-api-token"),
						TokenValue: aws.String(opts.LDAccessToken),
					},
				},
			},
		},
	})
	if err != nil {
		if !strings.Contains(err.Error(), "already exists") {
			return fmt.Errorf("unable to register the LaunchDarkly MCP server: %w", err)
		}

		return fmt.Errorf(
			"an MCP server named %s already exists on this account but is not visible to ListServices, "+
				"so it cannot be associated automatically: associate it with agent space %s in the console, "+
				"or re-run with --skip-mcp-server",
			MCPServerName,
			result.AgentSpaceID,
		)
	}

	// RegisterService returns a service ID or an additional step, never both.
	if oauth, ok := registration.AdditionalStep.(*agenttypes.AdditionalServiceRegistrationStepMemberOauth); ok {
		result.MCPAuthorizationURL = aws.ToString(oauth.Value.AuthorizationUrl)
		return nil
	}
	result.MCPServiceID = aws.ToString(registration.ServiceId)
	logf(
		"Registered the %s MCP server (%s) against the LaunchDarkly account the access token belongs to",
		MCPServerName,
		result.MCPServiceID,
	)

	return associateMCPServer(ctx, clients, opts, result, logf)
}

func associateMCPServer(
	ctx context.Context,
	clients Clients,
	opts SetupOptions,
	result *SetupResult,
	logf func(string, ...any),
) error {
	existing, err := FindAssociation(ctx, clients, result.AgentSpaceID, result.MCPServiceID)
	if err != nil {
		return err
	}
	if existing != "" {
		result.MCPAssociationID = existing
		logf("The %s MCP server is already associated with the agent space", MCPServerName)

		return nil
	}

	association, err := clients.Agent.AssociateService(ctx, &devopsagent.AssociateServiceInput{
		AgentSpaceId: aws.String(result.AgentSpaceID),
		ServiceId:    aws.String(result.MCPServiceID),
		Configuration: &agenttypes.ServiceConfigurationMemberMcpserver{
			Value: mcpServerConfiguration(opts),
		},
	})
	if err != nil {
		return fmt.Errorf("unable to associate the LaunchDarkly MCP server with the agent space: %w", err)
	}
	result.MCPAssociationID = aws.ToString(association.Association.AssociationId)
	logf("Associated the %s MCP server with the agent space", MCPServerName)

	return nil
}

// mcpServerConfiguration builds the tool allowlist. AWS has no "allow all
// tools" option, and an unclassified tool is treated as read-only, which the
// agent may invoke without approval.
func mcpServerConfiguration(opts SetupOptions) agenttypes.MCPServerConfiguration {
	readOnly := opts.MCPReadOnlyTools
	if readOnly == nil {
		readOnly = DefaultMCPReadOnlyTools
	}
	mutative := opts.MCPMutativeTools
	if mutative == nil {
		mutative = DefaultMCPMutativeTools
	}

	config := agenttypes.MCPServerConfiguration{
		Tools:       make([]string, 0, len(readOnly)+len(mutative)),
		ToolDetails: make([]agenttypes.MCPToolDetail, 0, len(readOnly)+len(mutative)),
	}
	for _, tool := range readOnly {
		config.Tools = append(config.Tools, tool)
		config.ToolDetails = append(config.ToolDetails, agenttypes.MCPToolDetail{
			Name:               aws.String(tool),
			ToolClassification: agenttypes.ToolClassificationReadOnly,
		})
	}
	for _, tool := range mutative {
		config.Tools = append(config.Tools, tool)
		config.ToolDetails = append(config.ToolDetails, agenttypes.MCPToolDetail{
			Name:               aws.String(tool),
			ToolClassification: agenttypes.ToolClassificationMutative,
		})
	}

	return config
}

func operatorAppInput(agentSpaceID, roleARN string, opts SetupOptions) *devopsagent.EnableOperatorAppInput {
	input := &devopsagent.EnableOperatorAppInput{
		AgentSpaceId:       aws.String(agentSpaceID),
		AuthFlow:           agenttypes.AuthFlow(opts.AuthFlow),
		OperatorAppRoleArn: aws.String(roleARN),
	}
	switch agenttypes.AuthFlow(opts.AuthFlow) {
	case agenttypes.AuthFlowIdc:
		input.IdcInstanceArn = aws.String(opts.IdcInstanceARN)
	case agenttypes.AuthFlowIdp:
		input.IssuerUrl = aws.String(opts.IssuerURL)
		input.IdpClientId = aws.String(opts.IdpClientID)
		input.IdpClientSecret = aws.String(opts.IdpClientSecret)
	}

	return input
}

// AssociateGitHub connects a repository to an agent space once GitHub itself
// has been registered in the console.
func AssociateGitHub(ctx context.Context, clients Clients, agentSpaceID string, opts SetupOptions) (string, error) {
	association, err := clients.Agent.AssociateService(ctx, githubAssociationInput(agentSpaceID, opts))
	if err != nil {
		return "", fmt.Errorf("unable to associate the GitHub repository: %w", err)
	}

	return aws.ToString(association.Association.AssociationId), nil
}

func githubAssociationInput(agentSpaceID string, opts SetupOptions) *devopsagent.AssociateServiceInput {
	input := &devopsagent.AssociateServiceInput{
		AgentSpaceId: aws.String(agentSpaceID),
		ServiceId:    aws.String(opts.GitHubServiceID),
		Configuration: &agenttypes.ServiceConfigurationMemberGithub{
			Value: agenttypes.GitHubConfiguration{
				Owner:     aws.String(opts.GitHubOwner),
				OwnerType: agenttypes.GithubRepoOwnerType(strings.ToLower(opts.GitHubOwnerType)),
				RepoId:    aws.String(opts.GitHubRepoID),
				RepoName:  aws.String(opts.GitHubRepo),
			},
		},
		Capabilities: map[string]agenttypes.CapabilityConfiguration{
			releaseReadinessCapability: {
				Enabled: aws.Bool(true),
				TriggerFilterGroups: []agenttypes.TriggerFilterGroup{
					{
						Events: []agenttypes.TriggerEvent{agenttypes.TriggerEventPullRequestReadyForReview},
						TargetBranches: &agenttypes.PatternFilter{
							Patterns: opts.GitHubTargetBranches,
						},
					},
				},
			},
			releaseReadinessTestingCapability: {Enabled: aws.Bool(true)},
		},
	}

	return input
}

func ensureAgentSpaceRole(ctx context.Context, client IAMAPI, accountID, region string) (string, error) {
	trustPolicy := fmt.Sprintf(`{"Version":"2012-10-17","Statement":[{"Effect":"Allow",`+
		`"Principal":{"Service":"aidevops.amazonaws.com"},"Action":"sts:AssumeRole",`+
		`"Condition":{"StringEquals":{"aws:SourceAccount":"%[1]s"},`+
		`"ArnLike":{"aws:SourceArn":"arn:aws:aidevops:%[2]s:%[1]s:agentspace/*"}}}]}`, accountID, region)

	arn, err := ensureRole(ctx, client, AgentSpaceRoleName, trustPolicy, AgentSpacePolicyARN)
	if err != nil {
		return "", err
	}

	// Topology discovery creates the Resource Explorer service-linked role on
	// first use, which the managed policy does not allow.
	inlinePolicy := fmt.Sprintf(`{"Version":"2012-10-17","Statement":[{"Sid":"AllowCreateServiceLinkedRoles",`+
		`"Effect":"Allow","Action":["iam:CreateServiceLinkedRole"],`+
		`"Resource":["arn:aws:iam::%s:role/aws-service-role/resource-explorer-2.amazonaws.com/AWSServiceRoleForResourceExplorer"]}]}`, accountID)
	if _, err := client.PutRolePolicy(ctx, &iam.PutRolePolicyInput{
		RoleName:       aws.String(AgentSpaceRoleName),
		PolicyName:     aws.String(ServiceLinkedRolePolicyName),
		PolicyDocument: aws.String(inlinePolicy),
	}); err != nil {
		return "", fmt.Errorf("unable to add the %s policy to %s: %w", ServiceLinkedRolePolicyName, AgentSpaceRoleName, err)
	}

	return arn, nil
}

func ensureOperatorAppRole(ctx context.Context, client IAMAPI, accountID, region string) (string, error) {
	// sts:TagSession is required because the managed policy scopes web app
	// access per space with an aws:PrincipalTag/AgentSpaceId condition.
	trustPolicy := fmt.Sprintf(`{"Version":"2012-10-17","Statement":[{"Effect":"Allow",`+
		`"Principal":{"Service":"aidevops.amazonaws.com"},"Action":["sts:AssumeRole","sts:TagSession"],`+
		`"Condition":{"StringEquals":{"aws:SourceAccount":"%[1]s"},`+
		`"ArnLike":{"aws:SourceArn":"arn:aws:aidevops:%[2]s:%[1]s:agentspace/*"}}}]}`, accountID, region)

	return ensureRole(ctx, client, OperatorAppRoleName, trustPolicy, OperatorAppPolicyARN)
}

func ensureRole(ctx context.Context, client IAMAPI, name, trustPolicy, policyARN string) (string, error) {
	var arn string
	created, err := client.CreateRole(ctx, &iam.CreateRoleInput{
		RoleName:                 aws.String(name),
		AssumeRolePolicyDocument: aws.String(trustPolicy),
	})
	switch {
	case err == nil:
		arn = aws.ToString(created.Role.Arn)
	case isAlreadyExists(err):
		existing, getErr := client.GetRole(ctx, &iam.GetRoleInput{RoleName: aws.String(name)})
		if getErr != nil {
			return "", fmt.Errorf("unable to read the existing %s role: %w", name, getErr)
		}
		arn = aws.ToString(existing.Role.Arn)
	default:
		return "", fmt.Errorf("unable to create the %s role: %w", name, err)
	}

	if _, err := client.AttachRolePolicy(ctx, &iam.AttachRolePolicyInput{
		RoleName:  aws.String(name),
		PolicyArn: aws.String(policyARN),
	}); err != nil {
		return "", fmt.Errorf("unable to attach %s to %s: %w", policyARN, name, err)
	}

	return arn, nil
}

func isAlreadyExists(err error) bool {
	var alreadyExists *iamtypes.EntityAlreadyExistsException

	return errors.As(err, &alreadyExists)
}

func isNoSuchEntity(err error) bool {
	var noSuchEntity *iamtypes.NoSuchEntityException

	return errors.As(err, &noSuchEntity)
}

func remainingManualSteps(region string, opts SetupOptions, result SetupResult) []ManualStep {
	var steps []ManualStep
	if result.MCPAuthorizationURL != "" {
		steps = append(steps, ManualStep{
			Kind:        ManualStepOAuthConsent,
			Description: "Approve the LaunchDarkly MCP server OAuth consent screen, then re-run setup",
			URL:         result.MCPAuthorizationURL,
		})
	}
	if !opts.SkipMCPServer && opts.LDAccessToken == "" && result.MCPServiceID == "" {
		steps = append(steps, ManualStep{
			Kind: ManualStepMCPServer,
			Description: fmt.Sprintf(
				"Create a LaunchDarkly service token to connect the MCP server (%s)",
				MCPServerEndpoint,
			),
			URL: AccessTokenURL(opts.LDBaseURI),
		})
	}
	if result.GitHubServiceID == "" {
		steps = append(steps, ManualStep{
			Kind:        ManualStepGitHubApp,
			Description: "Register GitHub and install the GitHub App (a browser consent screen), then re-run setup with --github-owner, --github-repo and --github-repo-id to connect a repository",
			URL:         GitHubRegistrationURL(region),
		})
	}
	steps = append(steps, ManualStep{
		Kind:        ManualStepKiroAPIKey,
		Description: "Create a Kiro API key if you want the agent to use Kiro — keys can only be created in the browser",
		URL:         KiroPortalURL,
	})

	return steps
}

// ConsoleURL is the AWS DevOps Agent console for a region.
func ConsoleURL(region string) string {
	return fmt.Sprintf("https://%s.console.aws.amazon.com/aidevops/home?region=%[1]s", region)
}

// GitHubRegistrationURL is the console page that registers GitHub with the
// account, which AWS only exposes in a browser.
func GitHubRegistrationURL(region string) string {
	return ConsoleURL(region) + "#/services/register/github"
}

// AccessTokenURL is the LaunchDarkly page where a service token is created.
func AccessTokenURL(baseURI string) string {
	if baseURI == "" {
		baseURI = DefaultLDBaseURI
	}

	return strings.TrimSuffix(baseURI, "/") + "/settings/authorization"
}

// OperatorAppURL is the browser entry point for an agent space's operator app.
func OperatorAppURL(agentSpaceID string) string {
	return fmt.Sprintf("https://%s.aidevops.global.app.aws", agentSpaceID)
}
