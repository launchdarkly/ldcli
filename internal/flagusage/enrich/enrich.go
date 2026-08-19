package enrich

import (
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"sync"
	"time"

	"github.com/launchdarkly/ldcli/internal/flagusage/scanner"
)

var DefaultCriticalEnvs = []string{
	"production",
	"staging",
	"catamorphic",
	"managed-federal-stg",
	"managed-federal-prod",
	"managed-eu-production",
}

type Options struct {
	ProjectKey   string
	Environments []string      // environments to check status for; empty = DefaultCriticalEnvs
	EvalWindow   time.Duration // evaluation lookback window; 0 = default (7 days)
	WindowLabel  string        // human label for the window (e.g. "6h", "7d"); derived from EvalWindow if empty

	// Series, when set, fetches the per-bucket evaluation timeseries (daily, or
	// hourly for windows <24h) via the public usage endpoint, one call per
	// flag/env. Default (false) uses the batched evaluationSummaries endpoint,
	// which returns window totals + per-variation split in a single call but no
	// buckets.
	Series bool

	// Exposures, when set, fetches true unique-context counts (the figure a
	// guarded release gates on) per context kind via the contextsCount endpoint.
	// Off by default — it's one call per flag/env/contextKind.
	Exposures bool
	// ContextKinds restricts exposure fetching to these kinds; empty = discover
	// the flag's kinds via the contextKinds endpoint.
	ContextKinds []string
}

// defaultEvalWindow is the lookback used when Options.EvalWindow is unset.
const defaultEvalWindow = 7 * 24 * time.Hour

// Enrich takes scan results and hydrates each unique flag with data from the LD API.
// It fetches flag config + per-environment status concurrently with bounded parallelism.
func Enrich(client *Client, scanResult *scanner.ScanResult, opts Options) ([]FlagDetail, error) {
	flagKeys := uniqueFlagKeys(scanResult)
	if len(flagKeys) == 0 {
		return nil, nil
	}

	callSitesByKey := countCallSites(scanResult)

	var (
		mu      sync.Mutex
		results []FlagDetail
		errors  []error
		wg      sync.WaitGroup
		sem     = make(chan struct{}, 10) // concurrency limit
	)

	for _, key := range flagKeys {
		wg.Add(1)
		go func(flagKey string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			detail, err := enrichFlag(client, flagKey, opts, callSitesByKey[flagKey])
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				errors = append(errors, fmt.Errorf("%s: %w", flagKey, err))
				return
			}
			results = append(results, *detail)
		}(key)
	}

	wg.Wait()

	if len(errors) > 0 && len(results) == 0 {
		return nil, fmt.Errorf("all flag lookups failed; first error: %w", errors[0])
	}

	return results, nil
}

func enrichFlag(client *Client, flagKey string, opts Options, callSites int) (*FlagDetail, error) {
	path := fmt.Sprintf("/api/v2/flags/%s/%s", opts.ProjectKey, flagKey)

	var flagResp apiFlagResponse
	if err := client.get(path, &flagResp); err != nil {
		// A flag that's referenced in code but missing from the project (404)
		// is an orphaned reference: the SDK call always falls back to the
		// in-code default, so the branch is dead (or the key was renamed and
		// the code wasn't updated). Surface it as a prime cleanup candidate
		// rather than silently dropping it.
		if errors.Is(err, ErrNotFound) {
			return &FlagDetail{
				Key:        flagKey,
				CallSites:  callSites,
				CodeRefs:   callSites,
				StaleState: "orphanedReference",
			}, nil
		}
		return nil, err
	}

	detail := &FlagDetail{
		Key:          flagResp.Key,
		Name:         flagResp.Name,
		Description:  flagResp.Description,
		Temporary:    flagResp.Temporary,
		Archived:     flagResp.Archived,
		CreationDate: time.UnixMilli(flagResp.CreationDate),
		Tags:         flagResp.Tags,
		Kind:         flagResp.Kind,
		CallSites:    callSites,
		CodeRefs:     callSites, // from our scan
		Environments: make(map[string]EnvStatus),
	}

	for _, v := range flagResp.Variations {
		detail.Variations = append(detail.Variations, Variation{
			Value:       v.Value,
			Name:        v.Name,
			Description: v.Description,
		})
	}

	envKeys := opts.Environments
	if len(envKeys) == 0 {
		envKeys = DefaultCriticalEnvs
	}

	presentEnvs := make([]string, 0, len(envKeys))
	for _, envKey := range envKeys {
		envConfig, ok := flagResp.Environments[envKey]
		if !ok {
			continue
		}

		isSimple := envConfig.On &&
			len(envConfig.Rules) == 0 &&
			len(envConfig.Targets) == 0 &&
			len(envConfig.ContextTargets) == 0 &&
			len(envConfig.Prerequisites) == 0 &&
			envConfig.Fallthrough.Variation != nil &&
			envConfig.Fallthrough.Rollout == nil

		targeting := TargetingSummary{
			RuleCount:            len(envConfig.Rules),
			TargetCount:          len(envConfig.Targets),
			ContextTargetCount:   len(envConfig.ContextTargets),
			PrerequisiteCount:    len(envConfig.Prerequisites),
			IsSimpleToggle:       isSimple,
			FallthroughVariation: envConfig.Fallthrough.Variation,
			OffVariation:         envConfig.OffVariation,
		}

		var exposure ExposureSummary
		for idxStr, sv := range envConfig.Summary.Variations {
			idx := 0
			fmt.Sscanf(idxStr, "%d", &idx)
			exposure.VariationExposure = append(exposure.VariationExposure, VariationExposure{
				VariationIndex: idx,
				Rules:          sv.Rules,
				Targets:        sv.Targets,
				ContextTargets: sv.ContextTargets,
				IsFallthrough:  sv.IsFallthrough,
				IsOff:          sv.IsOff,
			})
		}

		envStatus := EnvStatus{
			On:           envConfig.On,
			LastModified: time.UnixMilli(envConfig.LastModified),
			Targeting:    targeting,
			Exposure:     exposure,
		}

		// Fetch per-flag status in this environment
		statusPath := fmt.Sprintf("/api/v2/flag-statuses/%s/%s/%s",
			opts.ProjectKey, envKey, flagKey)
		var statusResp apiFlagStatusByFlag
		if err := client.get(statusPath, &statusResp); err == nil {
			envStatus.FlagStatus = statusResp.Name
			if statusResp.LastRequested != "" {
				if t, err := time.Parse(time.RFC3339, statusResp.LastRequested); err == nil {
					envStatus.LastEvaluation = t
				}
			}
		}

		detail.Environments[envKey] = envStatus
		presentEnvs = append(presentEnvs, envKey)
	}

	// Evaluation counts (total SDK calls). Default: one batched call across all
	// present envs. Series mode: per-env timeseries via the public usage endpoint.
	if opts.Series {
		for _, envKey := range presentEnvs {
			es := detail.Environments[envKey]
			es.Evaluations = fetchEvaluationCounts(client, opts.ProjectKey, envKey, flagKey, detail.Variations, opts.EvalWindow)
			detail.Environments[envKey] = es
		}
	} else {
		for envKey, counts := range fetchEvalSummaries(client, opts.ProjectKey, flagKey, presentEnvs, flagResp.Variations, opts.EvalWindow) {
			es := detail.Environments[envKey]
			es.Evaluations = counts
			detail.Environments[envKey] = es
		}
	}

	// Unique-context exposures (opt-in) — the figure a guarded release gates on.
	if opts.Exposures {
		windowLabel := opts.WindowLabel
		if windowLabel == "" {
			windowLabel = formatWindow(opts.EvalWindow)
		}
		for _, envKey := range presentEnvs {
			es := detail.Environments[envKey]
			es.UniqueContexts = fetchUniqueContexts(client, opts.ProjectKey, flagKey, envKey, opts.ContextKinds, opts.EvalWindow, windowLabel)
			detail.Environments[envKey] = es
		}
	}

	detail.finalize(time.Now())

	return detail, nil
}

// stableRolloutAge is how long a deterministic targeting state must have been
// settled before a still-evaluated flag is treated as safe to remove from code.
// Guards against flagging flags that are merely mid-rollout (turned fully on
// today but not yet finished ramping).
const stableRolloutAge = 14 * 24 * time.Hour

// finalize computes the flag's staleState and, only when it's ready for code
// removal, the value it can be hardcoded to.
func (d *FlagDetail) finalize(now time.Time) {
	d.StaleState = computeStaleStateAt(d, now)
	if d.StaleState == "readyForCodeRemoval" {
		if v, ok := recommendedRemovalValue(d); ok {
			d.RecommendedValue = v
		}
	}
}

func computeStaleStateAt(d *FlagDetail, now time.Time) string {
	if d.Archived {
		return "archived"
	}

	allInactive := true
	allLaunched := true
	hasAnyEnv := false
	hasTraffic := false

	for _, env := range d.Environments {
		hasAnyEnv = true
		if env.FlagStatus != "inactive" {
			allInactive = false
		}
		if env.FlagStatus != "launched" {
			allLaunched = false
		}
		if env.Evaluations.Total7d > 0 {
			hasTraffic = true
		}
	}

	if !hasAnyEnv {
		return "unknown"
	}

	// Permanent flags (temporary=false) are a declaration by the owner that the
	// flag is meant to stay — kill switches, ops toggles, configurable limits.
	// A permanent flag serving one variation is just config doing its job, not
	// debt, so it's never a code-removal candidate regardless of rollout state.
	// `temporary` is therefore the first gate on every removal recommendation.
	if d.Temporary && d.CodeRefs > 0 {
		// A fully rolled-out flag is still evaluated constantly, so eval volume
		// and LD's per-env "launched" status (which lags, and never agrees
		// across the no-traffic federal/EU envs) miss it. Check directly whether
		// every traffic-bearing env serves a single deterministic variation: if
		// so, the code branch is dead and the flag is ready to be hardcoded out.
		if isFullyRolledOut(d, now) {
			return "readyForCodeRemoval"
		}
		// allLaunched is a coarse proxy for "rolled out" — it can't tell whether
		// the envs serve the *same* variation. Trust it only when we have no
		// traffic data to run the precise isFullyRolledOut check above.
		if allLaunched && !hasTraffic {
			return "readyForCodeRemoval"
		}
	}

	if allInactive && d.CodeRefs == 0 {
		return "readyToArchive"
	}
	if allInactive {
		return "inactive"
	}
	if allLaunched {
		return "launched"
	}

	return "active"
}

// servedVariation returns the single variation index an environment serves
// deterministically, or ok=false if the env still branches (percentage rollout,
// targeting rules, or individual targets — i.e. the flag still does real work).
func servedVariation(env EnvStatus) (idx int, ok bool) {
	if !env.On {
		if env.Targeting.OffVariation != nil {
			return *env.Targeting.OffVariation, true
		}
		return 0, false
	}
	if env.Targeting.IsSimpleToggle && env.Targeting.FallthroughVariation != nil {
		return *env.Targeting.FallthroughVariation, true
	}
	return 0, false
}

// consensusVariation returns the single variation index served across the
// flag's environments, or ok=false if any env still branches or the envs serve
// different variations. When trafficOnly is set, only relevant (traffic-bearing)
// envs are considered, so zero-traffic envs (e.g. managed-federal-*) can't mask
// a finished rollout.
func consensusVariation(d *FlagDetail, trafficOnly bool) (int, bool) {
	served := -1
	seen := 0
	for _, env := range d.Environments {
		if trafficOnly && env.Evaluations.Total7d <= 0 {
			continue
		}
		idx, ok := servedVariation(env)
		if !ok {
			return 0, false // this env still branches
		}
		if served == -1 {
			served = idx
		} else if served != idx {
			return 0, false // envs serve different variations
		}
		seen++
	}
	if seen == 0 {
		return 0, false
	}
	return served, true
}

// isFullyRolledOut reports whether every relevant (traffic-bearing) environment
// serves the same single variation, and that state has been stable long enough
// to be confident the rollout is finished rather than in progress.
func isFullyRolledOut(d *FlagDetail, now time.Time) bool {
	if _, ok := consensusVariation(d, true); !ok {
		return false
	}
	for _, env := range d.Environments {
		if env.Evaluations.Total7d <= 0 {
			continue
		}
		if env.FlagStatus == "launched" ||
			(!env.LastModified.IsZero() && now.Sub(env.LastModified) > stableRolloutAge) {
			return true // stable: launched, or settled long enough ago
		}
	}
	return false
}

// recommendedRemovalValue returns the variation value a fully-rolled-out flag
// can be hardcoded to. Prefers the value agreed on by traffic-bearing envs;
// falls back to all configured envs for the no-traffic launched case.
func recommendedRemovalValue(d *FlagDetail) (any, bool) {
	idx, ok := consensusVariation(d, true)
	if !ok {
		idx, ok = consensusVariation(d, false)
	}
	if !ok || idx < 0 || idx >= len(d.Variations) {
		return nil, false
	}
	return d.Variations[idx].Value, true
}

// evalWindowStart returns the start of the evaluation window as a millisecond
// timestamp, aligned to a bucket boundary so the value (and therefore the API
// cache key) is stable across repeated runs — a raw time.Now() here makes every
// request URL unique and defeats the response cache entirely. The window length
// also implicitly selects the API's bucket granularity: windows >= 24h return
// daily buckets (aligned to the UTC day), shorter windows return hourly buckets
// (aligned to the hour) so a guarded-rollout-scale window can see per-hour rates.
func evalWindowStart(now time.Time, window time.Duration) int64 {
	from, _ := evalWindowRange(now, window)
	return from
}

// evalWindowRange returns the [from, to) window as millisecond timestamps, both
// aligned to the same bucket boundary as evalWindowStart so endpoints that need
// an explicit `to` (contextsCount, evaluationSummaries) stay cache-stable across
// repeated same-bucket runs.
func evalWindowRange(now time.Time, window time.Duration) (from, to int64) {
	if window <= 0 {
		window = defaultEvalWindow
	}
	align := time.Hour
	if window >= 24*time.Hour {
		align = 24 * time.Hour
	}
	end := now.UTC().Truncate(align)
	return end.Add(-window).UnixMilli(), end.UnixMilli()
}

// formatWindow renders a window duration as a short label (e.g. "6h", "7d").
func formatWindow(d time.Duration) string {
	if d <= 0 {
		d = defaultEvalWindow
	}
	if d%(24*time.Hour) == 0 {
		return fmt.Sprintf("%dd", d/(24*time.Hour))
	}
	if d%time.Hour == 0 {
		return fmt.Sprintf("%dh", d/time.Hour)
	}
	return d.String()
}

// variationLabels maps a variation _id to a human label (name, else its value),
// for translating the evaluationSummaries variationCounts (keyed by _id).
func variationLabels(variations []apiVariation) map[string]string {
	out := make(map[string]string, len(variations))
	for _, v := range variations {
		label := v.Name
		if label == "" {
			label = fmt.Sprintf("%v", v.Value)
		}
		out[v.ID] = label
	}
	return out
}

// fetchEvalSummaries fetches total evaluation counts (and the per-variation
// split) for one flag across many envs in a single batched POST. This is the
// default eval source — far fewer requests than the per-flag/env usage endpoint,
// at the cost of no per-bucket timeseries (use Series mode for that).
func fetchEvalSummaries(client *Client, projectKey, flagKey string, envKeys []string, variations []apiVariation, window time.Duration) map[string]EvaluationCounts {
	out := make(map[string]EvaluationCounts)
	if len(envKeys) == 0 {
		return out
	}
	from, to := evalWindowRange(time.Now(), window)
	idToLabel := variationLabels(variations)
	path := fmt.Sprintf("/internal/projects/%s/evaluationSummaries", projectKey)

	// The endpoint caps each request at 10 envs; chunk defensively.
	for _, chunk := range chunkStrings(envKeys, 10) {
		req := apiEvalSummariesRequest{
			FlagKeys:        []string{flagKey},
			EnvironmentKeys: chunk,
			From:            from,
			To:              to,
		}
		var resp apiEvalSummariesResponse
		if err := client.postJSON(path, req, nil, &resp); err != nil {
			continue
		}
		for _, summary := range resp.Data {
			if summary.FlagKey != flagKey {
				continue
			}
			for envKey, ev := range summary.Environments {
				counts := EvaluationCounts{Total7d: ev.TotalEvaluations}
				if len(ev.VariationCounts) > 0 {
					counts.ByVariation = make(map[string]int64, len(ev.VariationCounts))
					for id, c := range ev.VariationCounts {
						label := idToLabel[id]
						if label == "" {
							label = id
						}
						counts.ByVariation[label] += c
					}
				}
				out[envKey] = counts
			}
		}
	}
	return out
}

// fetchContextKinds returns the context kinds a flag is evaluated for in an env.
func fetchContextKinds(client *Client, projectKey, flagKey, envKey string) []string {
	path := fmt.Sprintf("/internal/projects/%s/flags/%s/monitor/contextKinds?filter=%s",
		projectKey, flagKey, url.QueryEscape("env equals "+envKey))
	var resp apiContextKindsResponse
	if err := client.get(path, &resp); err != nil {
		return nil
	}
	return resp.Data.ContextKinds
}

// fetchUniqueContexts returns true unique-context counts (uniqExact) per context
// kind for a flag/env over the window — the "exposures" a guarded release gates
// on, as opposed to total evaluation calls. Returns nil if no kind has data.
func fetchUniqueContexts(client *Client, projectKey, flagKey, envKey string, kinds []string, window time.Duration, windowLabel string) *UniqueContextCounts {
	if len(kinds) == 0 {
		kinds = fetchContextKinds(client, projectKey, flagKey, envKey)
	}
	if len(kinds) == 0 {
		return nil
	}
	from, to := evalWindowRange(time.Now(), window)
	result := &UniqueContextCounts{Window: windowLabel, ByContextKind: make(map[string]UniqueContextStat)}
	for _, kind := range kinds {
		filter := url.QueryEscape(fmt.Sprintf("env equals %s,contextKind equals %s", envKey, kind))
		path := fmt.Sprintf("/internal/projects/%s/flags/%s/monitor/contextsCount?from=%d&to=%d&filter=%s",
			projectKey, flagKey, from, to, filter)
		var resp apiContextsCountResponse
		if err := client.get(path, &resp); err != nil {
			continue
		}
		result.ByContextKind[kind] = UniqueContextStat{
			Count:     resp.Data.TotalUniqueContexts,
			IsSampled: resp.Data.IsSampled,
		}
	}
	if len(result.ByContextKind) == 0 {
		return nil
	}
	return result
}

// chunkStrings splits s into consecutive slices of at most size elements.
func chunkStrings(s []string, size int) [][]string {
	if size <= 0 {
		return [][]string{s}
	}
	var chunks [][]string
	for i := 0; i < len(s); i += size {
		end := i + size
		if end > len(s) {
			end = len(s)
		}
		chunks = append(chunks, s[i:end])
	}
	return chunks
}

func fetchEvaluationCounts(client *Client, projectKey, envKey, flagKey string, variations []Variation, window time.Duration) EvaluationCounts {
	var counts EvaluationCounts

	if window <= 0 {
		window = defaultEvalWindow
	}
	from := evalWindowStart(time.Now(), window)
	path := fmt.Sprintf("/api/v2/usage/evaluations/%s/%s/%s?from=%d",
		projectKey, envKey, flagKey, from)

	var resp apiUsageResponse
	if err := client.getWithHeaders(path, map[string]string{"LD-API-Version": "beta"}, &resp); err != nil {
		return counts
	}

	counts.Total7d = resp.TotalEvaluations

	// Build variation index → label map from metadata
	varLabels := make(map[string]string)
	for i, m := range resp.Metadata {
		idx := strconv.Itoa(i)
		label := fmt.Sprintf("%v", m.Key)
		if i < len(variations) && variations[i].Name != "" {
			label = variations[i].Name
		}
		varLabels[idx] = label
	}

	counts.ByVariation = make(map[string]int64)
	for _, day := range resp.Series {
		ts, ok := day["time"].(float64)
		if !ok {
			continue
		}
		dayTotal := int64(0)
		for k, v := range day {
			if k == "time" {
				continue
			}
			if count, ok := v.(float64); ok {
				dayTotal += int64(count)
				label := k
				if l, exists := varLabels[k]; exists {
					label = l
				}
				counts.ByVariation[label] += int64(count)
			}
		}
		counts.Daily = append(counts.Daily, DailyEvaluation{
			Time:  time.UnixMilli(int64(ts)),
			Count: dayTotal,
		})
	}

	return counts
}

// enrichable reports whether a scan reference carries a real LD flag key worth
// looking up. Wrapper definitions aren't call sites, and unresolved wrapper
// calls hold a guessed export name rather than a real key — enriching either
// would just produce bogus 404s.
func enrichable(ref scanner.FlagReference) bool {
	return ref.Kind != "wrapper-definition" && ref.Kind != "wrapper-call-unresolved"
}

func uniqueFlagKeys(result *scanner.ScanResult) []string {
	seen := make(map[string]bool)
	var keys []string
	for _, ref := range result.References {
		if !enrichable(ref) {
			continue
		}
		if !seen[ref.FlagKey] {
			seen[ref.FlagKey] = true
			keys = append(keys, ref.FlagKey)
		}
	}
	return keys
}

func countCallSites(result *scanner.ScanResult) map[string]int {
	counts := make(map[string]int)
	for _, ref := range result.References {
		if enrichable(ref) {
			counts[ref.FlagKey]++
		}
	}
	return counts
}
