package rollouts

import (
	"encoding/json"
	stderrors "errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/launchdarkly/ldcli/cmd/cliflags"
	resourcescmd "github.com/launchdarkly/ldcli/cmd/resources"
	"github.com/launchdarkly/ldcli/cmd/validators"
	"github.com/launchdarkly/ldcli/internal/errors"
	"github.com/launchdarkly/ldcli/internal/rollouts"
)

const startLongDescription = `Start an automated rollout for a feature flag.

By default, starts a progressive rollout (traffic shifts through stages without
metric monitoring). To start a guarded rollout, supply at least one metric key via
--pause-on-regression or --revert-on-regression:

  --pause-on-regression <metricKey>   Monitor the metric; pause the rollout at the
                                      current stage when regression is detected.
                                      Requires manual dismissal to continue.
  --revert-on-regression <metricKey>  Monitor the metric; automatically revert to the
                                      original variation when regression is detected.

Both flags are repeatable. A metric appearing in both is a usage error.

Stages are expressed as a compact list: --stages 25:60m,50:60m,100:60m
  - Allocation is a whole percent integer [1-100] (e.g. 25 = 25%).
    The CLI multiplies by 1000 internally for the API's basis-points field.
    Decimals (12.5) are rejected.
  - Duration is a Go duration string with a mandatory unit suffix (60m, 1h30m, 300s).
    Plain integers (3600) are rejected — they are ambiguous between seconds and millis.

Variation flags accept UUIDs (_id) only, NOT variation keys. Obtain UUIDs via:
  ldcli flags get --flag <key> --output json | jq '.variations[]'

THIS COMMAND IS BETA. The output schema and CLI surface may change between releases.`

// NewStartCmd builds the ` + "`flags rollouts-beta start`" + ` verb. The RunE closure follows the
// established CONVENTIONS.md pattern: Viper is read at RunE time (NOT at constructor time);
// the typed rollouts.Client is captured at construction so tests can inject a mock.
func NewStartCmd(client rollouts.Client) *cobra.Command {
	cmd := &cobra.Command{
		Args:  validators.Validate(),
		Long:  startLongDescription,
		RunE:  startRunE(client),
		Short: "Start an automated rollout for a feature flag (beta)",
		Use:   "start",
	}

	cmd.SetUsageTemplate(resourcescmd.SubcommandUsageTemplate())
	initStartFlags(cmd)
	return cmd
}

// initStartFlags registers the flags for the `start` verb.
//
// Required: --flag, --project, --environment, --stages, --target-variation,
// --original-variation, --randomization-unit.
// Optional repeatable: --pause-on-regression, --revert-on-regression.
// Optional single: --rule-id.
//
// Flags explicitly NOT registered (per decisions in CONTEXT.md):
//   - --metric (D-04 dropped; use --pause-on-regression / --revert-on-regression)
//   - --release-kind (D-05 dropped; inferred from presence of metric flags)
//   - --ref, --clauses (D-07 deferred)
//   - --skip-health-checks (D-09 deferred)
//   - --idempotency-key (D-10 out of scope for entire project)
//   - --extension-duration (Q5 recommends omit for Phase 2)
//   - --comment (CONTEXT.md Claude's Discretion: omit)
func initStartFlags(cmd *cobra.Command) {
	cmd.Flags().String(cliflags.FlagFlag, "", "The feature flag key")
	_ = cmd.MarkFlagRequired(cliflags.FlagFlag)
	_ = cmd.Flags().SetAnnotation(cliflags.FlagFlag, "required", []string{"true"})
	_ = viper.BindPFlag(cliflags.FlagFlag, cmd.Flags().Lookup(cliflags.FlagFlag))

	cmd.Flags().String(cliflags.ProjectFlag, "", "The project key")
	_ = cmd.MarkFlagRequired(cliflags.ProjectFlag)
	_ = cmd.Flags().SetAnnotation(cliflags.ProjectFlag, "required", []string{"true"})
	_ = viper.BindPFlag(cliflags.ProjectFlag, cmd.Flags().Lookup(cliflags.ProjectFlag))

	cmd.Flags().String(cliflags.EnvironmentFlag, "", cliflags.EnvironmentFlagDescription)
	_ = cmd.MarkFlagRequired(cliflags.EnvironmentFlag)
	_ = cmd.Flags().SetAnnotation(cliflags.EnvironmentFlag, "required", []string{"true"})
	_ = viper.BindPFlag(cliflags.EnvironmentFlag, cmd.Flags().Lookup(cliflags.EnvironmentFlag))

	cmd.Flags().String(cliflags.StagesFlag, "", cliflags.StagesFlagDescription)
	_ = cmd.MarkFlagRequired(cliflags.StagesFlag)
	_ = cmd.Flags().SetAnnotation(cliflags.StagesFlag, "required", []string{"true"})
	_ = viper.BindPFlag(cliflags.StagesFlag, cmd.Flags().Lookup(cliflags.StagesFlag))

	cmd.Flags().String(cliflags.TargetVariationFlag, "", cliflags.TargetVariationFlagDescription)
	_ = cmd.MarkFlagRequired(cliflags.TargetVariationFlag)
	_ = cmd.Flags().SetAnnotation(cliflags.TargetVariationFlag, "required", []string{"true"})
	_ = viper.BindPFlag(cliflags.TargetVariationFlag, cmd.Flags().Lookup(cliflags.TargetVariationFlag))

	cmd.Flags().String(cliflags.OriginalVariationFlag, "", cliflags.OriginalVariationFlagDescription)
	_ = cmd.MarkFlagRequired(cliflags.OriginalVariationFlag)
	_ = cmd.Flags().SetAnnotation(cliflags.OriginalVariationFlag, "required", []string{"true"})
	_ = viper.BindPFlag(cliflags.OriginalVariationFlag, cmd.Flags().Lookup(cliflags.OriginalVariationFlag))

	cmd.Flags().String(cliflags.RandomizationUnitFlag, "", cliflags.RandomizationUnitFlagDescription)
	_ = cmd.MarkFlagRequired(cliflags.RandomizationUnitFlag)
	_ = cmd.Flags().SetAnnotation(cliflags.RandomizationUnitFlag, "required", []string{"true"})
	_ = viper.BindPFlag(cliflags.RandomizationUnitFlag, cmd.Flags().Lookup(cliflags.RandomizationUnitFlag))

	// Optional repeatable flags for metric monitoring (D-04).
	// StringArray preserves comma-containing values (unlike StringSlice which splits on commas).
	cmd.Flags().StringArray(cliflags.PauseOnRegressionFlag, nil, cliflags.PauseOnRegressionFlagDescription)
	_ = viper.BindPFlag(cliflags.PauseOnRegressionFlag, cmd.Flags().Lookup(cliflags.PauseOnRegressionFlag))

	cmd.Flags().StringArray(cliflags.RevertOnRegressionFlag, nil, cliflags.RevertOnRegressionFlagDescription)
	_ = viper.BindPFlag(cliflags.RevertOnRegressionFlag, cmd.Flags().Lookup(cliflags.RevertOnRegressionFlag))

	// Optional single: rule ID for targeting a specific existing rule (D-07).
	cmd.Flags().String(cliflags.RuleIDFlag, "", cliflags.RuleIDFlagDescription)
	_ = viper.BindPFlag(cliflags.RuleIDFlag, cmd.Flags().Lookup(cliflags.RuleIDFlag))
}

// parseStages parses the compact stages string (e.g. "25:60m,50:60m,100:60m") into a slice
// of StageInput values. Implements D-01/D-02/D-03 validation rules:
//   - Allocation must be a whole percent integer [1-100] (decimals rejected)
//   - Duration must be a Go duration string with a unit suffix (plain integers rejected)
//   - At least one stage must be specified
func parseStages(raw string) ([]rollouts.StageInput, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, fmt.Errorf("must specify at least one stage (e.g. 25:60m,50:60m,100:60m)")
	}

	parts := strings.Split(raw, ",")
	stages := make([]rollouts.StageInput, 0, len(parts))

	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		sep := strings.Index(part, ":")
		if sep < 0 {
			return nil, fmt.Errorf("malformed stage %q: expected <allocation>:<duration> (e.g. 25:60m)", part)
		}

		allocStr := part[:sep]
		durStr := part[sep+1:]

		// D-02: allocation must be a whole percent integer; strconv.Atoi rejects decimals like "12.5".
		alloc, err := strconv.Atoi(allocStr)
		if err != nil {
			return nil, fmt.Errorf("allocation %q must be a whole percent integer (e.g. 25, not 12.5)", allocStr)
		}
		if alloc < 1 || alloc > 100 {
			return nil, fmt.Errorf("allocation %d is out of range; must be in range [1, 100]", alloc)
		}

		// D-03: duration must be a Go duration string with a mandatory unit suffix.
		dur, err := time.ParseDuration(durStr)
		if err != nil {
			return nil, fmt.Errorf("duration %q is invalid: must include a unit (e.g. 60m, 1h30m, 300s)", durStr)
		}

		stages = append(stages, rollouts.StageInput{
			Allocation:     alloc * 1000, // percent → basis-points (D-02)
			DurationMillis: dur.Milliseconds(),
		})
	}

	if len(stages) == 0 {
		return nil, fmt.Errorf("must specify at least one stage (e.g. 25:60m,50:60m,100:60m)")
	}

	return stages, nil
}

// startRunE is the start verb body. It reads flag values at RunE time (NOT at constructor
// time), validates them, builds the StartInstruction, and calls Client.Start.
func startRunE(client rollouts.Client) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, _ []string) error {
		// Read all flag values at RunE time per CONVENTIONS.md.
		accessToken := viper.GetString(cliflags.AccessTokenFlag)
		baseURI := viper.GetString(cliflags.BaseURIFlag)
		projKey := viper.GetString(cliflags.ProjectFlag)
		flagKey := viper.GetString(cliflags.FlagFlag)
		envKey := viper.GetString(cliflags.EnvironmentFlag)
		stagesRaw := viper.GetString(cliflags.StagesFlag)
		targetVarID := viper.GetString(cliflags.TargetVariationFlag)
		origVarID := viper.GetString(cliflags.OriginalVariationFlag)
		randomUnit := viper.GetString(cliflags.RandomizationUnitFlag)
		// viper.GetStringSlice works for cobra StringArray flags.
		pauseMetrics := viper.GetStringSlice(cliflags.PauseOnRegressionFlag)
		revertMetrics := viper.GetStringSlice(cliflags.RevertOnRegressionFlag)
		ruleID := viper.GetString(cliflags.RuleIDFlag)

		// --- Validation (BEFORE the client call) ---

		// D-04: mutex check — a metric cannot appear in both --pause-on-regression and --revert-on-regression.
		for _, m := range pauseMetrics {
			for _, n := range revertMetrics {
				if m == n {
					return emitStartError(cmd, errors.NewError(
						fmt.Sprintf("metric %q cannot appear in both --pause-on-regression and --revert-on-regression", m),
					))
				}
			}
		}

		// Parse and validate stages string (D-01/D-02/D-03).
		stages, err := parseStages(stagesRaw)
		if err != nil {
			return emitStartError(cmd, err)
		}

		// --- Build the instruction (D-04/D-05/D-07) ---

		// D-05: infer releaseKind from presence of metric flags.
		releaseKind := "progressive"
		if len(pauseMetrics)+len(revertMetrics) > 0 {
			releaseKind = "guarded"
		}

		// D-04: build metrics and metricMonitoringPreferences in a single pass.
		// PAPERCUT: PC-010 — Metrics and MetricMonitoringPreferences are parallel collections;
		// reconciled here in a single pass per D-04.
		metrics := make([]rollouts.MetricSource, 0, len(pauseMetrics)+len(revertMetrics))
		prefs := make(map[string]rollouts.MetricMonitoringPref, len(pauseMetrics)+len(revertMetrics))

		for _, m := range pauseMetrics {
			metrics = append(metrics, rollouts.MetricSource{Key: m})
			prefs[m] = rollouts.MetricMonitoringPref{AutoRollback: false}
		}
		for _, m := range revertMetrics {
			metrics = append(metrics, rollouts.MetricSource{Key: m})
			prefs[m] = rollouts.MetricMonitoringPref{AutoRollback: true}
		}

		// For progressive rollouts, leave Metrics and MetricMonitoringPreferences at their zero
		// values — the ,omitempty JSON tags suppress them from the request body.
		if releaseKind == "progressive" {
			metrics = nil
			prefs = nil
		}

		instr := rollouts.StartInstruction{
			Kind:                        "startAutomatedRelease",
			ReleaseKind:                 releaseKind,
			OriginalVariationID:         origVarID,
			TargetVariationID:           targetVarID,
			RandomizationUnit:           randomUnit,
			Stages:                      stages,
			Metrics:                     metrics,
			MetricMonitoringPreferences: prefs,
			RuleID:                      ruleID,
		}

		// Call the client.
		rollout, err := client.Start(cmd.Context(), accessToken, baseURI, projKey, flagKey, envKey, instr)
		if err != nil {
			return emitStartError(cmd, err)
		}

		env := rollouts.NewRolloutEnvelope(rollout)
		return emitStartSuccess(cmd, env, rollout)
	}
}

// emitStartSuccess writes the success envelope to stdout. JSON output emits the full
// envelope (D-07); plaintext output emits a concise single-rollout summary.
func emitStartSuccess(cmd *cobra.Command, env rollouts.Envelope, rollout *rollouts.Rollout) error {
	if cliflags.GetOutputKind(cmd) == "json" {
		body, err := json.MarshalIndent(env, "", "  ")
		if err != nil {
			return errors.NewErrorWrapped("failed to marshal rollouts envelope", err)
		}
		fmt.Fprintln(cmd.OutOrStdout(), string(body))
		return nil
	}

	fmt.Fprint(cmd.OutOrStdout(), RenderRolloutPlaintext(rollout))
	return nil
}

// emitStartError converts a client error into either a JSON envelope (when --output json) or
// a plain message (plaintext output). In both cases it returns a non-nil error so Cobra exits
// with code 1 (D-01: any error → exit 1).
//
// JSON-mode routing (AGENT-04 / D-07): the error envelope is written to stdout, NOT stderr.
// The returned error is a short sentinel so the root command's Fprintln(os.Stderr, err) does
// not double-emit the envelope to stderr.
func emitStartError(cmd *cobra.Command, err error) error {
	code := rollouts.ErrCodeUnknownUpstream
	message := err.Error()
	nextAction := ""

	var rerr *rollouts.RolloutError
	if stderrors.As(err, &rerr) && rerr != nil {
		code = rerr.Code
		message = rerr.Message
		nextAction = rerr.NextAction
	}

	if cliflags.GetOutputKind(cmd) == "json" {
		env := rollouts.NewErrorEnvelope(code, message, nextAction)
		body, mErr := json.MarshalIndent(env, "", "  ")
		if mErr != nil {
			return errors.NewErrorWrapped(message, mErr)
		}
		// Write envelope to stdout (AGENT-04 / D-07); return short sentinel so root
		// doesn't re-emit the envelope to stderr.
		fmt.Fprintln(cmd.OutOrStdout(), string(body))
		return errors.NewError("rollouts start failed")
	}

	return errors.NewError(message)
}
