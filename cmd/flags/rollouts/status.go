package rollouts

import (
	"context"
	"encoding/json"
	stderrors "errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"golang.org/x/sync/errgroup"

	"github.com/launchdarkly/ldcli/cmd/cliflags"
	resourcescmd "github.com/launchdarkly/ldcli/cmd/resources"
	"github.com/launchdarkly/ldcli/cmd/validators"
	"github.com/launchdarkly/ldcli/internal/errors"
	"github.com/launchdarkly/ldcli/internal/rollouts"
)

const statusLongDescription = `Show automated rollout status for a feature flag.

By default, returns the most-recent rollout on the flag — "most recent" means
the rollout with the highest createdAt timestamp (createdAt DESC; ID ASC tiebreaker,
matching the list verb's ordering per AGENT-05). Pass --rollout-id <id> to inspect
a specific rollout by UUID.

When --rollout-id is provided, --environment is also required. This is a surface of
API-PAPERCUTS.md PC-004: the upstream GET-by-ID endpoint requires environmentKey in
the URL path despite the rollout ID being globally unique. The CLI surfaces the
requirement explicitly rather than auto-resolving env via a list-and-filter call,
because the API gap is one of the learnings this prototype is meant to surface.

Output is full single-rollout detail by default — there is no --detailed / --short
toggle. JSON output emits the existing rollouts.v1beta1 envelope with kind="Rollout";
plaintext output emits sectioned blocks (Overview / Stages / Metrics / Events).

THIS COMMAND IS BETA. The output schema and CLI surface may change between releases.`

// NewStatusCmd builds the `flags rollouts-beta status` verb. The RunE closure follows
// the established CONVENTIONS.md pattern: Viper is read at RunE time (NOT at constructor
// time); the typed rollouts.Client is captured at construction so tests can inject a mock.
func NewStatusCmd(client rollouts.Client) *cobra.Command {
	cmd := &cobra.Command{
		Args:  validators.Validate(),
		Long:  statusLongDescription,
		RunE:  statusRunE(client),
		Short: "Show automated rollout status for a feature flag (beta)",
		Use:   "status",
	}

	cmd.SetUsageTemplate(resourcescmd.SubcommandUsageTemplate())
	initStatusFlags(cmd)
	return cmd
}

// initStatusFlags registers the flags for the `status` verb (D-02 surface).
//
// Required: --flag, --project (inherited shape from list / start).
// Optional: --environment, --rollout-id.
//
// Flags explicitly NOT registered (per D-02 / D-08 / D-01):
//   - --detailed / --short  (D-08; status is single-rollout full detail by default)
//   - --state / --limit     (D-02; status surface is the documented four flags only)
//   - --watch / poll flags  (D-01; --watch removed from project)
func initStatusFlags(cmd *cobra.Command) {
	cmd.Flags().String(cliflags.FlagFlag, "", "The feature flag key")
	_ = cmd.MarkFlagRequired(cliflags.FlagFlag)
	_ = cmd.Flags().SetAnnotation(cliflags.FlagFlag, "required", []string{"true"})
	_ = viper.BindPFlag(cliflags.FlagFlag, cmd.Flags().Lookup(cliflags.FlagFlag))

	cmd.Flags().String(cliflags.ProjectFlag, "", "The project key")
	_ = cmd.MarkFlagRequired(cliflags.ProjectFlag)
	_ = cmd.Flags().SetAnnotation(cliflags.ProjectFlag, "required", []string{"true"})
	_ = viper.BindPFlag(cliflags.ProjectFlag, cmd.Flags().Lookup(cliflags.ProjectFlag))

	cmd.Flags().String(cliflags.EnvironmentFlag, "",
		"Environment key (optional). Required when --rollout-id is set; see API-PAPERCUTS.md PC-004.")
	_ = viper.BindPFlag(cliflags.EnvironmentFlag, cmd.Flags().Lookup(cliflags.EnvironmentFlag))

	cmd.Flags().String(cliflags.RolloutIdFlag, "", cliflags.RolloutIdFlagDescription)
	_ = viper.BindPFlag(cliflags.RolloutIdFlag, cmd.Flags().Lookup(cliflags.RolloutIdFlag))
}

// statusRunE is the status verb body. It reads flag values at RunE time (NOT at constructor
// time), validates the --rollout-id / --environment pairing BEFORE any API call (D-03), then
// resolves the rollout via Client.Get (when --rollout-id is set) or Client.List → items[0]
// (most-recent fallback, D-04). On success it marshals the rollout into the existing
// rollouts.v1beta1 envelope verbatim (D-05); on error it routes through emitStatusError
// which mirrors the list / start error contract.
func statusRunE(client rollouts.Client) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, _ []string) error {
		accessToken := viper.GetString(cliflags.AccessTokenFlag)
		baseURI := viper.GetString(cliflags.BaseURIFlag)
		projKey := viper.GetString(cliflags.ProjectFlag)
		flagKey := viper.GetString(cliflags.FlagFlag)
		envKey := viper.GetString(cliflags.EnvironmentFlag)
		rolloutID := viper.GetString(cliflags.RolloutIdFlag)

		// D-03 validation BEFORE any API call: --rollout-id requires --environment.
		// Surface as a typed RolloutError so the JSON envelope carries error.code:
		// "bad_request" (papers over PC-004's API surface in a CLI-side guard).
		if rolloutID != "" && envKey == "" {
			return emitStatusError(cmd, &rollouts.RolloutError{
				Code:       rollouts.ErrCodeBadRequest,
				Message:    "--environment is required when --rollout-id is set (API-PAPERCUTS.md PC-004 — GET-by-ID requires environmentKey in the URL path)",
				NextAction: "Pass --environment alongside --rollout-id, or omit --rollout-id to get the most-recent rollout on the flag",
			})
		}

		rollout, err := resolveRollout(cmd.Context(), client, accessToken, baseURI, projKey, flagKey, envKey, rolloutID)
		if err != nil {
			return emitStatusError(cmd, err)
		}

		// Guarded rollouts have metric-results we can fetch in parallel. Latest snapshot only —
		// no time series / chart data. Failure to derive an env key (most-recent path with no
		// --environment) drops metric-results silently rather than failing the command, since
		// PC-019 means we can only recover the env key by parsing _links.self.href.
		if len(rollout.MetricConfigurations) > 0 {
			resolvedEnv := envKey
			if resolvedEnv == "" {
				resolvedEnv = envKeyFromLinks(rollout)
			}
			if resolvedEnv != "" {
				results, mrErr := fetchMetricResults(cmd.Context(), client, accessToken, baseURI, projKey, flagKey, resolvedEnv, rollout)
				if mrErr != nil {
					return emitStatusError(cmd, mrErr)
				}
				rollout.MetricResults = results
			}
		}

		env := rollouts.NewRolloutEnvelope(rollout)
		return emitStatusSuccess(cmd, env, rollout)
	}
}

// fetchMetricResults runs one GetMetricResult per metric configuration in parallel via
// errgroup. Order in the returned slice matches the order of rollout.MetricConfigurations
// so the plaintext renderer can pair config and result by index. If any fetch fails, the
// entire batch fails — the operator gets one error rather than a partially-populated
// results slice with silently-missing entries.
func fetchMetricResults(
	ctx context.Context,
	client rollouts.Client,
	accessToken, baseURI, projKey, flagKey, envKey string,
	rollout *rollouts.Rollout,
) ([]rollouts.MetricResult, error) {
	results := make([]rollouts.MetricResult, len(rollout.MetricConfigurations))
	g, gctx := errgroup.WithContext(ctx)
	for i, mc := range rollout.MetricConfigurations {
		i, mc := i, mc
		g.Go(func() error {
			mr, err := client.GetMetricResult(gctx, accessToken, baseURI, projKey, flagKey, envKey, rollout.ID, mc.MetricKey)
			if err != nil {
				return err
			}
			if mr != nil {
				results[i] = *mr
			}
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return nil, err
	}
	return results, nil
}

// envKeyFromLinks recovers the env key from the rollout's _links.self.href when the
// operator did not pass --environment explicitly. PC-019 surface — the API response does
// not echo environmentKey, only environmentId, so this is the only way to derive the
// human-readable env key for downstream calls (metric-results, which needs envKey in
// the URL path). Returns empty string on any failure to parse.
//
// Expected href shape: /internal/projects/{p}/environments/{e}/automated-releases/{id}
// (per real-staging smoke 2026-05-14; see .planning/phases/03-status-watch/03-SMOKE.md).
func envKeyFromLinks(rollout *rollouts.Rollout) string {
	self, ok := rollout.Links["self"]
	if !ok || self.Href == "" {
		return ""
	}
	parts := strings.Split(self.Href, "/environments/")
	if len(parts) < 2 {
		return ""
	}
	rest := strings.SplitN(parts[1], "/", 2)
	return rest[0]
}

// resolveRollout dispatches to either Client.Get (when --rollout-id is provided) or
// Client.List → items[0] (most-recent path) per D-04. Returns ErrCodeNoRolloutsFound when
// the list path comes back empty (D-09).
//
// "Most recent" semantics are honored verbatim from Phase 1 list — the production
// internal/rollouts/client.go.List sorts by CreatedAt DESC, ID ASC (lines 160-166), so
// items[0] is the answer with Limit:1.
func resolveRollout(
	ctx context.Context,
	client rollouts.Client,
	accessToken, baseURI, projKey, flagKey, envKey, rolloutID string,
) (*rollouts.Rollout, error) {
	if rolloutID != "" {
		// Direct GET-by-ID path (PC-004 surface — env required, validated upstream).
		return client.Get(ctx, accessToken, baseURI, projKey, envKey, rolloutID)
	}

	// Most-recent fallback. Limit:1 + the client's CreatedAt DESC sort gives us items[0]
	// as the answer; no Phase-3-specific sort needed (D-04).
	list, err := client.List(ctx, accessToken, baseURI, projKey, flagKey, rollouts.ListOpts{
		Environment: envKey,
		Limit:       1,
	})
	if err != nil {
		return nil, err
	}
	if list == nil || len(list.Items) == 0 {
		return nil, &rollouts.RolloutError{
			Code:       rollouts.ErrCodeNoRolloutsFound,
			Message:    fmt.Sprintf("No rollouts found for flag %q", flagKey),
			NextAction: "Verify the flag has at least one rollout, or pass --rollout-id <id> --environment <env> to address a specific rollout",
		}
	}
	r := list.Items[0]
	return &r, nil
}

// emitStatusSuccess writes the success envelope to stdout. JSON output emits the full
// envelope (D-05 — reuse verbatim); plaintext output emits the sectioned-block renderer
// per D-07.
func emitStatusSuccess(cmd *cobra.Command, env rollouts.Envelope, rollout *rollouts.Rollout) error {
	if cliflags.GetOutputKind(cmd) == "json" {
		body, err := json.MarshalIndent(env, "", "  ")
		if err != nil {
			return errors.NewErrorWrapped("failed to marshal rollouts envelope", err)
		}
		fmt.Fprintln(cmd.OutOrStdout(), string(body))
		return nil
	}

	fmt.Fprint(cmd.OutOrStdout(), RenderRolloutStatusPlaintext(rollout))
	return nil
}

// emitStatusError converts a client error into either a JSON envelope (when --output json)
// or a plain message (plaintext output). In both cases it returns a non-nil error so Cobra
// exits with code 1 (D-10 — exit 1 for any error path; no numeric taxonomy).
//
// JSON-mode routing (AGENT-04 / D-07): the error envelope is written to stdout, NOT stderr.
// The returned error is a short sentinel so the root command's Fprintln(os.Stderr, err)
// does not double-emit the envelope to stderr. Mirrors list.go emitError and start.go
// emitStartError verbatim.
func emitStatusError(cmd *cobra.Command, err error) error {
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
		return errors.NewError("rollouts status failed")
	}

	return errors.NewError(message)
}
