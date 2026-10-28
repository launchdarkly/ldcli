package awsdevops

import (
	"context"
	"time"
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

// MCPTools lists every tool in the allowlist, for tests.
func MCPTools() []string {
	return append(append([]string{}, mcpReadOnlyTools...), mcpMutativeTools...)
}

// SetOAuthPollInterval changes how often setup looks for the OAuth server,
// for tests. It returns a function that restores the old interval.
func SetOAuthPollInterval(d time.Duration) func() {
	old := oauthPollInterval
	oauthPollInterval = d

	return func() { oauthPollInterval = old }
}
