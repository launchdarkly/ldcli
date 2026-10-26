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

// ExperimentAgentPrompt is the system prompt of the custom agent that runs the
// experimentation skill.
//
//go:embed agents/experiment-orchestrator.md
var ExperimentAgentPrompt string

// ExperimentAgentName is the name of the custom agent. AWS keeps custom agent
// names unique in a space.
const ExperimentAgentName = "experiment-orchestrator"

// shippedAgentHashes holds the SHA-256 of each system prompt that an ldcli
// release sent. An agent with one of these prompts has no customer edits, so
// setup updates it without --replace-agent. When you change the prompt, add
// the hash of the new text. Never remove a hash.
var shippedAgentHashes = []string{
	"997dd932b26120de3ccfa19a8f0bf53c02b23d58f33a0ac9e40c188623e41f18",
}

const (
	agentAssetType = "custom_agent"
	// agentFilePath is where the console keeps the system prompt of a custom
	// agent.
	agentFilePath = ExperimentAgentName + ".md"
)

// agentTools are the IDs of the MCP tools that the agent can use. AWS names a
// tool of an MCP server "<server name>_<tool name>".
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

// spaceAgentTools are the tool IDs of agentTools that the space allows. An
// agent can use only the tools that the association of the server allows, and
// setup keeps the allowlist of an existing association. With no association
// yet, setup makes one that allows every ldcli tool.
func spaceAgentTools(ctx context.Context, clients Clients, spaceID, serviceID, serverName string) ([]string, error) {
	tools := slices.Concat(mcpReadOnlyTools, mcpMutativeTools)
	if spaceID == "" || serviceID == "" {
		return toolIDs(serverName, tools), nil
	}
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

// AgentAction is the planned custom agent action.
type AgentAction int

const (
	AgentCreate AgentAction = iota
	AgentKeep
	// AgentUpdate gives an agent with the prompt of this or an older ldcli
	// version the prompt, the tools, and the skill of this version.
	AgentUpdate
	// AgentLeave means that someone edited the prompt, so setup does not
	// change the agent without --replace-agent.
	AgentLeave
	AgentReplace
	// AgentSkip means that setup could not read the custom agents of the
	// space. The agent is optional, so the run continues without it.
	AgentSkip
)

// AgentStep is the planned custom agent step.
type AgentStep struct {
	Action  AgentAction
	AssetID string
	// Version is the asset version at plan time. A write stops if it changed.
	Version int32
	// ServerName is the name of the MCP server, which the tool IDs start with.
	ServerName string
	// Tools and Skills are what the existing agent has. An update adds the
	// ldcli tools and skill to them, so the ones that the customer added stay.
	Tools  []string
	Skills []string
	// NoFile means that the agent has no prompt file of this name.
	NoFile bool
	// Err says why the step is skipped.
	Err error
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
		return fmt.Sprintf("Leave custom agent %s unchanged because %s. Pass --replace-agent to overwrite it", label, why)
	case AgentReplace:
		return fmt.Sprintf("Overwrite custom agent %s, which is not the ldcli version because %s", label, why)
	case AgentSkip:
		return agentFailure(fmt.Errorf("Skip custom agent %s: %w", ExperimentAgentName, s.Err))
	default:
		return fmt.Sprintf("Create custom agent %s with skill %s and the MCP tools of server %s",
			ExperimentAgentName, ExperimentSkillName, s.ServerName)
	}
}

// agentFailure is the text of a failed agent step. A missing permission is the
// most likely cause, so it names the permissions that the step needs.
func agentFailure(err error) string {
	if errorCode(err) != "AccessDeniedException" {
		return err.Error()
	}

	return fmt.Sprintf("%v. The custom agent step needs aidevops:ListAssets, aidevops:GetAssetFile, "+
		"aidevops:CreateAsset, and aidevops:UpdateAsset", err)
}

func planAgent(ctx context.Context, clients Clients, opts SetupOptions, space SpaceStep, mcp MCPStep, skill SkillStep) AgentStep {
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
	switch {
	case err != nil:
		step.Action, step.Err = AgentSkip, err
	case prompt == ExperimentAgentPrompt && skill.AssetID != "" && slices.Contains(step.Skills, skill.AssetID) &&
		!slices.ContainsFunc(want, func(tool string) bool { return !slices.Contains(step.Tools, tool) }):
		step.Action = AgentKeep
	case found && (prompt == "" || slices.Contains(shippedAgentHashes, skillHash(prompt))):
		// An empty prompt has nothing to lose.
		step.Action = AgentUpdate
	case opts.ReplaceAgent:
		step.Action = AgentReplace
	default:
		step.Action = AgentLeave
	}

	return step
}

// agentMetadata reads the tools and the skills of a custom agent.
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

// union is a followed by the items of b that a does not have.
func union(a, b []string) []string {
	out := slices.Clone(a)
	for _, item := range b {
		if !slices.Contains(out, item) {
			out = append(out, item)
		}
	}

	return out
}

// checkAgent compares the space with a step that writes. A change to the
// version means that someone edited the agent after the plan.
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
	AgentCreated     = "created"
	AgentKept        = "kept"
	AgentUpdated     = "updated"
	AgentLeft        = "left"
	AgentOverwritten = "overwritten"
	AgentSkipped     = "skipped"
	AgentFailed      = "failed"
)

// installAgent does the custom agent step after the MCP server and skill
// steps, because the agent needs both. The agent is optional, so a failure does
// not stop setup. The failure goes in result.AgentError, and the caller reports
// it.
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

// errAgentNeeds means that an earlier step left the agent without its MCP
// server or its skill, so setup does not write the agent.
var errAgentNeeds = errors.New("the custom agent needs the MCP server and the skill")

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
		logf("Left custom agent %s (%s) unchanged. Pass --replace-agent to overwrite it", ExperimentAgentName, step.AssetID)

		return AgentLeft, nil
	case AgentSkip:
		return "", fmt.Errorf("setup skipped custom agent %s: %w", ExperimentAgentName, step.Err)
	}
	switch {
	case result.MCPServiceID == "":
		return "", fmt.Errorf("setup skipped custom agent %s, because no MCP server is connected: %w",
			ExperimentAgentName, errAgentNeeds)
	case result.SkillAssetID == "" || result.SkillAction == SkillSkipped:
		// A skipped skill step can keep the ID of a skill that someone
		// deleted.
		return "", fmt.Errorf("setup skipped custom agent %s, because skill %s is not ready: %w",
			ExperimentAgentName, ExperimentSkillName, errAgentNeeds)
	}
	tools, err := spaceAgentTools(ctx, clients, result.AgentSpaceID, result.MCPServiceID, step.ServerName)
	if err != nil {
		return "", err
	}
	if len(tools) == 0 {
		return "", fmt.Errorf("setup skipped custom agent %s, because the space allows none of the ldcli MCP tools: %w",
			ExperimentAgentName, errAgentNeeds)
	}

	// The other steps can take minutes, so check the agent right before the
	// write.
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

	// AWS updates only the metadata keys in the request, so the memory stores
	// that the customer attached stay.
	if _, err := clients.Agent.UpdateAsset(ctx, &devopsagent.UpdateAssetInput{
		AgentSpaceId: aws.String(result.AgentSpaceID),
		AssetId:      aws.String(step.AssetID),
		Content:      content,
		Metadata:     document.NewLazyDocument(metadata),
	}); err != nil {
		return "", fmt.Errorf("unable to update custom agent %s (%s): %w", ExperimentAgentName, step.AssetID, err)
	}
	logf("Updated custom agent %s (%s)", ExperimentAgentName, step.AssetID)
	if step.Action == AgentReplace {
		return AgentOverwritten, nil
	}

	return AgentUpdated, nil
}
