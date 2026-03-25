package onboarding_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/launchdarkly/ldcli/internal/onboarding"
)

func TestBuildPlan(t *testing.T) {
	plan := onboarding.BuildPlan("https://app.launchdarkly.com", "default", "test")

	t.Run("has all required steps", func(t *testing.T) {
		require.Len(t, plan.Steps, 7)

		expectedIDs := []string{"detect", "plan", "apply", "run", "validate", "first-flag", "recover"}
		for i, step := range plan.Steps {
			assert.Equal(t, expectedIDs[i], step.ID)
		}
	})

	t.Run("steps have non-empty fields", func(t *testing.T) {
		for _, step := range plan.Steps {
			assert.NotEmpty(t, step.ID, "step ID should not be empty")
			assert.NotEmpty(t, step.Title, "step Title should not be empty")
			assert.NotEmpty(t, step.Instructions, "step Instructions should not be empty")
			assert.NotEmpty(t, step.Tools, "step Tools should not be empty")
			assert.NotEmpty(t, step.SuccessCriteria, "step SuccessCriteria should not be empty")
		}
	})

	t.Run("step flow is connected", func(t *testing.T) {
		assert.Equal(t, "plan", plan.Steps[0].Next)
		assert.Equal(t, "apply", plan.Steps[1].Next)
		assert.Equal(t, "run", plan.Steps[2].Next)
		assert.Equal(t, "validate", plan.Steps[3].Next)
		assert.Equal(t, "first-flag", plan.Steps[4].Next)
		assert.Empty(t, plan.Steps[5].Next) // first-flag is terminal
		assert.Empty(t, plan.Steps[6].Next) // recover is terminal
	})

	t.Run("non-terminal steps have on_failure set to recover", func(t *testing.T) {
		for _, step := range plan.Steps[:6] {
			assert.Equal(t, "recover", step.OnFailure, "step %s should have on_failure=recover", step.ID)
		}
	})

	t.Run("has SDK recipes", func(t *testing.T) {
		assert.Greater(t, len(plan.SDKRecipes), 0)
	})

	t.Run("has recovery options", func(t *testing.T) {
		assert.Greater(t, len(plan.RecoveryOptions), 0)
	})

	t.Run("validate step includes project and environment", func(t *testing.T) {
		validateStep := plan.Steps[4]
		assert.Contains(t, validateStep.Instructions, "default")
		assert.Contains(t, validateStep.Instructions, "test")
		assert.Contains(t, validateStep.Instructions, "https://app.launchdarkly.com")
	})
}

func TestToJSON(t *testing.T) {
	plan := onboarding.BuildPlan("https://app.launchdarkly.com", "default", "test")

	data, err := plan.ToJSON()
	require.NoError(t, err)

	t.Run("produces valid JSON", func(t *testing.T) {
		var parsed onboarding.OnboardingPlan
		err := json.Unmarshal(data, &parsed)
		require.NoError(t, err)
		assert.Len(t, parsed.Steps, 7)
		assert.Greater(t, len(parsed.SDKRecipes), 0)
		assert.Greater(t, len(parsed.RecoveryOptions), 0)
	})
}

func TestPlaintext(t *testing.T) {
	plan := onboarding.BuildPlan("https://app.launchdarkly.com", "default", "test")
	text := plan.Plaintext()

	t.Run("contains header", func(t *testing.T) {
		assert.Contains(t, text, "LaunchDarkly AI-Agent Onboarding Plan")
	})

	t.Run("contains all step titles", func(t *testing.T) {
		assert.Contains(t, text, "Detect Repository Stack")
		assert.Contains(t, text, "Generate Integration Plan")
		assert.Contains(t, text, "Install Dependencies and Apply Code Changes")
		assert.Contains(t, text, "Start the Application")
		assert.Contains(t, text, "Validate SDK Connection")
		assert.Contains(t, text, "Create Your First Feature Flag")
		assert.Contains(t, text, "Recovery: Diagnose and Resume")
	})

	t.Run("contains JSON hint", func(t *testing.T) {
		assert.Contains(t, text, "--json")
	})
}

func TestAllSDKRecipes(t *testing.T) {
	recipes := onboarding.AllSDKRecipes()

	t.Run("has recipes for major languages", func(t *testing.T) {
		sdkIDs := make(map[string]bool)
		for _, r := range recipes {
			sdkIDs[r.SDKID] = true
		}

		assert.True(t, sdkIDs["node-server"], "should have Node server recipe")
		assert.True(t, sdkIDs["python-server-sdk"], "should have Python recipe")
		assert.True(t, sdkIDs["go-server-sdk"], "should have Go recipe")
		assert.True(t, sdkIDs["java-server-sdk"], "should have Java recipe")
		assert.True(t, sdkIDs["react-client-sdk"], "should have React recipe")
	})

	t.Run("all recipes have required fields", func(t *testing.T) {
		for _, r := range recipes {
			assert.NotEmpty(t, r.SDKID, "recipe SDKID should not be empty")
			assert.NotEmpty(t, r.DisplayName, "recipe DisplayName should not be empty")
			assert.NotEmpty(t, r.SDKType, "recipe SDKType should not be empty")
			assert.NotEmpty(t, r.DetectFiles, "recipe DetectFiles should not be empty")
			assert.NotEmpty(t, r.InstallCmd, "recipe InstallCmd should not be empty")
			assert.NotEmpty(t, r.ImportSnippet, "recipe ImportSnippet should not be empty")
			assert.NotEmpty(t, r.InitSnippet, "recipe InitSnippet should not be empty")
		}
	})

	t.Run("SDK types are valid", func(t *testing.T) {
		validTypes := map[string]bool{"server": true, "client": true, "mobile": true}
		for _, r := range recipes {
			assert.True(t, validTypes[r.SDKType], "recipe %s has invalid type %s", r.SDKID, r.SDKType)
		}
	})
}
