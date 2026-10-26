package awsdevops

import (
	"context"
	"slices"
)

// Setup plans a full setup and runs it, for tests.
func Setup(ctx context.Context, clients Clients, opts SetupOptions) (SetupResult, error) {
	plan, err := PlanSetup(ctx, clients, opts, true)
	if err != nil {
		return SetupResult{}, err
	}

	return RunPlan(ctx, clients, opts, plan)
}

// ConnectExisting plans a connect run and runs it, for tests.
func ConnectExisting(ctx context.Context, clients Clients, opts SetupOptions) (SetupResult, error) {
	plan, err := PlanSetup(ctx, clients, opts, false)
	if err != nil {
		return SetupResult{}, err
	}

	return ConnectPlan(ctx, clients, opts, plan)
}

// ShipSkill adds text to the shipped skill versions until the test ends.
func ShipSkill(text string) func() {
	saved := shippedSkillHashes
	shippedSkillHashes = append(append([]string(nil), saved...), skillHash(text))

	return func() { shippedSkillHashes = saved }
}

// ShippedSkillHashes are the hashes of the shipped skill versions.
func ShippedSkillHashes() []string { return shippedSkillHashes }

// SkillHash is the hash that setup compares.
func SkillHash(text string) string { return skillHash(text) }

// AllowedTools are the tools that setup allows on the MCP server.
func AllowedTools() []string { return slices.Concat(mcpReadOnlyTools, mcpMutativeTools) }

// ShippedAgentHashes are the hashes of the shipped custom agent prompts.
func ShippedAgentHashes() []string { return shippedAgentHashes }

// AgentTools are the tool IDs that setup gives the custom agent.
func AgentTools(serverName string) []string { return agentTools(serverName) }
