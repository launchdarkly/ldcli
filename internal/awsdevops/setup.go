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

	AuthFlow        string
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

// ErrPlanChanged means AWS changed after the plan.
var ErrPlanChanged = errors.New("AWS changed after the plan")

type planChangedErr struct{ msg string }

func (e planChangedErr) Error() string { return e.msg + ". Run setup again" }

func (e planChangedErr) Is(target error) bool { return target == ErrPlanChanged }

func planChanged(format string, args ...any) error {
	return planChangedErr{msg: fmt.Sprintf(format, args...)}
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
	// Seen lists same-name spaces at plan.
	Seen []string
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
	ID     string
}

func (l LinkStep) describe(accountID string) string {
	if l.Exists {
		return fmt.Sprintf("Keep the AWS account %s association", accountID)
	}

	return fmt.Sprintf("Associate AWS account %s with the agent space", accountID)
}

// SetupPlan describes what setup will change.
type SetupPlan struct {
	AccountID   string
	Region      string
	AgentSpace  string
	Space       SpaceStep
	Association string
	Link        LinkStep
	MCPServer   string
	Roles       []string
	RoleSteps   []RoleStep
	OperatorApp string
}

// reportExisting notes a planned create that updated.
func reportExisting(plan SetupPlan, name string, action RoleAction, existed bool, logf func(string, ...any)) {
	planned, _ := plan.roleAction(name)
	if existed || (planned == RoleCreate && action == RoleUpdate) {
		logf("Role %s already exists, so ldcli updated it", name)
	}
}

// checkRole compares a role with its step.
func checkRole(ctx context.Context, client IAMAPI, plan SetupPlan, name string) (RoleAction, error) {
	action, err := plan.roleAction(name)
	if err != nil {
		return action, err
	}
	now, err := planRole(ctx, client, name)
	if err != nil {
		return action, err
	}

	switch {
	case now.Action == action:
		return action, nil
	case action == RoleCreate && now.Action == RoleUpdate:
		// Another ldcli run created it.
		return RoleUpdate, nil
	case now.Action == RoleCreate:
		return action, planChanged("role %s disappeared after the plan", name)
	case action == RoleCreate:
		return action, planChanged("role %s appeared after the plan", name)
	default:
		return action, planChanged("the %s tag on role %s changed", ManagedTagKey, name)
	}
}

// roleAction is the planned action for name.
func (p SetupPlan) roleAction(name string) (RoleAction, error) {
	for _, step := range p.RoleSteps {
		if step.Name == name {
			return step.Action, nil
		}
	}

	return 0, fmt.Errorf("the plan has no step for role %s", name)
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
	plan.AgentSpace = plan.Space.describe()

	plan.MCPServer, err = planMCPServer(ctx, clients, opts)
	if err != nil {
		return plan, err
	}

	if !provision {
		plan.Roles = []string{"No change"}
		plan.OperatorApp = "No change"
		plan.Association = "No change"

		return plan, nil
	}
	if plan.Space.Action != SpaceCreate {
		plan.Link.ID, err = FindAssociation(ctx, clients, plan.Space.ID, awsServiceID)
		if err != nil {
			return plan, err
		}
		plan.Link.Exists = plan.Link.ID != ""
	}
	plan.Association = plan.Link.describe(accountID)
	for _, name := range []string{AgentSpaceRoleName, OperatorAppRoleName} {
		step, err := planRole(ctx, clients.IAM, name)
		if err != nil {
			return plan, err
		}
		plan.RoleSteps = append(plan.RoleSteps, step)
		plan.Roles = append(plan.Roles, step.describe())
	}
	plan.OperatorApp = "Enable the operator app"

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
	var seen []string
	if id == "" {
		var err error
		if seen, err = spacesNamed(ctx, clients, opts.AgentSpaceName); err != nil {
			return SpaceStep{}, err
		}
		if !opts.NewAgentSpace && len(seen) > 0 {
			id = seen[0]
		}
	}
	if id == "" {
		return SpaceStep{Action: SpaceCreate, Name: opts.AgentSpaceName, Seen: seen}, nil
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

// checkSpace compares AWS with the space step.
func checkSpace(ctx context.Context, clients Clients, step SpaceStep) error {
	if step.Action == SpaceCreate {
		now, err := spacesNamed(ctx, clients, step.Name)
		if err != nil {
			return err
		}
		for _, id := range now {
			if !slices.Contains(step.Seen, id) {
				return planChanged("agent space %s appeared after the plan", step.Name)
			}
		}

		return nil
	}

	_, err := clients.Agent.GetAgentSpace(ctx, &devopsagent.GetAgentSpaceInput{AgentSpaceId: aws.String(step.ID)})
	var missing *agenttypes.ResourceNotFoundException
	switch {
	case errors.As(err, &missing):
		return planChanged("agent space %s disappeared after the plan", step.ID)
	case err != nil:
		return fmt.Errorf("unable to read agent space %s: %w", step.ID, err)
	}

	return nil
}

func planMCPServer(ctx context.Context, clients Clients, opts SetupOptions) (string, error) {
	existing := opts.MCPServiceID
	if existing == "" {
		found, err := FindMCPServer(ctx, clients, opts.mcpEndpoint())
		if err != nil {
			return "", err
		}
		existing = found
	}

	switch {
	case existing != "" && opts.ReplaceMCPToken && opts.LDAccessToken != "":
		return fmt.Sprintf("Replace the token of MCP server %s (%s)", MCPServerName, existing), nil
	case existing != "":
		return fmt.Sprintf("Reuse MCP server %s (%s)", MCPServerName, existing), nil
	case opts.LDAccessToken != "":
		return fmt.Sprintf("Register MCP server %s at %s", MCPServerName, opts.mcpEndpoint()), nil
	default:
		return fmt.Sprintf("Ask for a token, then register MCP server %s", MCPServerName), nil
	}
}

// Setup plans a run, then executes the plan.
func Setup(ctx context.Context, clients Clients, opts SetupOptions) (SetupResult, error) {
	plan, err := PlanSetup(ctx, clients, opts, true)
	if err != nil {
		return SetupResult{}, err
	}

	return RunPlan(ctx, clients, opts, plan)
}

// RunPlan executes a plan from PlanSetup.
func RunPlan(ctx context.Context, clients Clients, opts SetupOptions, plan SetupPlan) (SetupResult, error) {
	logf := opts.Logf
	if logf == nil {
		logf = func(string, ...any) {}
	}

	accountID, err := clients.AccountID(ctx)
	if err != nil {
		return SetupResult{}, err
	}

	result := SetupResult{AccountID: accountID, Region: clients.Region}

	if plan.Space.Action == SpaceConnect {
		return result, errors.New("a connect plan runs through ConnectExisting")
	}
	if err := checkSpace(ctx, clients, plan.Space); err != nil {
		return result, err
	}
	if plan.Link.Exists {
		found, err := FindAssociation(ctx, clients, plan.Space.ID, awsServiceID)
		if err != nil {
			return result, err
		}
		if found == "" {
			return result, planChanged("the AWS account association disappeared after the plan")
		}
	}
	spaceRole, err := checkRole(ctx, clients.IAM, plan, AgentSpaceRoleName)
	if err != nil {
		return result, err
	}
	appRole, err := checkRole(ctx, clients.IAM, plan, OperatorAppRoleName)
	if err != nil {
		return result, err
	}
	var existed bool
	result.AgentSpaceRoleARN, existed, err = ensureAgentSpaceRole(ctx, clients.IAM, accountID, spaceRole, logf)
	if err != nil {
		return result, err
	}
	reportExisting(plan, AgentSpaceRoleName, spaceRole, existed, logf)
	logf("Agent space role %s is ready", AgentSpaceRoleName)

	result.OperatorAppRoleARN, existed, err = ensureOperatorAppRole(ctx, clients.IAM, accountID, appRole, logf)
	if err != nil {
		return result, err
	}
	reportExisting(plan, OperatorAppRoleName, appRole, existed, logf)
	logf("Operator app role %s is ready", OperatorAppRoleName)

	if plan.Space.Action == SpaceCreate {
		// Narrow the window after role writes.
		if err := checkSpace(ctx, clients, plan.Space); err != nil {
			return result, err
		}
		space, err := clients.Agent.CreateAgentSpace(ctx, &devopsagent.CreateAgentSpaceInput{
			Name:        aws.String(plan.Space.Name),
			Description: aws.String(agentSpaceDescription),
		})
		if err != nil {
			return result, fmt.Errorf("unable to create the agent space: %w", err)
		}
		result.AgentSpaceID = aws.ToString(space.AgentSpace.AgentSpaceId)
		logf("Created agent space %s (%s)", plan.Space.Name, result.AgentSpaceID)
	} else {
		result.AgentSpaceID = plan.Space.ID
		logf("Reused agent space %s (%s)", plan.Space.Name, plan.Space.ID)
		if opts.AgentSpaceID == "" {
			logf("Pass --new-agent-space to create another agent space")
		}
	}

	result.AWSAssociationID, err = FindAssociation(ctx, clients, result.AgentSpaceID, awsServiceID)
	if err != nil {
		return result, err
	}
	if result.AWSAssociationID != "" {
		logf("AWS account %s is already associated with the agent space", accountID)
	} else {
		awsAssociation, err := associateAWSAccount(ctx, clients, accountID, result, logf)
		if err != nil {
			return result, fmt.Errorf("unable to associate the AWS account with the agent space: %w", err)
		}
		result.AWSAssociationID = aws.ToString(awsAssociation.Association.AssociationId)
		logf("Associated AWS account %s with the agent space", accountID)
	}

	operatorApp, err := clients.Agent.EnableOperatorApp(ctx, operatorAppInput(result.AgentSpaceID, result.OperatorAppRoleARN, opts))
	if err != nil {
		return result, fmt.Errorf("unable to enable the operator app: %w", err)
	}
	result.OperatorAppURL = aws.ToString(operatorApp.OperatorAppUrl)
	logf("Enabled the operator app")

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

// ConnectExisting connects LaunchDarkly to an agent space the customer already
// set up. Only the MCP server is touched: the IAM roles, the AWS account
// association and the operator app belong to however they provisioned that
// space, and re-enabling the operator app would overwrite its sign-in
// configuration.
func ConnectExisting(ctx context.Context, clients Clients, opts SetupOptions) (SetupResult, error) {
	logf := opts.Logf
	if logf == nil {
		logf = func(string, ...any) {}
	}

	accountID, err := clients.AccountID(ctx)
	if err != nil {
		return SetupResult{}, err
	}

	space, err := clients.Agent.GetAgentSpace(ctx, &devopsagent.GetAgentSpaceInput{
		AgentSpaceId: aws.String(opts.AgentSpaceID),
	})
	if err != nil {
		return SetupResult{}, fmt.Errorf("unable to read agent space %s: %w", opts.AgentSpaceID, err)
	}

	result := SetupResult{
		AccountID:      accountID,
		Region:         clients.Region,
		AgentSpaceID:   opts.AgentSpaceID,
		OperatorAppURL: OperatorAppURL(opts.AgentSpaceID),
	}
	logf("Using agent space %s (%s)", aws.ToString(space.AgentSpace.Name), result.AgentSpaceID)

	if err := registerMCPServer(ctx, clients, opts, &result, logf); err != nil {
		return result, err
	}

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
			logf("Waiting for AWS to accept the agent space role")
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
			logf("Waiting for AWS to reach the MCP server at %s", endpoint)
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
		logf("Reused MCP server %s (%s) with its current token", MCPServerName, existing)
		logf("Pass --access-token and --replace-mcp-token to change the token")

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
		logf("Removed MCP server %s (%s) to replace its token", MCPServerName, existing)
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
	logf("Registered MCP server %s (%s)", MCPServerName, result.MCPServiceID)

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
		logf("Associated MCP server %s with agent space (%s) again", MCPServerName, agentSpaceID)
	}

	return nil
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
		logf("Disassociated service (%s) from agent space (%s)", serviceID, agentSpaceID)
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
		logf("MCP server %s is already associated with the agent space", MCPServerName)

		return nil
	}

	associationID, err := associateMCPServerWith(ctx, clients, result.AgentSpaceID, result.MCPServiceID)
	if err != nil {
		return err
	}
	result.MCPAssociationID = associationID
	logf("Associated MCP server %s with the agent space", MCPServerName)

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

func ensureAgentSpaceRole(ctx context.Context, client IAMAPI, accountID string, action RoleAction, logf func(string, ...any)) (string, bool, error) {
	trustPolicy := fmt.Sprintf(`{"Version":"2012-10-17","Statement":[{"Effect":"Allow",`+
		`"Principal":{"Service":"aidevops.amazonaws.com"},"Action":"sts:AssumeRole",`+
		`"Condition":{"StringEquals":{"aws:SourceAccount":"%[1]s"},`+
		`"ArnLike":{"aws:SourceArn":"arn:aws:aidevops:*:%[1]s:agentspace/*"}}}]}`, accountID)

	arn, ours, existed, err := ensureRole(ctx, client, AgentSpaceRoleName, trustPolicy, AgentSpacePolicyARN, action, logf)
	if err != nil {
		return "", false, err
	}
	if !ours {
		return arn, false, nil
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
		return "", false, fmt.Errorf("unable to add the %s policy to %s: %w", ServiceLinkedRolePolicyName, AgentSpaceRoleName, err)
	}

	return arn, existed, nil
}

func ensureOperatorAppRole(ctx context.Context, client IAMAPI, accountID string, action RoleAction, logf func(string, ...any)) (string, bool, error) {
	// sts:TagSession is required because the managed policy scopes web app
	// access per space with an aws:PrincipalTag/AgentSpaceId condition.
	trustPolicy := fmt.Sprintf(`{"Version":"2012-10-17","Statement":[{"Effect":"Allow",`+
		`"Principal":{"Service":"aidevops.amazonaws.com"},"Action":["sts:AssumeRole","sts:TagSession"],`+
		`"Condition":{"StringEquals":{"aws:SourceAccount":"%[1]s"},`+
		`"ArnLike":{"aws:SourceArn":"arn:aws:aidevops:*:%[1]s:agentspace/*"}}}]}`, accountID)

	arn, _, existed, err := ensureRole(ctx, client, OperatorAppRoleName, trustPolicy, OperatorAppPolicyARN, action, logf)

	return arn, existed, err
}

// ensureRole does the planned role action.
func ensureRole(
	ctx context.Context,
	client IAMAPI,
	name, trustPolicy, policyARN string,
	action RoleAction,
	logf func(string, ...any),
) (arn string, ours, existed bool, err error) {
	raced := false
	if action == RoleCreate {
		created, createErr := client.CreateRole(ctx, &iam.CreateRoleInput{
			RoleName:                 aws.String(name),
			AssumeRolePolicyDocument: aws.String(trustPolicy),
			Tags: []iamtypes.Tag{{
				Key:   aws.String(ManagedTagKey),
				Value: aws.String(ManagedTagValue),
			}},
		})
		switch {
		case isAlreadyExists(createErr):
			// A tagged role is already done.
			action, raced = RoleUpdate, true
		case createErr != nil:
			return "", false, false, fmt.Errorf("unable to create the %s role: %w", name, createErr)
		default:
			return aws.ToString(created.Role.Arn), true, false, attachPolicy(ctx, client, name, policyARN)
		}
	}

	existing, err := client.GetRole(ctx, &iam.GetRoleInput{RoleName: aws.String(name)})
	var missing *iamtypes.NoSuchEntityException
	switch {
	case errors.As(err, &missing):
		return "", false, false, planChanged("role %s disappeared after the plan", name)
	case err != nil:
		return "", false, false, fmt.Errorf("unable to read the existing %s role: %w", name, err)
	}
	arn = aws.ToString(existing.Role.Arn)

	managed, err := isManagedRole(ctx, client, name)
	if err != nil {
		return "", false, false, err
	}
	switch {
	case raced && !managed:
		return "", false, false, planChanged("role %s appeared after the plan", name)
	case managed != (action == RoleUpdate):
		return "", false, false, planChanged("the %s tag on role %s changed", ManagedTagKey, name)
	}
	if !managed {
		logf("Role %s has no %s tag, so ldcli did not change it", name, ManagedTagKey)
		logf("Role %s must trust aidevops.amazonaws.com and allow %s", name, policyName(policyARN))

		return arn, false, false, nil
	}

	// Older trust policies covered one region.
	if _, err := client.UpdateAssumeRolePolicy(ctx, &iam.UpdateAssumeRolePolicyInput{
		RoleName:       aws.String(name),
		PolicyDocument: aws.String(trustPolicy),
	}); err != nil {
		return "", false, false, fmt.Errorf("unable to update the trust policy on the %s role: %w", name, err)
	}

	return arn, true, raced, attachPolicy(ctx, client, name, policyARN)
}

func attachPolicy(ctx context.Context, client IAMAPI, name, policyARN string) error {
	if _, err := client.AttachRolePolicy(ctx, &iam.AttachRolePolicyInput{
		RoleName:  aws.String(name),
		PolicyArn: aws.String(policyARN),
	}); err != nil {
		return fmt.Errorf("unable to attach %s to %s: %w", policyARN, name, err)
	}

	return nil
}

// policyName is the last part of an ARN.
func policyName(arn string) string {
	return arn[strings.LastIndex(arn, "/")+1:]
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
