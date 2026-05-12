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

const listLongDescription = `List automated rollouts for a feature flag.

Rollouts are returned in reverse-chronological order by createdAt timestamp, with rollout ID
as the deterministic tiebreaker. Use --output json for machine-readable output (the full
envelope shape; see schemaVersion in the response). Use plaintext output for interactive
inspection.

THIS COMMAND IS BETA. The output schema and CLI surface may change between releases.`

// NewListCmd builds the `flags rollouts-beta list` verb. The RunE closure follows the
// established CONVENTIONS.md pattern: Viper is read at RunE time (NOT at constructor time);
// the typed rollouts.Client is captured at construction so tests can inject a mock.
func NewListCmd(client rollouts.Client) *cobra.Command {
	cmd := &cobra.Command{
		Args:  validators.Validate(),
		Long:  listLongDescription,
		RunE:  runE(client),
		Short: "List automated rollouts for a feature flag (beta)",
		Use:   "list",
	}

	cmd.SetUsageTemplate(resourcescmd.SubcommandUsageTemplate())
	initListFlags(cmd)
	return cmd
}

// runE is the list verb body. It reads required flags from Viper, calls Client.List, then
// marshals the result into the v1beta1 envelope. JSON output goes through json.MarshalIndent
// directly (the existing output.CmdOutput dispatcher operates on flat resource maps and
// cannot carry the typed Envelope shape).
func runE(client rollouts.Client) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, _ []string) error {
		accessToken := viper.GetString(cliflags.AccessTokenFlag)
		baseURI := viper.GetString(cliflags.BaseURIFlag)
		projKey := viper.GetString(cliflags.ProjectFlag)
		flagKey := viper.GetString(cliflags.FlagFlag)

		// Plan 01 leaves all ListOpts fields zero-valued; Plan 03 wires --environment/--limit/--all.
		opts := rollouts.ListOpts{}

		list, err := client.List(cmd.Context(), accessToken, baseURI, projKey, flagKey, opts)
		if err != nil {
			return emitError(cmd, err)
		}

		env := rollouts.NewListEnvelope(list)
		return emitSuccess(cmd, env, list)
	}
}

// emitSuccess writes the success envelope to stdout. JSON output emits the full envelope;
// plaintext output renders the RolloutList via the Phase 1 placeholder renderer.
func emitSuccess(cmd *cobra.Command, env rollouts.Envelope, list *rollouts.RolloutList) error {
	if cliflags.GetOutputKind(cmd) == "json" {
		body, err := json.MarshalIndent(env, "", "  ")
		if err != nil {
			return errors.NewErrorWrapped("failed to marshal rollouts envelope", err)
		}
		fmt.Fprintln(cmd.OutOrStdout(), string(body))
		return nil
	}

	fmt.Fprint(cmd.OutOrStdout(), RenderRolloutListPlaintext(list, false))
	return nil
}

// emitError converts an error returned from the client into either a JSON envelope (when
// --output json) or a plain message (plaintext output). In both cases the error is propagated
// back to the root command so Cobra exits with code 1 (D-01: any error → exit 1; no numeric
// taxonomy). When `errors.As` finds a *rollouts.RolloutError, we use its Code / Message /
// NextAction; otherwise we fall back to ErrCodeUnknownUpstream.
func emitError(cmd *cobra.Command, err error) error {
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
			// Fall back to the raw message rather than swallowing the original error.
			return errors.NewErrorWrapped(message, mErr)
		}
		return errors.NewError(string(body))
	}

	return errors.NewError(message)
}
