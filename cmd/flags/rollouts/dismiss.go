package rollouts

import (
	"encoding/json"
	stderrors "errors"
	"fmt"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/launchdarkly/ldcli/cmd/cliflags"
	resourcescmd "github.com/launchdarkly/ldcli/cmd/resources"
	"github.com/launchdarkly/ldcli/cmd/validators"
	"github.com/launchdarkly/ldcli/internal/errors"
	"github.com/launchdarkly/ldcli/internal/rollouts"
)

const dismissLongDescription = `Dismiss an active regression on an automated rollout so it can resume.

Pre-reads the most recent rollout's state for the flag in the specified environment.
Refuses with error.code: "no_active_regression" if the rollout's Status.Kind is NOT
"regressed" — there is nothing to dismiss in states like "active", "paused", etc.
Refuses with error.code: "no_rollouts_found" when no rollouts exist for the flag.

When the pre-read confirms an active regression, the CLI sends a dismissRegression
semantic-patch instruction to LaunchDarkly (the upstream returns 204 No Content —
see API-PAPERCUTS.md PC-007). The CLI then re-fetches the post-dismiss state with
bounded backoff (1s, 3s, 5s — ~9s total budget) until Status.Kind transitions out
of "regressed" or the budget is exhausted.

On success the CLI returns the post-dismiss Rollout in the rollouts.v1beta1 envelope
with meta.uiURL pointing at the LaunchDarkly UI. When the polling budget is exhausted
before the regressed state clears, the CLI still returns success (the PATCH succeeded)
but populates meta.warnings with an eventual-consistency notice referencing PC-007.

AGENT-04 note: up to 4 HTTP calls are issued per invocation (1 List pre-read + 1 PATCH
+ up to 3 Get polls). The CLI is context-cancellation aware — SIGINT cancels mid-poll.

THIS COMMAND IS BETA. The output schema and CLI surface may change between releases.`

// NewDismissCmd builds the ` + "`flags rollouts-beta dismiss-regression`" + ` verb. The RunE closure follows
// the established CONVENTIONS.md pattern: Viper is read at RunE time (NOT at constructor
// time); the typed rollouts.Client is captured at construction so tests can inject a mock.
func NewDismissCmd(client rollouts.Client) *cobra.Command {
	cmd := &cobra.Command{
		Args:  validators.Validate(),
		Long:  dismissLongDescription,
		RunE:  dismissRunE(client),
		Short: "Dismiss an active regression so the rollout can resume (beta)",
		Use:   "dismiss-regression",
	}

	cmd.SetUsageTemplate(resourcescmd.SubcommandUsageTemplate())
	initDismissFlags(cmd)
	return cmd
}

// initDismissFlags registers the flags for the `dismiss-regression` verb.
//
// Required: --flag, --project, --environment.
//
// No --to-variation (stop-only) and no --rollout-id (out of scope per prototype framing).
func initDismissFlags(cmd *cobra.Command) {
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
}

// dismissRunE is the dismiss-regression verb body. It reads flag values at RunE time
// (NOT at constructor time), pre-reads the current rollout state to enforce the
// no-active-regression check (STOP-04 / SC#3), then calls Client.DismissRegression.
func dismissRunE(client rollouts.Client) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, _ []string) error {
		// Read all flag values at RunE time per CONVENTIONS.md.
		accessToken := viper.GetString(cliflags.AccessTokenFlag)
		baseURI := viper.GetString(cliflags.BaseURIFlag)
		projKey := viper.GetString(cliflags.ProjectFlag)
		flagKey := viper.GetString(cliflags.FlagFlag)
		envKey := viper.GetString(cliflags.EnvironmentFlag)

		// --- Pre-read guard (SC#3 / STOP-04): list the current rollout BEFORE patching ---

		list, err := client.List(cmd.Context(), accessToken, baseURI, projKey, flagKey, rollouts.ListOpts{
			Environment: envKey,
			Limit:       1,
		})
		if err != nil {
			return emitDismissError(cmd, err)
		}
		if list == nil || len(list.Items) == 0 {
			return emitDismissError(cmd, &rollouts.RolloutError{
				Code:       rollouts.ErrCodeNoRolloutsFound,
				Message:    fmt.Sprintf("No rollouts found for flag %q in environment %q", flagKey, envKey),
				NextAction: "Verify the flag has at least one rollout (try: ldcli flags rollouts-beta list --flag <key>); there is no regression to dismiss.",
			})
		}

		current := &list.Items[0]

		// No-active-regression check (SC#3): refuse to dismiss when the rollout is not regressed.
		// The "regressed" Status.Kind bucket is the only state where a dismissal is meaningful;
		// all other states (active, paused, completed, reverted) do not need dismissal.
		//
		// PAPERCUT: PC-021 — Phase 4 real-staging smoke (04-SMOKE.md Smoke D + history sweep)
		// established that upstream never emits Status.Kind=="regressed"; regression info is
		// encoded in status.label with Kind=="paused". The check below therefore rejects every
		// real regression on staging. The production CLI build should reshape this gate to
		// scan events[] for a regression_detected event without a subsequent dismissal (see
		// CLI-LEARNINGS.md CL-013 for production CLI guidance).
		if current.Status.Kind != "regressed" {
			return emitDismissError(cmd, &rollouts.RolloutError{
				Code:    rollouts.ErrCodeNoActiveRegression,
				Message: fmt.Sprintf("Rollout %s for flag %q in environment %q is in state %q; there is no active regression to dismiss.", current.ID, flagKey, envKey, current.Status.Kind),
				NextAction: "Run `ldcli flags rollouts-beta status --flag <key>` to inspect the current state. " +
					"Dismissal is only meaningful when the rollout is in 'regressed' state.",
			})
		}

		// --- Build instruction and call client.DismissRegression ---

		instr := rollouts.DismissRegressionInstruction{Kind: "dismissRegression"}

		rollout, warnings, err := client.DismissRegression(cmd.Context(), accessToken, baseURI, projKey, flagKey, envKey, instr)
		if err != nil {
			return emitDismissError(cmd, err)
		}

		// --- Build success envelope with uiURL (SC#4) and optional warnings ---

		uiURL := rollouts.BuildUIURL(baseURI, projKey, flagKey, envKey, rollout.ID)
		env := rollouts.NewRolloutEnvelopeWithUI(rollout, uiURL)
		// NewRolloutEnvelopeWithUI already populates Meta; append client-returned warnings.
		if len(warnings) > 0 {
			if env.Meta == nil {
				env.Meta = &rollouts.EnvelopeMeta{}
			}
			env.Meta.Warnings = append(env.Meta.Warnings, warnings...)
		}

		return emitDismissSuccess(cmd, env, rollout, warnings)
	}
}

// emitDismissSuccess writes the success envelope to stdout. JSON output emits the full
// envelope (D-07); plaintext output emits the concise dismiss confirmation. Warnings are
// routed to stderr in plaintext mode so JSON consumers see them only in meta.warnings.
func emitDismissSuccess(cmd *cobra.Command, env rollouts.Envelope, rollout *rollouts.Rollout, warnings []string) error {
	if cliflags.GetOutputKind(cmd) == "json" {
		body, err := json.MarshalIndent(env, "", "  ")
		if err != nil {
			return errors.NewErrorWrapped("failed to marshal rollouts envelope", err)
		}
		fmt.Fprintln(cmd.OutOrStdout(), string(body))
		return nil
	}

	fmt.Fprint(cmd.OutOrStdout(), RenderRolloutDismissPlaintext(rollout))
	for _, w := range warnings {
		fmt.Fprintf(cmd.ErrOrStderr(), "⚠ %s\n", w)
	}
	return nil
}

// emitDismissError converts a client error into either a JSON envelope (when --output json)
// or a plain message (plaintext output). In both cases it returns a non-nil error so Cobra
// exits with code 1 (D-01: any error → exit 1).
//
// JSON-mode routing (AGENT-04 / D-07): the error envelope is written to stdout, NOT stderr.
// The returned error is a short sentinel so the root command's Fprintln(os.Stderr, err) does
// not double-emit the envelope to stderr. Mirrors emitStopError verbatim.
func emitDismissError(cmd *cobra.Command, err error) error {
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
		return errors.NewError("rollouts dismiss-regression failed")
	}

	return errors.NewError(message)
}
