package awsdevops

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/devopsagent"
	"github.com/aws/aws-sdk-go-v2/service/devopsagent/document"
	agenttypes "github.com/aws/aws-sdk-go-v2/service/devopsagent/types"
)

//go:embed agents/experiment-orchestrator.md
var ExperimentAgentPrompt string

const ExperimentAgentName = "experiment-orchestrator"

// shippedAgentHashes are the prompts that ldcli releases sent. When you change
// the prompt, add its hash. Never remove a hash.
var shippedAgentHashes = []string{
	"d3d2124c29e290df9cadd38e9bae2619ac8622cabb180fa0ad4fc59d5358dee5",
}

const agentResetHint = "To use the ldcli version, delete the agent in the AWS console and run setup again"

const (
	agentAssetType = "custom_agent"
	agentFilePath  = ExperimentAgentName + ".md"
)

// AWS names a tool of an MCP server "<server name>_<tool name>".
func agentTools(serverName string) []string {
	return toolIDs(serverName, slices.Concat(mcpReadOnlyTools, mcpMutativeTools))
}

func toolIDs(serverName string, tools []string) []string {
	ids := make([]string, 0, len(tools))
	for _, tool := range tools {
		ids = append(ids, serverName+"_"+tool)
	}

	return ids
}

// spaceAgentTools are the ldcli tools that the association of the server
// allows, because an agent can use no others.
func spaceAgentTools(ctx context.Context, clients Clients, spaceID, serviceID, serverName string) ([]string, error) {
	if spaceID == "" || serviceID == "" {
		return agentTools(serverName), nil
	}
	tools := slices.Concat(mcpReadOnlyTools, mcpMutativeTools)
	associations, err := listAssociations(ctx, clients, spaceID)
	if err != nil {
		return nil, err
	}
	for _, association := range associations {
		if aws.ToString(association.ServiceId) != serviceID {
			continue
		}
		config, ok := association.Configuration.(*agenttypes.ServiceConfigurationMemberMcpserver)
		if !ok {
			break
		}
		tools = slices.DeleteFunc(tools, func(tool string) bool { return !slices.Contains(config.Value.Tools, tool) })
	}

	return toolIDs(serverName, tools), nil
}

type AgentAction int

const (
	AgentCreate AgentAction = iota
	AgentKeep
	AgentUpdate
	AgentLeave
	AgentSkip
)

type AgentStep struct {
	Action  AgentAction
	AssetID string
	// Version is the asset version at plan time. A write stops if it changed.
	Version    int32
	ServerName string
	// Tools and Skills are what the existing agent has.
	Tools  []string
	Skills []string
	NoFile bool
	Err    error
}

func (s AgentStep) describe() string {
	label := fmt.Sprintf("%s (%s)", ExperimentAgentName, s.AssetID)
	why := "someone edited its prompt"
	if s.NoFile {
		why = "it has no " + agentFilePath
	}
	switch s.Action {
	case AgentKeep:
		return "Keep custom agent " + label
	case AgentUpdate:
		return fmt.Sprintf("Update custom agent %s to the prompt and tools of this ldcli version", label)
	case AgentLeave:
		return fmt.Sprintf("Leave custom agent %s unchanged because %s. %s", label, why, agentResetHint)
	case AgentSkip:
		return agentFailure(fmt.Errorf("Skip custom agent %s: %w", ExperimentAgentName, s.Err))
	default:
		return fmt.Sprintf("Create custom agent %s with skill %s and the MCP tools of server %s",
			ExperimentAgentName, ExperimentSkillName, s.ServerName)
	}
}

func agentFailure(err error) string {
	if errorCode(err) != "AccessDeniedException" {
		return err.Error()
	}

	return fmt.Sprintf("%v. The custom agent step needs aidevops:ListAssets, aidevops:GetAssetFile, "+
		"aidevops:CreateAsset, aidevops:UpdateAsset, and aidevops:ListAssociations", err)
}

func planAgent(ctx context.Context, clients Clients, space SpaceStep, mcp MCPStep, skill SkillStep) AgentStep {
	step := AgentStep{ServerName: mcp.name()}
	if space.Action == SpaceCreate {
		return step
	}
	asset, err := findAsset(ctx, clients, space.ID, agentAssetType, ExperimentAgentName)
	if err != nil {
		step.Action, step.Err = AgentSkip, err
		return step
	}
	if asset == nil {
		return step
	}
	step.AssetID, step.Version = aws.ToString(asset.AssetId), aws.ToInt32(asset.Version)
	step.Tools, step.Skills = agentMetadata(*asset)
	prompt, found, err := assetText(ctx, clients, space.ID, step.AssetID, agentFilePath)
	step.NoFile = !found
	if err != nil {
		step.Action, step.Err = AgentSkip, err
		return step
	}
	want, err := spaceAgentTools(ctx, clients, space.ID, mcp.ServiceID, step.ServerName)
	if mcp.CanAddTools {
		// The MCP step adds these tools to the allowlist before the agent step.
		want = union(want, toolIDs(step.ServerName, mcp.MissingTools))
	}
	switch {
	case err != nil:
		step.Action, step.Err = AgentSkip, err
	case prompt == ExperimentAgentPrompt && skill.AssetID != "" && slices.Contains(step.Skills, skill.AssetID) &&
		!slices.ContainsFunc(want, func(tool string) bool { return !slices.Contains(step.Tools, tool) }):
		step.Action = AgentKeep
	case found && (prompt == "" || slices.Contains(shippedAgentHashes, skillHash(prompt))):
		// An empty prompt has nothing to lose.
		step.Action = AgentUpdate
	default:
		step.Action = AgentLeave
	}

	return step
}

func agentMetadata(asset agenttypes.Asset) (tools, skills []string) {
	if asset.Metadata == nil {
		return nil, nil
	}
	encoded, err := asset.Metadata.MarshalSmithyDocument()
	if err != nil {
		return nil, nil
	}
	var metadata struct {
		Tools  []string `json:"tools"`
		Skills []string `json:"skills"`
	}
	if err := json.Unmarshal(encoded, &metadata); err != nil {
		return nil, nil
	}

	return metadata.Tools, metadata.Skills
}

func union(a, b []string) []string {
	out := slices.Clone(a)
	for _, item := range b {
		if !slices.Contains(out, item) {
			out = append(out, item)
		}
	}

	return out
}

func checkAgent(ctx context.Context, clients Clients, spaceID string, step AgentStep) error {
	asset, err := findAsset(ctx, clients, spaceID, agentAssetType, ExperimentAgentName)
	switch {
	case err != nil:
		return err
	case step.Action == AgentCreate && asset != nil:
		return planChanged("custom agent %s appeared after the plan", ExperimentAgentName)
	case step.Action == AgentCreate:
		return nil
	case asset == nil || aws.ToString(asset.AssetId) != step.AssetID:
		return planChanged("custom agent %s disappeared after the plan", ExperimentAgentName)
	case aws.ToInt32(asset.Version) != step.Version:
		return planChanged("custom agent %s changed after the plan", ExperimentAgentName)
	}

	return nil
}

// The values of SetupResult.AgentAction.
const (
	AgentCreated = "created"
	AgentKept    = "kept"
	AgentUpdated = "updated"
	AgentLeft    = "left"
	AgentSkipped = "skipped"
	AgentFailed  = "failed"
)

// installAgent runs after the MCP server and skill steps, because the agent
// needs both. A failure does not stop setup.
func installAgent(
	ctx context.Context,
	clients Clients,
	step AgentStep,
	result *SetupResult,
	logf func(string, ...any),
) {
	result.AgentAssetID = step.AssetID
	action, err := doAgentStep(ctx, clients, step, result, logf)
	result.AgentAction = action
	if err != nil {
		result.AgentAction = AgentFailed
		if step.Action == AgentSkip || errors.Is(err, ErrPlanChanged) || errors.Is(err, errAgentNeeds) {
			result.AgentAction = AgentSkipped
		}
		result.AgentError = agentFailure(err)
	}
}

var errAgentNeeds = errors.New("the custom agent needs the skill")

func doAgentStep(
	ctx context.Context,
	clients Clients,
	step AgentStep,
	result *SetupResult,
	logf func(string, ...any),
) (string, error) {
	switch step.Action {
	case AgentKeep:
		logf("Kept custom agent %s (%s)", ExperimentAgentName, step.AssetID)

		return AgentKept, nil
	case AgentLeave:
		logf("Left custom agent %s (%s) unchanged. %s", ExperimentAgentName, step.AssetID, agentResetHint)

		return AgentLeft, nil
	case AgentSkip:
		return "", fmt.Errorf("setup skipped custom agent %s: %w", ExperimentAgentName, step.Err)
	}
	if result.SkillAssetID == "" || result.SkillAction == SkillSkipped {
		return "", fmt.Errorf("setup skipped custom agent %s, because skill %s is not ready: %w",
			ExperimentAgentName, ExperimentSkillName, errAgentNeeds)
	}
	tools, err := spaceAgentTools(ctx, clients, result.AgentSpaceID, result.MCPServiceID, result.mcpServerName)
	if err != nil {
		return "", err
	}
	if len(tools) == 0 {
		return "", fmt.Errorf("setup skipped custom agent %s, because the space allows none of the ldcli MCP tools: %w",
			ExperimentAgentName, errAgentNeeds)
	}

	if err := checkAgent(ctx, clients, result.AgentSpaceID, step); err != nil {
		return "", err
	}
	content := &agenttypes.AssetContentMemberFile{Value: agenttypes.AssetFileContent{
		Path: aws.String(agentFilePath),
		Body: &agenttypes.AssetFileBodyMemberText{Value: ExperimentAgentPrompt},
	}}
	skills := []string{result.SkillAssetID}
	if step.Action == AgentUpdate {
		tools, skills = union(step.Tools, tools), union(step.Skills, skills)
	}
	metadata := map[string]any{"tools": tools, "skills": skills}
	if step.Action == AgentCreate {
		metadata["name"] = ExperimentAgentName
		created, err := clients.Agent.CreateAsset(ctx, &devopsagent.CreateAssetInput{
			AgentSpaceId: aws.String(result.AgentSpaceID),
			AssetType:    aws.String(agentAssetType),
			Content:      content,
			Metadata:     document.NewLazyDocument(metadata),
		})
		if err != nil {
			return "", fmt.Errorf("unable to create custom agent %s: %w", ExperimentAgentName, err)
		}
		result.AgentAssetID = aws.ToString(created.Asset.AssetId)
		logf("Created custom agent %s (%s)", ExperimentAgentName, result.AgentAssetID)

		return AgentCreated, nil
	}

	// AWS keeps the metadata keys that the request leaves out.
	if _, err := clients.Agent.UpdateAsset(ctx, &devopsagent.UpdateAssetInput{
		AgentSpaceId: aws.String(result.AgentSpaceID),
		AssetId:      aws.String(step.AssetID),
		Content:      content,
		Metadata:     document.NewLazyDocument(metadata),
	}); err != nil {
		return "", fmt.Errorf("unable to update custom agent %s (%s): %w", ExperimentAgentName, step.AssetID, err)
	}
	logf("Updated custom agent %s (%s)", ExperimentAgentName, step.AssetID)
	return AgentUpdated, nil
}
