package rollouts

import (
	"encoding/json"
	stderrors "errors"
	"fmt"
	"sort"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/launchdarkly/ldcli/cmd/cliflags"
	resourcescmd "github.com/launchdarkly/ldcli/cmd/resources"
	"github.com/launchdarkly/ldcli/cmd/validators"
	"github.com/launchdarkly/ldcli/internal/errors"
	"github.com/launchdarkly/ldcli/internal/rollouts"
)

const listLongDescription = `List automated rollouts for a feature flag.

Rollouts are returned in reverse-chronological order by createdAt timestamp,
with rollout ID as the deterministic tiebreaker.

By default, the 20 most recent rollouts are returned. Use --all to fetch the
full history (subject to upstream API limits — see API-PAPERCUTS.md PC-003).

Use --output json for machine-readable output (the full envelope shape; see
schemaVersion in the response). Use plaintext output for interactive inspection,
optionally with --detailed for per-record multi-line records.

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

// runE is the list verb body. It reads the configured flags from Viper, calls Client.List,
// then marshals the result into the v1beta1 envelope. JSON output goes through
// json.MarshalIndent directly (the existing output.CmdOutput dispatcher operates on flat
// resource maps and cannot carry the typed Envelope shape).
//
// Saturation warning: when the response item count equals the requested limit (and --all
// was not set), the envelope's meta.warnings is decorated with a hint pointing at
// API-PAPERCUTS.md PC-003 (no pagination on list endpoint). The warning lives at the
// envelope-construction site (here) rather than inside `internal/rollouts/` so the
// internal package stays free of UI/envelope concerns.
func runE(client rollouts.Client) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, _ []string) error {
		accessToken := viper.GetString(cliflags.AccessTokenFlag)
		baseURI := viper.GetString(cliflags.BaseURIFlag)
		projKey := viper.GetString(cliflags.ProjectFlag)
		flagKey := viper.GetString(cliflags.FlagFlag)

		opts := rollouts.ListOpts{
			Environment: viper.GetString(cliflags.EnvironmentFlag),
			Limit:       viper.GetInt(cliflags.LimitFlag),
			All:         viper.GetBool(cliflags.AllFlag),
		}

		list, err := client.List(cmd.Context(), accessToken, baseURI, projKey, flagKey, opts)
		if err != nil {
			return emitError(cmd, err)
		}

		// Defensive deterministic sort at the command boundary. The production
		// internal/rollouts/client.go also sorts; doing it here too guarantees stable
		// output for any Client implementation (including test doubles), and keeps
		// the CLI contract (AGENT-05) independent of the upstream API path.
		if list != nil {
			sortRolloutsByRecency(list.Items)
		}

		env := rollouts.NewListEnvelope(list)

		// Saturation hint: when len(items) == opts.Limit and --all was not requested, the
		// list is likely truncated upstream (no pagination — see API-PAPERCUTS.md PC-003).
		if !opts.All && opts.Limit > 0 && list != nil && len(list.Items) >= opts.Limit {
			if env.Meta == nil {
				env.Meta = &rollouts.EnvelopeMeta{}
			}
			env.Meta.Warnings = append(env.Meta.Warnings, fmt.Sprintf(
				"List returned exactly %d items; results may be truncated upstream (see API-PAPERCUTS.md PC-003)",
				opts.Limit,
			))
		}

		detailed := viper.GetBool(cliflags.DetailedFlag)
		return emitSuccess(cmd, env, list, detailed)
	}
}

// emitSuccess writes the success envelope to stdout. JSON output emits the full envelope
// (D-07: JSON output is always the full field set, regardless of --detailed); plaintext
// output renders via the 5-col table or, when --detailed is set, the multi-record form.
func emitSuccess(cmd *cobra.Command, env rollouts.Envelope, list *rollouts.RolloutList, detailed bool) error {
	if cliflags.GetOutputKind(cmd) == "json" {
		body, err := json.MarshalIndent(env, "", "  ")
		if err != nil {
			return errors.NewErrorWrapped("failed to marshal rollouts envelope", err)
		}
		fmt.Fprintln(cmd.OutOrStdout(), string(body))
		return nil
	}

	fmt.Fprint(cmd.OutOrStdout(), RenderRolloutListPlaintext(list, detailed))
	return nil
}

// sortRolloutsByRecency sorts rollouts in place by createdAt DESC, then ID ASC as the
// deterministic tiebreaker (AGENT-05). Matches the production-path sort in
// internal/rollouts/client.go; documenting the order explicitly in help text and applying
// it at both layers ensures stable behavior regardless of which Client is in use.
func sortRolloutsByRecency(items []rollouts.Rollout) {
	sort.Slice(items, func(i, j int) bool {
		ti, tj := items[i].CreatedAt, items[j].CreatedAt
		if ti.Equal(tj) {
			return items[i].ID < items[j].ID
		}
		return ti.After(tj)
	})
}

// emitError converts an error returned from the client into either a JSON envelope (when
// --output json) or a plain message (plaintext output). In both cases the function returns a
// non-nil error so Cobra exits with code 1 (D-01: any error → exit 1; no numeric taxonomy).
//
// JSON-mode routing (AGENT-04 / D-07 — see CONTEXT.md): the error envelope is written to
// stdout, NOT stderr. Agents must branch on outcome via stdout; routing the envelope to
// stderr in JSON mode forces consumers to scrub both streams, defeating the contract. The
// returned error in JSON mode is intentionally a short sentinel ("rollouts list failed")
// rather than the full envelope JSON, so the root command's `fmt.Fprintln(os.Stderr, ...)`
// in `Execute()` does not double-emit the envelope to stderr.
//
// Plaintext output keeps the existing UX — the error message is returned verbatim and
// surfaces on stderr via the root command, since plaintext consumers are humans who read
// stderr alongside stdout.
//
// When `errors.As` finds a *rollouts.RolloutError, we use its Code / Message / NextAction;
// otherwise we fall back to ErrCodeUnknownUpstream.
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
			// Marshalling failed — fall back to surfacing the original message via the
			// standard error path (stderr).
			return errors.NewErrorWrapped(message, mErr)
		}
		// Write the envelope to stdout (AGENT-04 / D-07) and return a sentinel error so
		// Cobra exits 1 without re-emitting the envelope to stderr.
		fmt.Fprintln(cmd.OutOrStdout(), string(body))
		return errors.NewError("rollouts list failed")
	}

	return errors.NewError(message)
}
