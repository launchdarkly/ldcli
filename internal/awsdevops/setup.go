package awsdevops

import (
	"context"
	"errors"
	"fmt"
	"maps"
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

	// ManagedTagKey marks the roles and agent spaces this CLI created, so a
	// re-run never rewrites a role or a space that belongs to the customer.
	ManagedTagKey   = "ldcli:managed"
	ManagedTagValue = "aws-devops-agent"

	// awsServiceID is the well-known service identifier for the agent's own
	// AWS account association.
	awsServiceID = "aws"

	roleAssumableTimeout     = 2 * time.Minute
	roleAssumableFirstWait   = time.Second
	roleAssumableMaxInterval = 10 * time.Second

	MCPServerName     = "LaunchDarkly"
	MCPServerEndpoint = "https://mcp.launchdarkly.com/mcp/launchdarkly"

	// MCPOAuthServerName is the name of the MCP server that setup registers.
	// It differs from MCPServerName, the name that the AWS console guide
	// uses, so it does not conflict with a server that someone registered
	// with a token.
	MCPOAuthServerName = "LaunchDarkly-OAuth"

	// oauthTimeout is how long setup waits for a person to approve access.
	oauthTimeout = 10 * time.Minute

	agentSpaceDescription = "Managed by the LaunchDarkly CLI"
	mcpServerDescription  = "LaunchDarkly feature flag management MCP server"
	// ElevatedActionsPreference lets the agent run actions that change AWS
	// resources.
	ElevatedActionsPreference = "elevatedActionsEnabled"
)

// oauthPollInterval is how often setup looks for the server that AWS
// registers after a person approves access. Tests make it shorter.
var oauthPollInterval = 5 * time.Second

// The agent invokes read-only tools without asking for approval, so anything
// that changes LaunchDarkly state has to be classified explicitly. The lists
// cover the steps of the experiment-orchestration skill: find a flag, create
// it, run an experiment, read its results, and ramp or clean up the flag. The
// skill keeps its experiment record in the flag description and creates a
// missing primary metric, so the lists include update-flag-settings and
// create-metric. It checks that the metric's event arrives with
// list-metric-events, and sets the off variation with update-off-variation
// when create-flag does not take defaults. The lists also have the tools for
// the rest of that work that the skill does not name: change an experiment
// between iterations, check exposures and contexts before a rollout, and
// record an automated rollout for the change.
// They also cover incident work: turn a change off for one segment or
// context, and ask for approval where an environment needs it, then apply
// the request after a reviewer approves it.
var (
	mcpReadOnlyTools = []string{
		"list-projects",
		"get-project",
		"get-environment",
		"list-flags",
		"get-flag",
		"get-flag-status-across-envs",
		"check-flag-rollout-complete",
		"query-change-history",
		"list-metrics",
		"get-metric",
		"list-metric-events",
		"list-experiments",
		"get-experiment",
		"get-experiment-results",
		"get-experiment-metric-results",
		"get-flag-health",
		"get-flag-unique-context-exposures",
		"search-contexts",
		"list-segments",
		"get-segment",
		"list-approval-requests",
		"list-release-policies",
		"match-release-policies",
	}
	mcpMutativeTools = []string{
		"toggle-flag",
		"create-flag",
		"update-flag-settings",
		"update-off-variation",
		"create-metric",
		"update-rollout",
		"archive-flag",
		"create-experiment",
		"update-experiment",
		"start-experiment-iteration",
		"save-and-start-experiment-iteration",
		"stop-experiment-iteration",
		"start-guarded-rollout",
		"stop-guarded-rollout",
		"create-automated-rollout-config",
		"update-targeting-rules",
		"update-individual-targets",
		"create-approval-request",
		"apply-approval-request",
	}
)

// SetupOptions describes what to provision.
type SetupOptions struct {
	AgentSpaceID   string
	AgentSpaceName string
	NewAgentSpace  bool
	// FullSetup: the user passed --full-setup.
	FullSetup bool

	AuthFlow string
	// AuthFlowSet: the user passed --auth-flow.
	AuthFlowSet     bool
	IdcInstanceARN  string
	IssuerURL       string
	IdpClientID     string
	IdpClientSecret string

	// MCPServiceID, when set, is the registration to connect instead of the
	// server of the AWS account.
	MCPServiceID string
	// MCPEndpoint overrides the LaunchDarkly MCP endpoint, for an account
	// running the open-source MCP server itself. The endpoint identifies the
	// registration, so a self-hosted one is never confused with ours.
	MCPEndpoint string
	// OpenURL, when set, opens the OAuth approval page in a browser.
	OpenURL func(url string) error

	// ReplaceSkill overwrites the SKILL.md of an existing
	// experiment-orchestration skill that someone edited. Setup updates a
	// SKILL.md from an older ldcli version without this option.
	ReplaceSkill bool

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
	// OperatorAppEnabled is false when a connect run found the app off.
	OperatorAppEnabled bool   `json:"operatorAppEnabled"`
	MCPServiceID       string `json:"mcpServiceId,omitempty"`
	MCPAssociationID   string `json:"mcpAssociationId,omitempty"`
	// MCPToolsAdded lists the ldcli tools that setup added to the allowlist of
	// an existing MCP server association.
	MCPToolsAdded []string `json:"mcpToolsAdded,omitempty"`
	// MCPToolsError is set if the allowlist of an existing association misses
	// ldcli tools that setup could not add. The rest of the setup ran.
	MCPToolsError string `json:"mcpToolsError,omitempty"`
	SkillAssetID  string `json:"skillAssetId,omitempty"`
	// SkillAction is what the skill step did, for example "kept" or "left".
	SkillAction string `json:"skillAction,omitempty"`
	// SkillError is set if the skill step failed or was skipped. The skill is
	// optional, so the rest of the setup ran.
	SkillError   string `json:"skillError,omitempty"`
	AgentAssetID string `json:"agentAssetId,omitempty"`
	// AgentAction is what the custom agent step did, for example "created".
	AgentAction string `json:"agentAction,omitempty"`
	// AgentError is set if the custom agent step failed or was skipped.
	AgentError string `json:"agentError,omitempty"`
	// mcpServerName is the name of the server in AWS, which the tool IDs of
	// the custom agent start with.
	mcpServerName string
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
		return fmt.Sprintf("Update the trust policy and permissions of role %s", s.Name)
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
	// EnableElevated turns on elevated actions on the space.
	EnableElevated bool
}

func (s SpaceStep) describe() string {
	label := fmt.Sprintf("%s (%s)", s.Name, s.ID)
	elevated := ""
	if s.EnableElevated {
		elevated = ", and turn on elevated actions"
	}
	switch s.Action {
	case SpaceCreate:
		return "Create agent space " + s.Name + elevated
	case SpaceConnect:
		return "Connect to agent space " + label
	default:
		return "Reuse agent space " + label + elevated
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

		// Name the current sign-in, so a change to a sign-in that someone
		// else set up is visible in the plan.
		return fmt.Sprintf("Change the operator app sign-in from %s to %s", flowLabel(a.Flow), flowLabel(flow))
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

// checkApp compares the app with its step.
func checkApp(ctx context.Context, clients Clients, plan SetupPlan) error {
	if plan.Space.Action == SpaceCreate {
		return nil
	}
	state, err := appState(ctx, clients, plan.Space.ID)
	on := state.Flow != ""
	switch {
	case err != nil:
		return err
	case on && plan.App.Action == AppEnable:
		return planChanged("the operator app was enabled after the plan")
	case !on && plan.App.Action != AppEnable:
		return planChanged("the operator app was disabled after the plan")
	case on && state.Flow != plan.App.Flow:
		// This also stops a run if someone else made the planned change, for
		// example IAM to IdC, after the plan. EnableOperatorApp would work then,
		// but a new plan is safe and finds that nothing is left to change.
		return planChanged("the operator app sign-in changed after the plan")
	}

	return nil
}

// MCPAction is the planned MCP server action.
type MCPAction int

const (
	MCPRegister MCPAction = iota
	MCPReuse
)

// MCPStep is the planned MCP server step.
type MCPStep struct {
	Action    MCPAction
	ServiceID string
	// Endpoint is the URL that the server serves.
	Endpoint string
	// Name is the name of an existing server in AWS.
	Name string
	// Spaces lists spaces linked to the server.
	Spaces []string
	// SpaceID is the agent space of the step. It is empty for a new space.
	SpaceID string
	// MissingTools are the ldcli tools that the allowlist of the existing
	// association in the space misses. CanAddTools says whether setup can add
	// them.
	MissingTools []string
	CanAddTools  bool
}

// shared reports whether other spaces use the server.
func (m MCPStep) shared() bool {
	return len(m.otherSpaces()) > 0
}

// otherSpaces lists the spaces, other than the space of the step, that use
// the server.
func (m MCPStep) otherSpaces() []string {
	var others []string
	for _, id := range m.Spaces {
		if id != m.SpaceID {
			others = append(others, id)
		}
	}

	return others
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
	Skill    SkillStep
	Agent    AgentStep
}

// PlanText is the plan as lines for a person to read.
type PlanText struct {
	// Scope tells if the run is a full setup.
	Scope       string
	AgentSpace  string
	Association string
	Roles       []string
	OperatorApp string
	MCPServer   string
	Skill       string
	Agent       string
}

// Text describes each step of the plan.
func (p SetupPlan) Text() PlanText {
	text := PlanText{
		Scope:      "Full setup",
		AgentSpace: p.Space.describe(),
		MCPServer:  p.MCP.describe(),
		Skill:      p.Skill.describe(),
		Agent:      p.Agent.describe(),
	}
	if p.Space.Action == SpaceConnect {
		text.Scope = "Connect the MCP server only. To also update the IAM roles, " +
			"account association, and operator app of the space, run setup again with --full-setup"
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

	plan.MCP, err = planMCPStep(ctx, clients, opts, plan.Space.ID)
	if err != nil {
		return plan, err
	}

	if plan.MCP.Action == MCPReuse && plan.Space.Action != SpaceCreate {
		association, err := findMCPAssociation(ctx, clients, plan.Space.ID, plan.MCP.ServiceID)
		if err != nil {
			return plan, err
		}
		if association != nil {
			plan.MCP.MissingTools, plan.MCP.CanAddTools = missingTools(*association)
		}
	}

	// The skill and the prompt come from ai-tooling main. Both steps compare
	// and write these texts, so a later change there does not reach the run.
	skill, prompt, fetchErr := fetchSources(ctx, clients.Sources)
	plan.Skill = planSkill(ctx, clients, opts, plan.Space, skill, fetchErr)
	plan.Agent = planAgent(ctx, clients, plan.Space, plan.MCP, plan.Skill, prompt, fetchErr)

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
		id, err := FindAWSAssociation(ctx, clients, plan.Space.ID, accountID)
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
		case len(seen) > 0 && opts.NewAgentSpace && provision:
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
		if opts.AgentSpaceName == "" {
			return SpaceStep{}, errors.New("no agent space to connect to. Pass --agent-space-id")
		}

		return SpaceStep{}, fmt.Errorf("no agent space named %s to connect to. Pass --agent-space-id", opts.AgentSpaceName)
	}
	if id == "" && opts.AgentSpaceName == "" {
		return SpaceStep{}, errors.New("a new agent space needs a name. Pass --agent-space-name")
	}
	if id == "" {
		return SpaceStep{Action: SpaceCreate, Name: opts.AgentSpaceName, EnableElevated: true}, nil
	}

	space, err := clients.Agent.GetAgentSpace(ctx, &devopsagent.GetAgentSpaceInput{AgentSpaceId: aws.String(id)})
	if err != nil {
		return SpaceStep{}, fmt.Errorf("unable to read agent space %s: %w", id, err)
	}
	step := SpaceStep{Action: SpaceReuse, ID: id, Name: aws.ToString(space.AgentSpace.Name)}
	if !provision {
		step.Action = SpaceConnect
	} else {
		step.EnableElevated = !space.AgentSpace.Preferences[ElevatedActionsPreference]
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
		if len(now) > 0 {
			return planChanged("agent space %s appeared after the plan", step.Name)
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

// enableElevatedActions turns on elevated actions on a space. An update
// replaces all the preferences, so it sends the current ones too.
func enableElevatedActions(ctx context.Context, clients Clients, spaceID string) error {
	space, err := clients.Agent.GetAgentSpace(ctx, &devopsagent.GetAgentSpaceInput{AgentSpaceId: aws.String(spaceID)})
	if err != nil {
		return fmt.Errorf("unable to read agent space %s: %w", spaceID, err)
	}
	preferences := maps.Clone(space.AgentSpace.Preferences)
	if preferences == nil {
		preferences = map[string]bool{}
	}
	preferences[ElevatedActionsPreference] = true
	_, err = clients.Agent.UpdateAgentSpace(ctx, &devopsagent.UpdateAgentSpaceInput{
		AgentSpaceId: aws.String(spaceID),
		Preferences:  preferences,
	})
	if err != nil {
		return fmt.Errorf("unable to turn on elevated actions on agent space %s: %w", spaceID, err)
	}

	return nil
}

func planMCPStep(ctx context.Context, clients Clients, opts SetupOptions, spaceID string) (MCPStep, error) {
	step, err := planMCPAction(ctx, clients, opts, spaceID)
	step.Endpoint = opts.mcpEndpoint()
	step.SpaceID = spaceID

	return step, err
}

func planMCPAction(ctx context.Context, clients Clients, opts SetupOptions, spaceID string) (MCPStep, error) {
	linked, err := linkedMCPServer(ctx, clients, spaceID, opts.mcpEndpoint())
	if err != nil {
		return MCPStep{}, err
	}
	existing, name := linked.ID, linked.Name
	switch {
	case opts.MCPServiceID != "":
		name, err = checkMCPServiceID(ctx, clients, opts.MCPServiceID, opts.mcpEndpoint())
		if err != nil {
			return MCPStep{}, err
		}
		// A space with two LaunchDarkly servers makes the next run unable to
		// pick one.
		if linked.ID != "" && linked.ID != opts.MCPServiceID {
			return MCPStep{}, fmt.Errorf(
				"agent space %s already uses MCP server %s (%s). To use MCP server %s instead, "+
					"disconnect %s from the agent space in the AWS console, then run setup again",
				spaceID, linked.Name, linked.ID, opts.MCPServiceID, linked.ID,
			)
		}
		existing = opts.MCPServiceID
	case existing == "":
		// Every agent space uses the one server of the account.
		account, err := accountMCPServer(ctx, clients, opts.mcpEndpoint())
		if err != nil {
			return MCPStep{}, err
		}
		existing, name = account.ID, account.Name
	}
	if existing == "" {
		return MCPStep{Action: MCPRegister}, nil
	}

	// The other spaces are only for the plan text.
	spaces, err := spacesWith(ctx, clients, existing)
	if err != nil {
		return MCPStep{}, err
	}

	return MCPStep{Action: MCPReuse, ServiceID: existing, Name: name, Spaces: spaces}, nil
}

// checkMCPServiceID needs a registered MCP server at the LaunchDarkly
// endpoint. Setup finds a LaunchDarkly MCP server by its endpoint, because the
// name is the choice of the customer. A server at another endpoint is not one,
// so setup does not connect it to the agent space.
// It returns the name of the server.
func checkMCPServiceID(ctx context.Context, clients Clients, id, endpoint string) (string, error) {
	services, err := listServices(ctx, clients)
	if err != nil {
		return "", err
	}
	for _, service := range services {
		if aws.ToString(service.ServiceId) != id || service.ServiceType != agenttypes.ServiceMcpServer {
			continue
		}
		if served := mcpEndpointOf(service); served != endpoint {
			return "", fmt.Errorf(
				"MCP server %s serves %s, not the LaunchDarkly MCP server at %s. "+
					"If it is a LaunchDarkly MCP server that you host, pass --mcp-endpoint %s",
				id, served, endpoint, served,
			)
		}

		return serviceName(service), nil
	}

	return "", fmt.Errorf("ID %s names no registered MCP server", id)
}

// name is the name of the server in AWS. A new server gets
// MCPOAuthServerName.
func (m MCPStep) name() string {
	if m.Action == MCPRegister {
		return MCPOAuthServerName
	}
	if m.Name != "" {
		return m.Name
	}

	return MCPServerName
}

func (m MCPStep) describe() string {
	if m.Action == MCPRegister {
		return fmt.Sprintf(
			"Register MCP server %s for the AWS account at %s. You approve access to LaunchDarkly in a browser",
			MCPOAuthServerName, m.Endpoint,
		)
	}
	line := fmt.Sprintf("Reuse MCP server %s (%s)", m.name(), m.ServiceID)
	if m.shared() {
		line += fmt.Sprintf(". Agent spaces %s also use it", strings.Join(m.otherSpaces(), ", "))
	}
	switch {
	case len(m.MissingTools) == 0:
	case m.CanAddTools:
		line += ", and add " + strings.Join(m.MissingTools, ", ") +
			" to its allowlist. The rewrite gives each tool the ldcli classification"
	default:
		line += ". Its allowlist misses " + strings.Join(m.MissingTools, ", ") +
			", which setup cannot add because the allowlist has tools that ldcli does not classify"
	}

	return line
}

// checkMCP compares AWS with the MCP step of agent space spaceID. The ID is
// empty for a space that the run creates.
func checkMCP(ctx context.Context, clients Clients, opts SetupOptions, step MCPStep, spaceID string) error {
	if step.Action == MCPReuse {
		services, err := listServices(ctx, clients)
		if err != nil {
			return err
		}
		for _, service := range services {
			if aws.ToString(service.ServiceId) == step.ServiceID {
				return nil
			}
		}

		return planChanged("MCP server %s disappeared after the plan", step.ServiceID)
	}

	linked, err := linkedMCPServer(ctx, clients, spaceID, opts.mcpEndpoint())
	if err != nil {
		return err
	}
	if linked.ID != "" {
		return planChanged("agent space %s got an MCP server at %s after the plan", spaceID, opts.mcpEndpoint())
	}
	account, err := accountMCPServer(ctx, clients, opts.mcpEndpoint())
	if err != nil {
		return err
	}
	if account.ID != "" {
		return planChanged("the AWS account got MCP server %s (%s) after the plan", account.Name, account.ID)
	}

	return nil
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
		return result, errors.New("a connect plan runs through ConnectPlan")
	}
	if err := checkSpace(ctx, clients, plan.Space); err != nil {
		return result, err
	}
	if plan.Link.Exists {
		found, err := FindAWSAssociation(ctx, clients, plan.Space.ID, accountID)
		if err != nil {
			return result, err
		}
		if found == "" {
			return result, planChanged("the AWS account association disappeared after the plan")
		}
	}
	if err := checkApp(ctx, clients, plan); err != nil {
		return result, err
	}
	if err := checkMCP(ctx, clients, opts, plan.MCP, plan.Space.ID); err != nil {
		return result, err
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
		input := &devopsagent.CreateAgentSpaceInput{
			Name:        aws.String(plan.Space.Name),
			Description: aws.String(agentSpaceDescription),
			Tags:        map[string]string{ManagedTagKey: ManagedTagValue},
		}
		if plan.Space.EnableElevated {
			input.Preferences = map[string]bool{ElevatedActionsPreference: true}
		}
		space, err := clients.Agent.CreateAgentSpace(ctx, input)
		if err != nil {
			return result, fmt.Errorf("unable to create the agent space: %w", err)
		}
		result.AgentSpaceID = aws.ToString(space.AgentSpace.AgentSpaceId)
		logf("Created agent space %s (%s)", plan.Space.Name, result.AgentSpaceID)
	} else {
		result.AgentSpaceID = plan.Space.ID
		logf("Reused agent space %s (%s)", plan.Space.Name, plan.Space.ID)
		if plan.Space.EnableElevated {
			if err := enableElevatedActions(ctx, clients, plan.Space.ID); err != nil {
				return result, err
			}
			logf("Turned on elevated actions on agent space %s", plan.Space.ID)
		}
	}

	result.AWSAssociationID, err = FindAWSAssociation(ctx, clients, result.AgentSpaceID, accountID)
	if err != nil {
		return result, err
	}
	switch {
	case result.AWSAssociationID != "":
		logf("AWS account %s is already associated with the agent space", accountID)
	case plan.Link.Exists:
		// The plan keeps the association, and the role steps can take
		// minutes. Someone removed it in that time, so do not create one.
		return result, planChanged("the AWS account association disappeared after the plan")
	default:
		awsAssociation, err := associateAWSAccount(ctx, clients, accountID, result, logf)
		if err != nil {
			return result, fmt.Errorf("unable to associate the AWS account with the agent space: %w", err)
		}
		result.AWSAssociationID = aws.ToString(awsAssociation.Association.AssociationId)
		logf("Associated AWS account %s with the agent space", accountID)
	}

	// The role and association steps can take minutes, so check the app again
	// right before its step.
	if err := checkApp(ctx, clients, plan); err != nil {
		return result, err
	}
	if plan.App.Action == AppKeep {
		result.OperatorAppURL = plan.App.URL
		logf("Kept the operator app and its sign-in")
	} else {
		operatorApp, err := clients.Agent.EnableOperatorApp(ctx, operatorAppInput(result.AgentSpaceID, result.OperatorAppRoleARN, opts))
		if err != nil {
			return result, fmt.Errorf("unable to enable the operator app: %w", err)
		}
		result.OperatorAppURL = aws.ToString(operatorApp.OperatorAppUrl)
		logf("Enabled the operator app")
	}
	// The app is on here, so an empty URL means only that AWS sent none.
	result.OperatorAppEnabled = true
	if result.OperatorAppURL == "" {
		result.OperatorAppURL = OperatorAppURL(result.AgentSpaceID)
	}

	// The role and association steps can take minutes. A server that appeared
	// or went in that time makes AWS reject the step with an error that does
	// not say that the plan changed, so check the server again. A new space
	// has its ID by now.
	if err := checkMCP(ctx, clients, opts, plan.MCP, result.AgentSpaceID); err != nil {
		return result, err
	}
	if err := registerMCPServer(ctx, clients, opts, plan.MCP, &result, logf); err != nil {
		return result, err
	}
	// The optional skill and agent steps go last.
	installSkill(ctx, clients, plan.Skill, &result, logf)
	installAgent(ctx, clients, plan.Agent, &result, logf)

	return result, nil
}

// mcpEndpoint is the endpoint the MCP server is registered at and found by.
func (o SetupOptions) mcpEndpoint() string {
	if o.MCPEndpoint != "" {
		return o.MCPEndpoint
	}

	return MCPServerEndpoint
}

// ConnectPlan runs a connect plan.
func ConnectPlan(ctx context.Context, clients Clients, opts SetupOptions, plan SetupPlan) (SetupResult, error) {
	logf := opts.Logf
	if logf == nil {
		logf = func(string, ...any) {}
	}

	if plan.Space.Action != SpaceConnect {
		return SetupResult{}, errors.New("a setup plan runs through RunPlan")
	}
	accountID, err := clients.AccountID(ctx)
	if err != nil {
		return SetupResult{}, err
	}
	if err := checkSpace(ctx, clients, plan.Space); err != nil {
		return SetupResult{}, err
	}

	result := SetupResult{
		AccountID:    accountID,
		Region:       clients.Region,
		AgentSpaceID: plan.Space.ID,
	}
	// A connect run does not change the app, so report it as it is now. It
	// can have changed since the plan.
	app, err := appState(ctx, clients, plan.Space.ID)
	if err != nil {
		return result, err
	}
	// An app that is off has no URL.
	result.OperatorAppEnabled = app.Flow != ""
	if result.OperatorAppEnabled {
		result.OperatorAppURL = app.URL
		if result.OperatorAppURL == "" {
			result.OperatorAppURL = OperatorAppURL(plan.Space.ID)
		}
	}
	logf("Using agent space %s (%s)", plan.Space.Name, result.AgentSpaceID)

	if err := checkMCP(ctx, clients, opts, plan.MCP, plan.Space.ID); err != nil {
		return result, err
	}
	if err := registerMCPServer(ctx, clients, opts, plan.MCP, &result, logf); err != nil {
		return result, err
	}
	installSkill(ctx, clients, plan.Skill, &result, logf)
	installAgent(ctx, clients, plan.Agent, &result, logf)

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

func registerMCPServer(
	ctx context.Context,
	clients Clients,
	opts SetupOptions,
	step MCPStep,
	result *SetupResult,
	logf func(string, ...any),
) error {
	if step.Action == MCPReuse {
		result.MCPServiceID = step.ServiceID
		logf("Reused MCP server %s (%s)", step.name(), step.ServiceID)

		return associateMCPServer(ctx, clients, opts, step, step.name(), result, logf)
	}

	return registerOAuthMCPServer(ctx, clients, opts, step, result, logf)
}

// registerOAuthMCPServer registers the MCP server of the AWS account with
// OAuth. AWS does not register the server at once. It sends an approval URL,
// and it registers the server after a person approves access in a browser.
// Setup then finds the server by its endpoint and its OAuth sign-in.
func registerOAuthMCPServer(
	ctx context.Context,
	clients Clients,
	opts SetupOptions,
	step MCPStep,
	result *SetupResult,
	logf func(string, ...any),
) error {
	endpoint := opts.mcpEndpoint()
	input := &devopsagent.RegisterServiceInput{
		Service: agenttypes.PostRegisterServiceSupportedServiceMcpServer,
		ServiceDetails: &agenttypes.ServiceDetailsMemberMcpserver{
			Value: agenttypes.MCPServerDetails{
				Name:        aws.String(MCPOAuthServerName),
				Endpoint:    aws.String(endpoint),
				Description: aws.String(mcpServerDescription),
				AuthorizationConfig: &agenttypes.MCPServerAuthorizationConfigMemberAuthorizationDiscovery{
					Value: agenttypes.MCPServerAuthorizationDiscoveryConfig{
						ReturnToEndpoint: aws.String(ConsoleURL(clients.Region)),
					},
				},
			},
		},
	}
	registration, err := registerWithRetry(ctx, clients, input, logf, endpoint)
	if err != nil && strings.Contains(err.Error(), "already exists") {
		// Setup reuses only an OAuth server at the endpoint, so this server
		// uses a token or another endpoint, or it appeared after the check.
		return fmt.Errorf(
			"an MCP server named %s is already registered on this AWS account, so setup cannot register one. "+
				"Rename or delete that server in the AWS console, then run setup again. "+
				"Or, to connect a server that is already registered, pass --mcp-service-id <id>: %w",
			MCPOAuthServerName, err,
		)
	}
	if err != nil {
		return fmt.Errorf("unable to register the LaunchDarkly MCP server: %w", err)
	}

	result.MCPServiceID = aws.ToString(registration.ServiceId)
	if result.MCPServiceID == "" {
		step, ok := registration.AdditionalStep.(*agenttypes.AdditionalServiceRegistrationStepMemberOauth)
		if !ok {
			return errors.New("AWS did not register the LaunchDarkly MCP server and sent no OAuth approval URL")
		}
		approvalURL := aws.ToString(step.Value.AuthorizationUrl)
		logf("To connect the MCP server, approve access to LaunchDarkly at:\n  %s", approvalURL)
		if opts.OpenURL != nil {
			if err := opts.OpenURL(approvalURL); err != nil {
				logf("Unable to open a browser. Open the URL yourself: %v", err)
			}
		}
		result.MCPServiceID, err = waitForOAuthServer(ctx, clients, endpoint, logf)
		if err != nil {
			return err
		}
	}
	logf("Registered MCP server %s (%s)", MCPOAuthServerName, result.MCPServiceID)

	return associateMCPServer(ctx, clients, opts, step, MCPOAuthServerName, result, logf)
}

// waitForOAuthServer looks for the OAuth server of the account until it
// appears or oauthTimeout passes.
func waitForOAuthServer(ctx context.Context, clients Clients, endpoint string, logf func(string, ...any)) (string, error) {
	logf("Waiting for you to approve access in the browser")
	deadline := time.Now().Add(oauthTimeout)
	for {
		server, err := accountMCPServer(ctx, clients, endpoint)
		if err != nil {
			return "", err
		}
		if server.ID != "" {
			return server.ID, nil
		}
		if time.Now().After(deadline) {
			return "", fmt.Errorf(
				"AWS did not register the LaunchDarkly MCP server in %s. If you did not approve access, run setup again",
				oauthTimeout,
			)
		}

		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(oauthPollInterval):
		}
	}
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

func associateMCPServer(
	ctx context.Context,
	clients Clients,
	opts SetupOptions,
	step MCPStep,
	name string,
	result *SetupResult,
	logf func(string, ...any),
) error {
	result.mcpServerName = name
	existing, err := findMCPAssociation(ctx, clients, result.AgentSpaceID, result.MCPServiceID)
	if err != nil {
		return err
	}
	if existing != nil {
		result.MCPAssociationID = aws.ToString(existing.AssociationId)
		logf("MCP server %s is already associated with the agent space", name)
		extendAllowlist(ctx, clients, opts, step, *existing, result, logf)

		return nil
	}

	associationID, err := associateMCPServerWith(ctx, clients, result.AgentSpaceID, result.MCPServiceID)
	if err != nil {
		return err
	}
	result.MCPAssociationID = associationID
	logf("Associated MCP server %s with the agent space", name)

	return nil
}

func findMCPAssociation(ctx context.Context, clients Clients, agentSpaceID, serviceID string) (*agenttypes.Association, error) {
	associations, err := listAssociations(ctx, clients, agentSpaceID)
	if err != nil {
		return nil, err
	}
	for _, association := range associations {
		if aws.ToString(association.ServiceId) == serviceID {
			return &association, nil
		}
	}

	return nil, nil
}

// missingTools returns the ldcli tools that the allowlist of an association
// misses, and whether setup can add them. AWS replaces the whole configuration
// on an update and does not send the classification of each tool back, so
// setup can rewrite the allowlist only if it holds ldcli tools alone. An
// unclassified tool counts as read-only, so a rewrite would let a mutative
// tool that the customer added run without approval.
func missingTools(association agenttypes.Association) (missing []string, canAdd bool) {
	if _, ok := association.Configuration.(*agenttypes.ServiceConfigurationMemberMcpserver); !ok {
		// Not an allowlist that setup knows how to read, so leave it.
		return nil, false
	}
	allowed := associationTools(association.Configuration)
	ldcli := slices.Concat(mcpReadOnlyTools, mcpMutativeTools)
	for _, tool := range ldcli {
		if !slices.Contains(allowed, tool) {
			missing = append(missing, tool)
		}
	}
	canAdd = !slices.ContainsFunc(allowed, func(tool string) bool { return !slices.Contains(ldcli, tool) })

	return missing, canAdd
}

// extendAllowlist adds the ldcli tools that an existing association misses,
// for example after an ldcli upgrade that allows more tools. A failure does not
// stop setup, because the server works with the tools that it has.
func extendAllowlist(
	ctx context.Context,
	clients Clients,
	opts SetupOptions,
	step MCPStep,
	association agenttypes.Association,
	result *SetupResult,
	logf func(string, ...any),
) {
	missing, canAdd := missingTools(association)
	tools := strings.Join(missing, ", ")
	switch {
	case len(missing) == 0:
		return
	case !canAdd:
		result.MCPToolsError = fmt.Sprintf("the MCP server allowlist of the agent space misses %s, and it has tools "+
			"that ldcli does not classify, so setup did not change it. Add the missing tools in the AWS console", tools)

		return
	case !slices.Equal(missing, step.MissingTools) || canAdd != step.CanAddTools:
		// The plan showed another change, so do not make this one.
		result.MCPToolsError = "the MCP server allowlist changed after the plan, so setup did not change it. Run setup again"

		return
	}
	_, err := clients.Agent.UpdateAssociation(ctx, &devopsagent.UpdateAssociationInput{
		AgentSpaceId:  association.AgentSpaceId,
		AssociationId: association.AssociationId,
		Configuration: &agenttypes.ServiceConfigurationMemberMcpserver{Value: mcpServerConfiguration()},
		Capabilities:  association.Capabilities,
	})
	if err != nil {
		result.MCPToolsError = fmt.Sprintf("unable to add %s to the MCP server allowlist: %v", strings.Join(missing, ", "), err)

		return
	}
	result.MCPToolsAdded = missing
	logf("Added %s to the MCP server allowlist", strings.Join(missing, ", "))
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

// OperatorAppURL is the browser entry point for an agent space's operator app.
func OperatorAppURL(agentSpaceID string) string {
	return fmt.Sprintf("https://%s.aidevops.global.app.aws", agentSpaceID)
}
