package onboarding

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Step represents a single step in the onboarding workflow that an AI agent should execute.
type Step struct {
	ID              string   `json:"id"`
	Title           string   `json:"title"`
	Instructions    string   `json:"instructions"`
	Tools           []string `json:"tools"`
	SuccessCriteria string   `json:"success_criteria"`
	Next            string   `json:"next"`
	OnFailure       string   `json:"on_failure"`
}

// RecoveryOption represents a ranked recovery action when a step fails.
type RecoveryOption struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Priority    int    `json:"priority"`
}

// OnboardingPlan is the top-level structure returned by the onboard command,
// providing the AI agent with a complete set of instructions.
type OnboardingPlan struct {
	Steps           []Step           `json:"steps"`
	SDKRecipes      []SDKRecipe      `json:"sdk_recipes"`
	RecoveryOptions []RecoveryOption `json:"recovery_options"`
}

// ToJSON serializes the onboarding plan to indented JSON.
func (p OnboardingPlan) ToJSON() ([]byte, error) {
	return json.MarshalIndent(p, "", "  ")
}

// Plaintext returns a human-readable summary of the onboarding plan.
func (p OnboardingPlan) Plaintext() string {
	var sb strings.Builder
	sb.WriteString("LaunchDarkly AI-Agent Onboarding Plan\n")
	sb.WriteString("=====================================\n\n")

	for i, step := range p.Steps {
		sb.WriteString(fmt.Sprintf("Step %d: %s\n", i+1, step.Title))
		sb.WriteString(fmt.Sprintf("  ID: %s\n", step.ID))
		sb.WriteString(fmt.Sprintf("  Success Criteria: %s\n", step.SuccessCriteria))
		if step.Next != "" {
			sb.WriteString(fmt.Sprintf("  Next: %s\n", step.Next))
		}
		sb.WriteString("\n")
	}

	sb.WriteString(fmt.Sprintf("SDK Recipes: %d available\n", len(p.SDKRecipes)))
	sb.WriteString(fmt.Sprintf("Recovery Options: %d defined\n\n", len(p.RecoveryOptions)))
	sb.WriteString("Run with --json for the full structured plan suitable for AI agent consumption.\n")

	return sb.String()
}

// BuildPlan constructs the full onboarding plan with all steps, SDK recipes, and recovery options.
func BuildPlan(baseURI, project, environment string) OnboardingPlan {
	return OnboardingPlan{
		Steps:           buildSteps(baseURI, project, environment),
		SDKRecipes:      AllSDKRecipes(),
		RecoveryOptions: buildRecoveryOptions(),
	}
}

func buildRecoveryOptions() []RecoveryOption {
	return []RecoveryOption{
		{
			ID:          "retry-step",
			Title:       "Retry Current Step",
			Description: "Re-attempt the current step after reviewing the error output.",
			Priority:    1,
		},
		{
			ID:          "check-credentials",
			Title:       "Verify Credentials",
			Description: "Confirm the LaunchDarkly access token is valid and has write-level access. Run: ldcli environments list --project <project>",
			Priority:    2,
		},
		{
			ID:          "check-network",
			Title:       "Check Network Connectivity",
			Description: "Verify the application can reach LaunchDarkly endpoints. Check firewall rules and proxy settings.",
			Priority:    3,
		},
		{
			ID:          "manual-install",
			Title:       "Manual SDK Install",
			Description: "If automatic dependency installation fails, provide the user with copy/paste instructions from the SDK recipe.",
			Priority:    4,
		},
		{
			ID:          "switch-sdk",
			Title:       "Try Alternative SDK",
			Description: "If detection chose the wrong SDK, re-run the detect step or allow the user to manually select an SDK.",
			Priority:    5,
		},
		{
			ID:          "skip-step",
			Title:       "Skip to Next Step",
			Description: "If a step is non-critical (e.g., auto-run failed but user can start manually), skip to the next step.",
			Priority:    6,
		},
		{
			ID:          "abort",
			Title:       "Abort Onboarding",
			Description: "Stop the onboarding workflow entirely and present the user with documentation links for manual setup.",
			Priority:    7,
		},
	}
}
