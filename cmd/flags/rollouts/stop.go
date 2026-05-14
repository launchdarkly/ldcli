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

const stopLongDescription = `Stop an automated rollout for a feature flag.

Stops the most recent in-progress rollout on the flag in the specified environment,
rolling out to the chosen final variation:

  --to-variation <variation-uuid>   REQUIRED. The variation UUID (_id) to conclude the
                                    rollout on. Pass the original variation (control) to
                                    roll back, or the target variation (test) to roll
                                    forward. Obtain UUIDs via:
                                      ldcli flags get --flag <key> --output json | jq '.variations[]'

Before issuing the stop mutation, the CLI pre-reads the current rollout state. If the
most-recent rollout is already in a terminal state (completed or reverted), the command
refuses with error.code: "rollout_already_terminal" — the stop instruction is never sent.
If no rollouts exist, the command refuses with error.code: "no_rollouts_found".

After a successful stop, the CLI re-fetches the rollout via list to surface the
post-mutation state in the envelope's data block (API-PAPERCUTS.md PC-001 — the
stopAutomatedRelease PATCH returns the updated FeatureFlag, not the Rollout).

The JSON envelope includes meta.uiURL with a permalink to the LD UI for the affected
rollout so operators can verify the result in the browser.

THIS COMMAND IS BETA. The output schema and CLI surface may change between releases.`

// NewStopCmd builds the ` + "`flags rollouts-beta stop`" + ` verb. The RunE closure follows the
// established CONVENTIONS.md pattern: Viper is read at RunE time (NOT at constructor time);
// the typed rollouts.Client is captured at construction so tests can inject a mock.
func NewStopCmd(client rollouts.Client) *cobra.Command {
	cmd := &cobra.Command{
		Args:  validators.Validate(),
		Long:  stopLongDescription,
		RunE:  stopRunE(client),
		Short: "Stop an automated rollout for a feature flag (beta)",
		Use:   "stop",
	}

	cmd.SetUsageTemplate(resourcescmd.SubcommandUsageTemplate())
	initStopFlags(cmd)
	return cmd
}

// initStopFlags registers the flags for the `stop` verb.
//
// Required: --flag, --project, --environment, --to-variation.
//
// No optional flags (mirrors Phase 2/3 required-only convention for mutation commands).
func initStopFlags(cmd *cobra.Command) {
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

	cmd.Flags().String(cliflags.ToVariationFlag, "", cliflags.ToVariationFlagDescription)
	_ = cmd.MarkFlagRequired(cliflags.ToVariationFlag)
	_ = cmd.Flags().SetAnnotation(cliflags.ToVariationFlag, "required", []string{"true"})
	_ = viper.BindPFlag(cliflags.ToVariationFlag, cmd.Flags().Lookup(cliflags.ToVariationFlag))
}

// stopRunE is the stop verb body. It reads flag values at RunE time (NOT at constructor
// time), pre-reads the current rollout state to enforce STOP-02 (no silent mutation against
// terminal rollouts), then calls Client.Stop.
func stopRunE(client rollouts.Client) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, _ []string) error {
		// Read all flag values at RunE time per CONVENTIONS.md.
		accessToken := viper.GetString(cliflags.AccessTokenFlag)
		baseURI := viper.GetString(cliflags.BaseURIFlag)
		projKey := viper.GetString(cliflags.ProjectFlag)
		flagKey := viper.GetString(cliflags.FlagFlag)
		envKey := viper.GetString(cliflags.EnvironmentFlag)
		toVariationID := viper.GetString(cliflags.ToVariationFlag)

		// --- Pre-read guard (SC#1 / STOP-02): list the current rollout BEFORE patching ---

		list, err := client.List(cmd.Context(), accessToken, baseURI, projKey, flagKey, rollouts.ListOpts{
			Environment: envKey,
			Limit:       1,
		})
		if err != nil {
			return emitStopError(cmd, err)
		}
		if list == nil || len(list.Items) == 0 {
			return emitStopError(cmd, &rollouts.RolloutError{
				Code:       rollouts.ErrCodeNoRolloutsFound,
				Message:    fmt.Sprintf("No rollouts found for flag %q in environment %q", flagKey, envKey),
				NextAction: "Verify the flag has at least one rollout (try: ldcli flags rollouts-beta list --flag <key>); there is nothing to stop.",
			})
		}

		current := &list.Items[0]

		// Terminal-state check (SC#1): refuse to stop a rollout that has already concluded.
		// The two terminal Status.Kind buckets are "completed" and "reverted" (per
		// internal/rollouts/status_mapping.go). "paused" and "regressed" are NOT terminal —
		// they are dismissable/resumable (Plan 04-02's territory).
		if current.Status.Kind == "completed" || current.Status.Kind == "reverted" {
			return emitStopError(cmd, &rollouts.RolloutError{
				Code:    rollouts.ErrCodeAlreadyTerminal,
				Message: fmt.Sprintf("Rollout %s for flag %q in environment %q is already in state %q; cannot stop a terminal rollout.", current.ID, flagKey, envKey, current.Status.Kind),
				NextAction: "List the flag's rollouts (ldcli flags rollouts-beta list --flag <key>) to confirm the terminal state. " +
					"Start a new rollout if needed.",
			})
		}

		// --- Build the stop instruction and call the client ---

		instr := rollouts.StopInstruction{
			Kind:             "stopAutomatedRelease",
			FinalVariationID: toVariationID,
		}

		rollout, err := client.Stop(cmd.Context(), accessToken, baseURI, projKey, flagKey, envKey, instr)
		if err != nil {
			return emitStopError(cmd, err)
		}

		// --- Build success envelope with uiURL (SC#4) ---

		uiURL := rollouts.BuildUIURL(baseURI, projKey, flagKey, envKey, rollout.ID)
		env := rollouts.NewRolloutEnvelopeWithUI(rollout, uiURL)
		return emitStopSuccess(cmd, env, rollout)
	}
}

// emitStopSuccess writes the success envelope to stdout. JSON output emits the full
// envelope (D-07); plaintext output emits the concise stop confirmation.
func emitStopSuccess(cmd *cobra.Command, env rollouts.Envelope, rollout *rollouts.Rollout) error {
	if cliflags.GetOutputKind(cmd) == "json" {
		body, err := json.MarshalIndent(env, "", "  ")
		if err != nil {
			return errors.NewErrorWrapped("failed to marshal rollouts envelope", err)
		}
		fmt.Fprintln(cmd.OutOrStdout(), string(body))
		return nil
	}

	fmt.Fprint(cmd.OutOrStdout(), RenderRolloutStopPlaintext(rollout))
	return nil
}

// emitStopError converts a client error into either a JSON envelope (when --output json) or
// a plain message (plaintext output). In both cases it returns a non-nil error so Cobra exits
// with code 1 (D-01: any error → exit 1).
//
// JSON-mode routing (AGENT-04 / D-07): the error envelope is written to stdout, NOT stderr.
// The returned error is a short sentinel so the root command's Fprintln(os.Stderr, err) does
// not double-emit the envelope to stderr. Mirrors emitStartError verbatim.
func emitStopError(cmd *cobra.Command, err error) error {
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
		return errors.NewError("rollouts stop failed")
	}

	return errors.NewError(message)
}
