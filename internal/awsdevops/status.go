package awsdevops

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/devopsagent"
	agenttypes "github.com/aws/aws-sdk-go-v2/service/devopsagent/types"
	"github.com/aws/aws-sdk-go-v2/service/iam"
)

// AWSServiceID identifies the AWS account association.
const AWSServiceID = awsServiceID

// Status is a read-only view of what setup has provisioned so far.
type Status struct {
	AccountID    string             `json:"accountId"`
	Region       string             `json:"region"`
	Roles        []RoleStatus       `json:"roles"`
	Services     []ServiceStatus    `json:"services,omitempty"`
	AgentSpaces  []AgentSpaceStatus `json:"agentSpaces"`
	MissingSetup []string           `json:"missingSetup,omitempty"`
}

type RoleStatus struct {
	Name   string `json:"name"`
	ARN    string `json:"arn,omitempty"`
	Exists bool   `json:"exists"`
}

type ServiceStatus struct {
	ServiceID   string `json:"serviceId"`
	ServiceType string `json:"serviceType"`
	Name        string `json:"name,omitempty"`
}

type AgentSpaceStatus struct {
	AgentSpaceID   string `json:"agentSpaceId"`
	Name           string `json:"name"`
	OperatorAppURL string `json:"operatorAppUrl"`
	// OperatorAppEnabled is false if the app is off. The URL is then empty.
	OperatorAppEnabled bool `json:"operatorAppEnabled"`
	// OperatorAppError is set if status could not read the app, for example
	// without aidevops:GetOperatorApp. The state of the app is then unknown,
	// and OperatorAppEnabled is false.
	OperatorAppError string              `json:"operatorAppError,omitempty"`
	Associations     []AssociationStatus `json:"associations,omitempty"`
	// Skill is the experiment-orchestration skill, and Agent is the
	// experiment-orchestrator custom agent.
	Skill *AssetStatus `json:"skill,omitempty"`
	Agent *AgentStatus `json:"agent,omitempty"`
}

// The values of AssetStatus.State.
const (
	// AssetCurrent is the text of this ldcli version.
	AssetCurrent = "current"
	// AssetOutdated is the text of an older ldcli version, or an empty file.
	// Setup updates it.
	AssetOutdated = "outdated"
	// AssetEdited is a text that no ldcli version sent. Setup leaves it
	// without --replace-skill or --replace-agent.
	AssetEdited = "edited"
	// AssetNoFile is an asset without the file that setup writes, for example
	// a skill that someone made from a PDF. Setup leaves it too.
	AssetNoFile  = "no-file"
	AssetMissing = "missing"
)

// AssetStatus is the state of an asset that setup makes.
type AssetStatus struct {
	AssetID string `json:"assetId,omitempty"`
	State   string `json:"state,omitempty"`
	// Error is set if status could not read the asset, for example without
	// aidevops:ListAssets. State is then empty.
	Error string `json:"error,omitempty"`
}

// AgentStatus is the state of the custom agent.
type AgentStatus struct {
	AssetStatus
	Tools []string `json:"tools,omitempty"`
	// SkillAttached says whether the agent loads the experiment-orchestration
	// skill. It is nil if status could not read the agent or the skill.
	SkillAttached *bool `json:"skillAttached,omitempty"`
}

type AssociationStatus struct {
	AssociationID string `json:"associationId"`
	ServiceID     string `json:"serviceId"`
	Status        string `json:"status,omitempty"`
	// AccountID and AccountType are set for an AWS account association.
	// AccountType is "monitor" for the account of the agent space, and
	// "source" for another account that the agent space can read.
	AccountID   string `json:"accountId,omitempty"`
	AccountType string `json:"accountType,omitempty"`
	// Tools is set for an MCP server association. AWS does not validate an
	// MCP server association, so Status is empty for it.
	Tools []string `json:"tools,omitempty"`
}

// GetStatus reports the IAM roles and agent spaces in the caller's account. It
// inspects one agent space when agentSpaceID is set, and every space otherwise.
func GetStatus(ctx context.Context, clients Clients, agentSpaceID string) (Status, error) {
	accountID, err := clients.AccountID(ctx)
	if err != nil {
		return Status{}, err
	}

	status := Status{AccountID: accountID, Region: clients.Region}
	for _, name := range []string{AgentSpaceRoleName, OperatorAppRoleName} {
		role, err := clients.IAM.GetRole(ctx, &iam.GetRoleInput{RoleName: aws.String(name)})
		if err != nil {
			status.Roles = append(status.Roles, RoleStatus{Name: name})
			status.MissingSetup = append(status.MissingSetup, fmt.Sprintf("IAM role %s does not exist", name))

			continue
		}
		status.Roles = append(status.Roles, RoleStatus{
			Name:   name,
			ARN:    aws.ToString(role.Role.Arn),
			Exists: true,
		})
	}

	services, err := listServices(ctx, clients)
	if err != nil {
		return status, err
	}
	for _, service := range services {
		status.Services = append(status.Services, ServiceStatus{
			ServiceID:   aws.ToString(service.ServiceId),
			ServiceType: string(service.ServiceType),
			Name:        serviceName(service),
		})
	}

	spaceIDs := []string{agentSpaceID}
	if agentSpaceID == "" {
		spaces, err := listAgentSpaces(ctx, clients)
		if err != nil {
			return status, err
		}
		spaceIDs = nil
		for _, space := range spaces {
			spaceIDs = append(spaceIDs, aws.ToString(space.AgentSpaceId))
		}
	}

	for _, id := range spaceIDs {
		space, err := clients.Agent.GetAgentSpace(ctx, &devopsagent.GetAgentSpaceInput{AgentSpaceId: aws.String(id)})
		if err != nil {
			return status, fmt.Errorf("unable to read agent space %s: %w", id, err)
		}
		spaceStatus := AgentSpaceStatus{
			AgentSpaceID: id,
			Name:         aws.ToString(space.AgentSpace.Name),
		}
		// status is a read command. If it cannot read the app, it reports the
		// app as unknown and shows the rest of the space.
		app, err := appState(ctx, clients, id)
		switch {
		case err != nil:
			spaceStatus.OperatorAppError = errorCode(err)
		case app.Flow != "":
			spaceStatus.OperatorAppEnabled = true
			spaceStatus.OperatorAppURL = app.URL
			if spaceStatus.OperatorAppURL == "" {
				spaceStatus.OperatorAppURL = OperatorAppURL(id)
			}
		}

		associations, err := listAssociations(ctx, clients, id)
		if err != nil {
			return status, err
		}
		for _, association := range associations {
			accountID, accountType := associationAccount(association.Configuration)
			spaceStatus.Associations = append(spaceStatus.Associations, AssociationStatus{
				AssociationID: aws.ToString(association.AssociationId),
				ServiceID:     aws.ToString(association.ServiceId),
				Status:        string(association.Status),
				AccountID:     accountID,
				AccountType:   accountType,
				Tools:         associationTools(association.Configuration),
			})
		}

		spaceStatus.Skill = skillStatus(ctx, clients, id)
		spaceStatus.Agent = agentStatus(ctx, clients, id, *spaceStatus.Skill, associations)

		status.AgentSpaces = append(status.AgentSpaces, spaceStatus)
	}

	return status, nil
}

// assetState compares the text of an asset with the text of this ldcli
// version and the texts of older versions.
func assetState(text string, found bool, current string, shipped []string) string {
	switch {
	case !found:
		return AssetNoFile
	case text == current:
		return AssetCurrent
	case found && (text == "" || slices.Contains(shipped, skillHash(text))):
		return AssetOutdated
	}

	return AssetEdited
}

// skillStatus reads the skill. Like the operator app, an unreadable skill does
// not stop status.
func skillStatus(ctx context.Context, clients Clients, spaceID string) *AssetStatus {
	asset, err := findSkill(ctx, clients, spaceID, ExperimentSkillName)
	switch {
	case err != nil:
		return &AssetStatus{Error: errorCode(err)}
	case asset == nil:
		return &AssetStatus{State: AssetMissing}
	}
	status := &AssetStatus{AssetID: aws.ToString(asset.AssetId)}
	text, found, err := skillText(ctx, clients, spaceID, status.AssetID)
	switch {
	case err != nil:
		status.Error = errorCode(err)
	case !found && !stillListed(ctx, clients, spaceID, skillAssetType, ExperimentSkillName, status.AssetID):
		// AWS also sends ResourceNotFoundException for an asset that someone
		// deleted after the list, and setup then makes a new one.
		return &AssetStatus{State: AssetMissing}
	default:
		status.State = assetState(text, found, ExperimentSkill, shippedSkillHashes)
	}

	return status
}

// stillListed reports whether the asset is still in the space.
func stillListed(ctx context.Context, clients Clients, spaceID, assetType, name, assetID string) bool {
	asset, err := findAsset(ctx, clients, spaceID, assetType, name)

	return err != nil || (asset != nil && aws.ToString(asset.AssetId) == assetID)
}

// agentStatus reads the custom agent. An agent with the ldcli prompt is
// current only if it also has the skill and the ldcli tools that the space
// allows, because setup updates it otherwise.
func agentStatus(
	ctx context.Context,
	clients Clients,
	spaceID string,
	skill AssetStatus,
	associations []agenttypes.Association,
) *AgentStatus {
	asset, err := findAsset(ctx, clients, spaceID, agentAssetType, ExperimentAgentName)
	switch {
	case err != nil:
		return &AgentStatus{AssetStatus: AssetStatus{Error: errorCode(err)}}
	case asset == nil:
		return &AgentStatus{AssetStatus: AssetStatus{State: AssetMissing}}
	}
	status := &AgentStatus{AssetStatus: AssetStatus{AssetID: aws.ToString(asset.AssetId)}}
	var skills []string
	status.Tools, skills = agentMetadata(*asset)
	if skill.Error == "" {
		attached := skill.AssetID != "" && slices.Contains(skills, skill.AssetID)
		status.SkillAttached = &attached
	}
	prompt, found, err := assetText(ctx, clients, spaceID, status.AssetID, agentFilePath)
	if err != nil {
		status.Error = errorCode(err)

		return status
	}
	status.State = assetState(prompt, found, ExperimentAgentPrompt, shippedAgentHashes)
	if status.State == AssetCurrent && (status.SkillAttached == nil || !*status.SkillAttached ||
		missingAgentTools(status.Tools, associations)) {
		status.State = AssetOutdated
	}

	return status
}

// missingAgentTools reports whether the agent lacks an ldcli tool that an MCP
// server association of the space allows. status does not know which server is
// the LaunchDarkly one, so it matches the tool by the part after the server
// name.
func missingAgentTools(tools []string, associations []agenttypes.Association) bool {
	for _, association := range associations {
		for _, allowed := range associationTools(association.Configuration) {
			if !slices.Contains(slices.Concat(mcpReadOnlyTools, mcpMutativeTools), allowed) {
				continue
			}
			if !slices.ContainsFunc(tools, func(tool string) bool { return strings.HasSuffix(tool, "_"+allowed) }) {
				return true
			}
		}
	}

	return false
}

// associationAccount returns the AWS account of an association. Each AWS
// account association has the service ID "aws", so only the configuration
// tells the accounts apart.
func associationAccount(configuration agenttypes.ServiceConfiguration) (accountID, accountType string) {
	switch c := configuration.(type) {
	case *agenttypes.ServiceConfigurationMemberAws:
		return aws.ToString(c.Value.AccountId), string(c.Value.AccountType)
	case *agenttypes.ServiceConfigurationMemberSourceAws:
		return aws.ToString(c.Value.AccountId), string(c.Value.AccountType)
	}

	return "", ""
}

// associationTools returns the MCP tools of an MCP server association, such
// as the LaunchDarkly MCP server.
func associationTools(configuration agenttypes.ServiceConfiguration) []string {
	if c, ok := configuration.(*agenttypes.ServiceConfigurationMemberMcpserver); ok {
		return c.Value.Tools
	}

	return nil
}

// errorCode gives the AWS error code, for example AccessDeniedException, or
// the error text if the error has no code.
func errorCode(err error) string {
	var api interface{ ErrorCode() string }
	if errors.As(err, &api) && api.ErrorCode() != "" {
		return api.ErrorCode()
	}

	return err.Error()
}
