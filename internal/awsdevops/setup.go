package awsdevops

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/devopsagent"
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

	// ManagedTagKey marks the roles this CLI created, so a re-run never
	// rewrites a role of the same name that belongs to the customer.
	ManagedTagKey   = "ldcli:managed"
	ManagedTagValue = "aws-devops-agent"

	// awsServiceID is the well-known service identifier for the agent's own
	// AWS account association.
	awsServiceID = "aws"

	DefaultLDBaseURI = "https://app.launchdarkly.com"

	roleAssumableTimeout     = 2 * time.Minute
	roleAssumableFirstWait   = time.Second
	roleAssumableMaxInterval = 10 * time.Second

	MCPServerName     = "LaunchDarkly"
	MCPServerEndpoint = "https://mcp.launchdarkly.com/mcp/launchdarkly"

	agentSpaceDescription = "Managed by the LaunchDarkly CLI"
)

// The agent invokes read-only tools without asking for approval, so anything
// that changes flag state has to be classified explicitly.
var (
	mcpReadOnlyTools = []string{"list-projects", "list-flags", "get-flag"}
	mcpMutativeTools = []string{"toggle-flag"}
)

// SetupOptions describes what to provision.
type SetupOptions struct {
	AgentSpaceID   string
	AgentSpaceName string
	NewAgentSpace  bool

	AuthFlow string
	// AuthFlowSet: the user passed --auth-flow.
	AuthFlowSet     bool
	IdcInstanceARN  string
	IssuerURL       string
	IdpClientID     string
	IdpClientSecret string

	LDBaseURI       string
	LDAccessToken   string
	ReplaceMCPToken bool

	// MCPServiceID, when set, is the registration to reuse or replace instead
	// of the one found by endpoint.
	MCPServiceID string
	// MCPEndpoint overrides the LaunchDarkly MCP endpoint, for an account
	// running the open-source MCP server itself. The endpoint identifies the
	// registration, so a self-hosted one is never confused with ours.
	MCPEndpoint string

	// Logf, when set, reports progress as each step completes.
	Logf func(format string, args ...any)
}

// SetupResult holds the identifiers of everything the setup touched.
type SetupResult struct {
	AccountID          string `json:"accountId"`
	Region             string `json:"region"`
	AgentSpaceID       string `json:"agentSpaceId,omitempty"`
	AgentSpaceRoleARN  string `json:"agentSpaceRoleArn,omitempty"`
	OperatorAppRoleARN string `json:"operatorAppRoleArn,omitempty"`
	AWSAssociationID   string `json:"awsAssociationId,omitempty"`
	OperatorAppURL     string `json:"operatorAppUrl,omitempty"`
	MCPServiceID       string `json:"mcpServiceId,omitempty"`
	MCPAssociationID   string `json:"mcpAssociationId,omitempty"`
}

// RoleAction is what setup does to a role.
type RoleAction int

const (
	RoleCreate RoleAction = iota
	RoleUpdate
	RoleLeave
)

// RoleStep is one planned role change.
type RoleStep struct {
	Name   string
	Action RoleAction
}

func (s RoleStep) describe() string {
	switch s.Action {
	case RoleCreate:
		return "Create role " + s.Name
	case RoleUpdate:
		return fmt.Sprintf("Update the trust policy of role %s", s.Name)
	default:
		return fmt.Sprintf("Leave role %s unchanged because it has no %s tag", s.Name, ManagedTagKey)
	}
}

// SpaceAction is the planned space action.
type SpaceAction int

const (
	SpaceCreate SpaceAction = iota
	SpaceReuse
	SpaceConnect
)

// SpaceStep is the planned agent space action.
type SpaceStep struct {
	Action SpaceAction
	ID     string
	Name   string
}

func (s SpaceStep) describe() string {
	label := fmt.Sprintf("%s (%s)", s.Name, s.ID)
	switch s.Action {
	case SpaceCreate:
		return "Create agent space " + s.Name
	case SpaceConnect:
		return "Connect to agent space " + label
	default:
		return "Reuse agent space " + label
	}
}

// LinkStep is the planned account association.
type LinkStep struct {
	// Exists means the association was there.
	Exists bool
}

func (l LinkStep) describe(accountID string) string {
	if l.Exists {
		return fmt.Sprintf("Keep the AWS account %s association", accountID)
	}

	return fmt.Sprintf("Associate AWS account %s with the agent space", accountID)
}

// AppAction is the planned operator app action.
type AppAction int

const (
	AppEnable AppAction = iota
	AppKeep
	AppChange
)

// AppStep is the planned operator app step.
type AppStep struct {
	Action AppAction
	URL    string
	// Flow is the sign-in at plan time.
	Flow string
}

func (a AppStep) describe(flow string) string {
	switch a.Action {
	case AppKeep:
		return "Keep the operator app and its sign-in"
	case AppChange:
		switch {
		case a.Flow == flow && flow == "iam":
			return "Set the operator app sign-in to IAM again"
		case a.Flow == flow:
			return fmt.Sprintf("Update the operator app %s sign-in settings", flowLabel(flow))
		}

		return "Change the operator app sign-in to " + flowLabel(flow)
	default:
		return fmt.Sprintf("Enable the operator app with %s sign-in", flowLabel(flow))
	}
}

func flowLabel(flow string) string {
	switch flow {
	case "idc":
		return "IdC"
	case "idp":
		return "IdP"
	default:
		return "IAM"
	}
}

// appState reads the app. Flow "" means off. The space must exist. AWS sends
// ResourceNotFoundException for an app that is off and for a space that does
// not exist, so appState cannot tell the two apart.
func appState(ctx context.Context, clients Clients, spaceID string) (AppStep, error) {
	app, err := clients.Agent.GetOperatorApp(ctx, &devopsagent.GetOperatorAppInput{AgentSpaceId: aws.String(spaceID)})
	var missing *agenttypes.ResourceNotFoundException
	switch {
	case errors.As(err, &missing):
		return AppStep{}, nil
	case err != nil:
		return AppStep{}, fmt.Errorf("unable to read the operator app of agent space %s: %w", spaceID, err)
	}
	state := AppStep{URL: aws.ToString(app.OperatorAppUrl)}
	switch {
	case app.Idc != nil:
		state.Flow = "idc"
	case app.Idp != nil:
		state.Flow = "idp"
	case app.Iam != nil:
		state.Flow = "iam"
	}

	return state, nil
}

func planApp(ctx context.Context, clients Clients, opts SetupOptions, space SpaceStep) (AppStep, error) {
	if space.Action == SpaceCreate {
		return AppStep{Action: AppEnable}, nil
	}
	state, err := appState(ctx, clients, space.ID)
	switch {
	case err != nil:
		return AppStep{}, err
	case state.Flow == "":
		return AppStep{Action: AppEnable}, nil
	case opts.AuthFlowSet:
		state.Action = AppChange
	default:
		state.Action = AppKeep
	}

	return state, nil
}

// MCPAction is the planned MCP server action.
type MCPAction int

const (
	MCPRegister MCPAction = iota
	MCPReuse
	MCPReplace
	MCPAsk
	// MCPSkip means that the user gave no token, so the run skips the server.
	MCPSkip
)

// MCPStep is the planned MCP server step.
type MCPStep struct {
	Action    MCPAction
	ServiceID string
	// Endpoint is the URL that the server serves.
	Endpoint string
	// Spaces lists spaces linked to the server.
	Spaces []string
}

// SetupPlan describes what setup will change. It holds only the steps. Text
// describes them, so the printed plan cannot disagree with the steps.
type SetupPlan struct {
	AccountID string
	Region    string
	Space     SpaceStep
	// Link is set only in a full setup plan. A connect plan does not read or
	// change the account association.
	Link      LinkStep
	RoleSteps []RoleStep
	// App is the operator app at plan time. In a connect plan, its Action is
	// AppKeep and its Flow is the current sign-in, which the run reports.
	App AppStep
	// AuthFlow is the sign-in that a full setup sets.
	AuthFlow string
	MCP      MCPStep
}

// PlanText is the plan as lines for a person to read.
type PlanText struct {
	AgentSpace  string
	Association string
	Roles       []string
	OperatorApp string
	MCPServer   string
}

// Text describes each step of the plan.
func (p SetupPlan) Text() PlanText {
	text := PlanText{AgentSpace: p.Space.describe(), MCPServer: p.MCP.describe()}
	if p.Space.Action == SpaceConnect {
		text.Association = "No change"
		text.Roles = []string{"No change"}
		text.OperatorApp = "No change"
		if p.App.Flow == "" {
			text.OperatorApp = "No change. The operator app is off"
		}

		return text
	}
	text.Association = p.Link.describe(p.AccountID)
	for _, step := range p.RoleSteps {
		text.Roles = append(text.Roles, step.describe())
	}
	text.OperatorApp = p.App.describe(p.AuthFlow)

	return text
}

// PlanSetup describes a run without changing AWS.
func PlanSetup(ctx context.Context, clients Clients, opts SetupOptions, provision bool) (SetupPlan, error) {
	accountID, err := clients.AccountID(ctx)
	if err != nil {
		return SetupPlan{}, err
	}
	plan := SetupPlan{AccountID: accountID, Region: clients.Region}

	plan.Space, err = planAgentSpace(ctx, clients, opts, provision)
	if err != nil {
		return plan, err
	}

	plan.MCP, err = planMCPStep(ctx, clients, opts)
	if err != nil {
		return plan, err
	}

	if !provision {
		// Read the app, so the run does not print a URL that does not work.
		plan.App, err = appState(ctx, clients, plan.Space.ID)
		if err != nil {
			return plan, err
		}
		plan.App.Action = AppKeep

		return plan, nil
	}
	plan.AuthFlow = opts.AuthFlow
	if plan.Space.Action != SpaceCreate {
		id, err := FindAssociation(ctx, clients, plan.Space.ID, awsServiceID)
		if err != nil {
			return plan, err
		}
		plan.Link.Exists = id != ""
	}
	for _, name := range []string{AgentSpaceRoleName, OperatorAppRoleName} {
		step, err := planRole(ctx, clients.IAM, name)
		if err != nil {
			return plan, err
		}
		plan.RoleSteps = append(plan.RoleSteps, step)
	}
	plan.App, err = planApp(ctx, clients, opts, plan.Space)
	if err != nil {
		return plan, err
	}

	return plan, nil
}

// planRole says what setup will do to a role.
func planRole(ctx context.Context, client IAMAPI, name string) (RoleStep, error) {
	_, err := client.GetRole(ctx, &iam.GetRoleInput{RoleName: aws.String(name)})
	var missing *iamtypes.NoSuchEntityException
	switch {
	case errors.As(err, &missing):
		return RoleStep{Name: name, Action: RoleCreate}, nil
	case err != nil:
		return RoleStep{}, fmt.Errorf("unable to read the %s role: %w", name, err)
	}

	managed, err := isManagedRole(ctx, client, name)
	if err != nil {
		return RoleStep{}, err
	}
	if !managed {
		return RoleStep{Name: name, Action: RoleLeave}, nil
	}

	return RoleStep{Name: name, Action: RoleUpdate}, nil
}

func planAgentSpace(ctx context.Context, clients Clients, opts SetupOptions, provision bool) (SpaceStep, error) {
	id := opts.AgentSpaceID
	if id == "" {
		seen, err := spacesNamed(ctx, clients, opts.AgentSpaceName)
		switch {
		case err != nil:
			return SpaceStep{}, err
		case len(seen) > 0 && opts.NewAgentSpace:
			// Two spaces with one name make the next run unable to pick.
			return SpaceStep{}, fmt.Errorf(
				"an agent space named %s already exists (%s). Pass --agent-space-name with another name",
				opts.AgentSpaceName, strings.Join(seen, ", "),
			)
		case len(seen) > 1:
			return SpaceStep{}, fmt.Errorf(
				"%d agent spaces are named %s (%s). Pass --agent-space-id",
				len(seen), opts.AgentSpaceName, strings.Join(seen, ", "),
			)
		case len(seen) == 1:
			id = seen[0]
		}
	}
	if id == "" && !provision {
		// A connect run never creates a space.
		return SpaceStep{}, fmt.Errorf("no agent space named %s to connect to. Pass --agent-space-id", opts.AgentSpaceName)
	}
	if id == "" && opts.AgentSpaceName == "" {
		return SpaceStep{}, errors.New("a new agent space needs a name. Pass --agent-space-name")
	}
	if id == "" {
		return SpaceStep{Action: SpaceCreate, Name: opts.AgentSpaceName}, nil
	}

	space, err := clients.Agent.GetAgentSpace(ctx, &devopsagent.GetAgentSpaceInput{AgentSpaceId: aws.String(id)})
	if err != nil {
		return SpaceStep{}, fmt.Errorf("unable to read agent space %s: %w", id, err)
	}
	step := SpaceStep{Action: SpaceReuse, ID: id, Name: aws.ToString(space.AgentSpace.Name)}
	if !provision {
		step.Action = SpaceConnect
	}

	return step, nil
}

func planMCPStep(ctx context.Context, clients Clients, opts SetupOptions) (MCPStep, error) {
	step, err := planMCPAction(ctx, clients, opts)
	step.Endpoint = opts.mcpEndpoint()

	return step, err
}

func planMCPAction(ctx context.Context, clients Clients, opts SetupOptions) (MCPStep, error) {
	existing := opts.MCPServiceID
	if existing != "" {
		if err := checkMCPServiceID(ctx, clients, existing, opts.mcpEndpoint()); err != nil {
			return MCPStep{}, err
		}
	}
	if existing == "" {
		found, err := FindMCPServer(ctx, clients, opts.mcpEndpoint())
		if err != nil {
			return MCPStep{}, err
		}
		existing = found
	}

	switch {
	case existing != "" && opts.ReplaceMCPToken && opts.LDAccessToken != "":
		spaces, err := spacesWith(ctx, clients, existing)
		if err != nil {
			return MCPStep{}, err
		}

		return MCPStep{Action: MCPReplace, ServiceID: existing, Spaces: spaces}, nil
	case existing != "":
		return MCPStep{Action: MCPReuse, ServiceID: existing}, nil
	case opts.LDAccessToken != "":
		return MCPStep{Action: MCPRegister}, nil
	default:
		return MCPStep{Action: MCPAsk}, nil
	}
}

// checkMCPServiceID needs a registered MCP server at the LaunchDarkly
// endpoint. Setup finds a LaunchDarkly MCP server by its endpoint, because the
// name is the choice of the customer. A server at another endpoint is not one,
// so setup does not connect it to the agent space.
func checkMCPServiceID(ctx context.Context, clients Clients, id, endpoint string) error {
	services, err := listServices(ctx, clients)
	if err != nil {
		return err
	}
	for _, service := range services {
		if aws.ToString(service.ServiceId) != id || service.ServiceType != agenttypes.ServiceMcpServer {
			continue
		}
		if served := mcpEndpointOf(service); served != endpoint {
			return fmt.Errorf(
				"MCP server %s serves %s, not the LaunchDarkly MCP server at %s. "+
					"If it is a LaunchDarkly MCP server that you host, pass --mcp-endpoint %s",
				id, served, endpoint, served,
			)
		}

		return nil
	}

	return fmt.Errorf("ID %s names no registered MCP server", id)
}

func (m MCPStep) describe() string {
	switch m.Action {
	case MCPReplace:
		line := fmt.Sprintf("Replace the token of MCP server %s (%s)", MCPServerName, m.ServiceID)
		if len(m.Spaces) > 0 {
			line += ", reconnecting " + strings.Join(m.Spaces, ", ")
		}

		return line
	case MCPReuse:
		return fmt.Sprintf("Reuse MCP server %s (%s)", MCPServerName, m.ServiceID)
	case MCPRegister:
		return fmt.Sprintf("Register MCP server %s at %s", MCPServerName, m.Endpoint)
	case MCPSkip:
		return fmt.Sprintf("Skip MCP server %s, because no token was given", MCPServerName)
	default:
		return fmt.Sprintf("Ask for a token, then register MCP server %s", MCPServerName)
	}
}

// Setup provisions the IAM roles, agent space, service associations and the
// MCP server the AWS DevOps Agent needs to manage LaunchDarkly flags.
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

	result.AgentSpaceRoleARN, err = ensureAgentSpaceRole(ctx, clients.IAM, accountID, logf)
	if err != nil {
		return result, err
	}
	logf("Agent space role ready: %s", result.AgentSpaceRoleARN)

	result.OperatorAppRoleARN, err = ensureOperatorAppRole(ctx, clients.IAM, accountID, logf)
	if err != nil {
		return result, err
	}
	logf("Operator app role ready: %s", result.OperatorAppRoleARN)

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
			Description: aws.String(agentSpaceDescription),
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

	operatorApp, err := clients.Agent.EnableOperatorApp(ctx, operatorAppInput(result.AgentSpaceID, result.OperatorAppRoleARN, opts))
	if err != nil {
		return result, fmt.Errorf("unable to enable the operator app: %w", err)
	}
	result.OperatorAppURL = aws.ToString(operatorApp.OperatorAppUrl)
	logf("Operator app available at %s", result.OperatorAppURL)

	if err := registerMCPServer(ctx, clients, opts, &result, logf); err != nil {
		return result, err
	}

	return result, nil
}

// mcpEndpoint is the endpoint the MCP server is registered at and found by.
func (o SetupOptions) mcpEndpoint() string {
	if o.MCPEndpoint != "" {
		return o.MCPEndpoint
	}

	return MCPServerEndpoint
}

// registrationFailed names the agent spaces a failed token replacement left
// without an MCP server: the old registration is already gone by then, and its
// token cannot be read back, so only a re-run with a working token restores them.
func registrationFailed(err error, detached []string) error {
	if len(detached) == 0 {
		return fmt.Errorf("unable to register the LaunchDarkly MCP server: %w", err)
	}

	return fmt.Errorf(
		"unable to register the LaunchDarkly MCP server: %w. The previous registration was already "+
			"removed, so agent spaces %s have no LaunchDarkly MCP server: re-run setup with "+
			"--access-token <token> to restore them",
		err,
		strings.Join(detached, ", "),
	)
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

// registerWithRetry retries while AWS reports the MCP endpoint as unreachable,
// which it does intermittently for an endpoint that is in fact serving.
func registerWithRetry(
	ctx context.Context,
	clients Clients,
	input *devopsagent.RegisterServiceInput,
	logf func(string, ...any),
	endpoint string,
) (*devopsagent.RegisterServiceOutput, error) {
	deadline := time.Now().Add(roleAssumableTimeout)
	wait := roleAssumableFirstWait
	for first := true; ; first = false {
		registration, err := clients.Agent.RegisterService(ctx, input)
		if err == nil || !isEndpointUnreachable(err) || time.Now().After(deadline) {
			return registration, err
		}
		if first {
			logf("AWS could not reach %s, retrying", endpoint)
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

func isEndpointUnreachable(err error) bool {
	return strings.Contains(err.Error(), "is not reachable")
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
	endpoint := opts.mcpEndpoint()
	existing := opts.MCPServiceID
	if existing == "" {
		found, err := FindMCPServer(ctx, clients, endpoint)
		if err != nil {
			return err
		}
		existing = found
	}
	if existing == "" && opts.LDAccessToken == "" {
		// Nothing to reuse and no token to register with; the caller collects
		// one and calls RegisterMCPServer afterwards.
		return nil
	}
	if existing != "" && (!opts.ReplaceMCPToken || opts.LDAccessToken == "") {
		result.MCPServiceID = existing
		logf(
			"Reusing the %s MCP server already registered on this account (%s); it keeps the access "+
				"token it was registered with, so pass --access-token --replace-mcp-token to change it",
			MCPServerName,
			existing,
		)

		return associateMCPServer(ctx, clients, result, logf)
	}
	var detached []string
	if existing != "" {
		// AWS stores the token write-only, so the old registration cannot be
		// restored once it is deregistered. Reject a token LaunchDarkly will
		// not accept while the working registration is still in place.
		if err := ValidateAccessToken(ctx, opts.LDBaseURI, opts.LDAccessToken); err != nil {
			return err
		}

		var err error
		detached, err = disassociateEverywhere(ctx, clients, existing, logf)
		if err != nil {
			return err
		}
		if _, err := clients.Agent.DeregisterService(ctx, &devopsagent.DeregisterServiceInput{
			ServiceId: aws.String(existing),
		}); err != nil {
			return fmt.Errorf("unable to deregister the existing LaunchDarkly MCP server %s: %w", existing, err)
		}
		logf("Deregistered the %s MCP server (%s) so it can be re-registered with the new access token",
			MCPServerName, existing)
	}

	input := &devopsagent.RegisterServiceInput{
		Service: agenttypes.PostRegisterServiceSupportedServiceMcpServer,
		ServiceDetails: &agenttypes.ServiceDetailsMemberMcpserver{
			Value: agenttypes.MCPServerDetails{
				Name:        aws.String(MCPServerName),
				Endpoint:    aws.String(endpoint),
				Description: aws.String("LaunchDarkly feature flag management MCP server"),
				AuthorizationConfig: &agenttypes.MCPServerAuthorizationConfigMemberBearerToken{
					Value: agenttypes.MCPServerBearerTokenConfig{
						TokenName:  aws.String("launchdarkly-api-token"),
						TokenValue: aws.String(opts.LDAccessToken),
					},
				},
			},
		},
	}

	registration, err := registerWithRetry(ctx, clients, input, logf, endpoint)
	if err != nil {
		if strings.Contains(err.Error(), "already exists") {
			return fmt.Errorf(
				"an MCP server named %s is already registered on this account at another endpoint, "+
					"so %s cannot be registered: pass --mcp-service-id <id> to use that registration, "+
					"or rename it in the console",
				MCPServerName,
				endpoint,
			)
		}

		return registrationFailed(err, detached)
	}

	result.MCPServiceID = aws.ToString(registration.ServiceId)
	logf(
		"Registered the %s MCP server (%s) against the LaunchDarkly account the access token belongs to",
		MCPServerName,
		result.MCPServiceID,
	)

	if err := associateMCPServer(ctx, clients, result, logf); err != nil {
		return err
	}

	// The deregistered service was account-level, so reconnect the other agent
	// spaces that were using it before the token was replaced.
	for _, agentSpaceID := range detached {
		if agentSpaceID == result.AgentSpaceID {
			continue
		}
		if _, err := associateMCPServerWith(ctx, clients, agentSpaceID, result.MCPServiceID); err != nil {
			return err
		}
		logf("Re-associated the %s MCP server with agent space %s", MCPServerName, agentSpaceID)
	}

	return nil
}

// spacesWith lists spaces associated with serviceID.
func spacesWith(ctx context.Context, clients Clients, serviceID string) ([]string, error) {
	spaces, err := listAgentSpaces(ctx, clients)
	if err != nil {
		return nil, err
	}
	var ids []string
	for _, space := range spaces {
		id := aws.ToString(space.AgentSpaceId)
		link, err := FindAssociation(ctx, clients, id, serviceID)
		if err != nil {
			return nil, err
		}
		if link != "" {
			ids = append(ids, id)
		}
	}
	slices.Sort(ids)

	return ids, nil
}

// disassociateEverywhere removes a service from every agent space that uses
// it, which AWS requires before the service can be deregistered. It returns
// the agent spaces it was removed from, so the re-registered service can be
// reconnected to them.
func disassociateEverywhere(
	ctx context.Context,
	clients Clients,
	serviceID string,
	logf func(string, ...any),
) ([]string, error) {
	spaces, err := listAgentSpaces(ctx, clients)
	if err != nil {
		return nil, err
	}

	var detached []string
	for _, space := range spaces {
		agentSpaceID := aws.ToString(space.AgentSpaceId)
		associationID, err := FindAssociation(ctx, clients, agentSpaceID, serviceID)
		if err != nil {
			return nil, err
		}
		if associationID == "" {
			continue
		}
		if _, err := clients.Agent.DisassociateService(ctx, &devopsagent.DisassociateServiceInput{
			AgentSpaceId:  aws.String(agentSpaceID),
			AssociationId: aws.String(associationID),
		}); err != nil {
			return nil, fmt.Errorf("unable to disassociate service %s from agent space %s: %w", serviceID, agentSpaceID, err)
		}
		logf("Disassociated service %s from agent space %s", serviceID, agentSpaceID)
		detached = append(detached, agentSpaceID)
	}

	return detached, nil
}

func associateMCPServer(
	ctx context.Context,
	clients Clients,
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

	associationID, err := associateMCPServerWith(ctx, clients, result.AgentSpaceID, result.MCPServiceID)
	if err != nil {
		return err
	}
	result.MCPAssociationID = associationID
	logf("Associated the %s MCP server with the agent space", MCPServerName)

	return nil
}

func associateMCPServerWith(ctx context.Context, clients Clients, agentSpaceID, serviceID string) (string, error) {
	association, err := clients.Agent.AssociateService(ctx, &devopsagent.AssociateServiceInput{
		AgentSpaceId: aws.String(agentSpaceID),
		ServiceId:    aws.String(serviceID),
		Configuration: &agenttypes.ServiceConfigurationMemberMcpserver{
			Value: mcpServerConfiguration(),
		},
	})
	if err != nil {
		return "", fmt.Errorf(
			"unable to associate the LaunchDarkly MCP server with agent space %s: %w",
			agentSpaceID,
			err,
		)
	}

	return aws.ToString(association.Association.AssociationId), nil
}

// mcpServerConfiguration builds the tool allowlist. AWS has no "allow all
// tools" option, and an unclassified tool is treated as read-only, which the
// agent may invoke without approval.
func mcpServerConfiguration() agenttypes.MCPServerConfiguration {
	readOnly, mutative := mcpReadOnlyTools, mcpMutativeTools

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

func ensureAgentSpaceRole(ctx context.Context, client IAMAPI, accountID string, logf func(string, ...any)) (string, error) {
	trustPolicy := fmt.Sprintf(`{"Version":"2012-10-17","Statement":[{"Effect":"Allow",`+
		`"Principal":{"Service":"aidevops.amazonaws.com"},"Action":"sts:AssumeRole",`+
		`"Condition":{"StringEquals":{"aws:SourceAccount":"%[1]s"},`+
		`"ArnLike":{"aws:SourceArn":"arn:aws:aidevops:*:%[1]s:agentspace/*"}}}]}`, accountID)

	arn, ours, err := ensureRole(ctx, client, AgentSpaceRoleName, trustPolicy, AgentSpacePolicyARN, logf)
	if err != nil {
		return "", err
	}
	if !ours {
		return arn, nil
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

func ensureOperatorAppRole(ctx context.Context, client IAMAPI, accountID string, logf func(string, ...any)) (string, error) {
	// sts:TagSession is required because the managed policy scopes web app
	// access per space with an aws:PrincipalTag/AgentSpaceId condition.
	trustPolicy := fmt.Sprintf(`{"Version":"2012-10-17","Statement":[{"Effect":"Allow",`+
		`"Principal":{"Service":"aidevops.amazonaws.com"},"Action":["sts:AssumeRole","sts:TagSession"],`+
		`"Condition":{"StringEquals":{"aws:SourceAccount":"%[1]s"},`+
		`"ArnLike":{"aws:SourceArn":"arn:aws:aidevops:*:%[1]s:agentspace/*"}}}]}`, accountID)

	arn, _, err := ensureRole(ctx, client, OperatorAppRoleName, trustPolicy, OperatorAppPolicyARN, logf)

	return arn, err
}

// ensureRole creates the role, or reuses one that already has its name. The
// second return value reports whether the role is one this CLI manages: a role
// the customer created themselves is reused as it is, since rewriting its trust
// policy or attaching policies to it would change something setup does not own.
func ensureRole(
	ctx context.Context,
	client IAMAPI,
	name, trustPolicy, policyARN string,
	logf func(string, ...any),
) (string, bool, error) {
	var arn string
	created, err := client.CreateRole(ctx, &iam.CreateRoleInput{
		RoleName:                 aws.String(name),
		AssumeRolePolicyDocument: aws.String(trustPolicy),
		Tags: []iamtypes.Tag{{
			Key:   aws.String(ManagedTagKey),
			Value: aws.String(ManagedTagValue),
		}},
	})
	switch {
	case err == nil:
		arn = aws.ToString(created.Role.Arn)
	case isAlreadyExists(err):
		existing, getErr := client.GetRole(ctx, &iam.GetRoleInput{RoleName: aws.String(name)})
		if getErr != nil {
			return "", false, fmt.Errorf("unable to read the existing %s role: %w", name, getErr)
		}
		arn = aws.ToString(existing.Role.Arn)

		managed, err := isManagedRole(ctx, client, name)
		if err != nil {
			return "", false, err
		}
		if !managed {
			logf(
				"Reusing %s as it is: it has no %s=%s tag, so it was not created by this CLI. "+
					"It must trust aidevops.amazonaws.com and allow %s",
				arn, ManagedTagKey, ManagedTagValue, policyARN,
			)

			return arn, false, nil
		}

		// Roles created before the trust policy covered every region are
		// pinned to one region's agent spaces, so refresh it.
		if _, err := client.UpdateAssumeRolePolicy(ctx, &iam.UpdateAssumeRolePolicyInput{
			RoleName:       aws.String(name),
			PolicyDocument: aws.String(trustPolicy),
		}); err != nil {
			return "", false, fmt.Errorf("unable to update the trust policy on the %s role: %w", name, err)
		}
	default:
		return "", false, fmt.Errorf("unable to create the %s role: %w", name, err)
	}

	if _, err := client.AttachRolePolicy(ctx, &iam.AttachRolePolicyInput{
		RoleName:  aws.String(name),
		PolicyArn: aws.String(policyARN),
	}); err != nil {
		return "", false, fmt.Errorf("unable to attach %s to %s: %w", policyARN, name, err)
	}

	return arn, true, nil
}

func isManagedRole(ctx context.Context, client IAMAPI, name string) (bool, error) {
	var marker *string
	for {
		tags, err := client.ListRoleTags(ctx, &iam.ListRoleTagsInput{
			RoleName: aws.String(name),
			Marker:   marker,
		})
		if err != nil {
			return false, fmt.Errorf("unable to read the tags on the %s role: %w", name, err)
		}
		for _, tag := range tags.Tags {
			if aws.ToString(tag.Key) == ManagedTagKey && aws.ToString(tag.Value) == ManagedTagValue {
				return true, nil
			}
		}
		if !tags.IsTruncated {
			return false, nil
		}
		marker = tags.Marker
	}
}

func isAlreadyExists(err error) bool {
	var alreadyExists *iamtypes.EntityAlreadyExistsException

	return errors.As(err, &alreadyExists)
}

// ConsoleURL is the AWS DevOps Agent console for a region.
func ConsoleURL(region string) string {
	return fmt.Sprintf("https://%s.console.aws.amazon.com/aidevops/home?region=%[1]s", region)
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
