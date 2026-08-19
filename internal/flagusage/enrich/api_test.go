package enrich

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/launchdarkly/ldcli/internal/flagusage/scanner"
)

// mockLD stands in for the LaunchDarkly API + gonfalon internal endpoints so we
// can exercise the full client → fetch → Enrich path with no network and no
// token. It records the requests it saw so tests can assert request shape
// (e.g. the batched POST body, the contextsCount filter).
type mockLD struct {
	t      *testing.T
	server *httptest.Server

	// canned responses
	flagJSON   string                          // GET /api/v2/flags/...
	statusName string                          // GET /api/v2/flag-statuses/...
	evalEnv    map[string]apiEvalEnvVariations // POST evaluationSummaries, by env
	kinds      []string                        // GET monitor/contextKinds
	uniqByKind map[string]int64                // GET monitor/contextsCount, by kind

	// recorded
	evalReq      apiEvalSummariesRequest
	contextsHits []string // filter values seen by contextsCount
}

func newMockLD(t *testing.T) *mockLD {
	m := &mockLD{
		t:          t,
		statusName: "launched",
		evalEnv:    map[string]apiEvalEnvVariations{},
		uniqByKind: map[string]int64{},
	}
	m.server = httptest.NewServer(http.HandlerFunc(m.handle))
	t.Cleanup(m.server.Close)
	// Route cache writes to a throwaway HOME so tests don't share state or hit
	// a real ~/.cache/flagpls.
	t.Setenv("HOME", t.TempDir())
	return m
}

func (m *mockLD) client() *Client { return NewClient("fake-token", m.server.URL) }

func (m *mockLD) handle(w http.ResponseWriter, r *http.Request) {
	p := r.URL.Path
	switch {
	case strings.HasPrefix(p, "/api/v2/flags/"):
		io.WriteString(w, m.flagJSON)
	case strings.HasPrefix(p, "/api/v2/flag-statuses/"):
		json.NewEncoder(w).Encode(apiFlagStatusByFlag{Name: m.statusName, LastRequested: "2026-06-20T00:00:00Z"})
	case strings.HasSuffix(p, "/evaluationSummaries"):
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &m.evalReq)
		envs := map[string]apiEvalEnvVariations{}
		for _, e := range m.evalReq.EnvironmentKeys {
			if v, ok := m.evalEnv[e]; ok {
				envs[e] = v
			}
		}
		json.NewEncoder(w).Encode(apiEvalSummariesResponse{
			Data: []apiEvalSummary{{FlagKey: m.evalReq.FlagKeys[0], Environments: envs}},
		})
	case strings.HasSuffix(p, "/monitor/contextKinds"):
		var resp apiContextKindsResponse
		resp.Data.ContextKinds = m.kinds
		json.NewEncoder(w).Encode(resp)
	case strings.HasSuffix(p, "/monitor/contextsCount"):
		filter := r.URL.Query().Get("filter")
		m.contextsHits = append(m.contextsHits, filter)
		var count int64
		for kind, c := range m.uniqByKind {
			if strings.Contains(filter, "contextKind equals "+kind) {
				count = c
			}
		}
		var resp apiContextsCountResponse
		resp.Data.TotalUniqueContexts = count
		json.NewEncoder(w).Encode(resp)
	default:
		http.Error(w, "unexpected path: "+p, http.StatusNotFound)
	}
}

// boolFlagJSON builds a simple-toggle boolean flag config serving variation 0
// in production, with two variations whose _ids match the eval-summary keys.
const boolFlagJSON = `{
  "key": "my-flag", "name": "My Flag", "kind": "boolean", "temporary": true,
  "creationDate": 1700000000000,
  "variations": [
    {"_id": "v0", "value": true, "name": "On", "valueHash": "h0"},
    {"_id": "v1", "value": false, "name": "Off", "valueHash": "h1"}
  ],
  "environments": {
    "production": {
      "on": true, "lastModified": 1700000000000,
      "rules": [], "targets": [], "contextTargets": [], "prerequisites": [],
      "fallthrough": {"variation": 0}, "offVariation": 1,
      "_summary": {"variations": {"0": {"isFallthrough": true}, "1": {"isOff": true}}}
    }
  }
}`

func scanWith(flagKey string) *scanner.ScanResult {
	return &scanner.ScanResult{
		References: []scanner.FlagReference{
			{FlagKey: flagKey, Kind: "direct", FilePath: "a.ts", Line: 1},
		},
	}
}

func TestEnrichBatchedEvalSummaries(t *testing.T) {
	m := newMockLD(t)
	m.flagJSON = boolFlagJSON
	m.evalEnv["production"] = apiEvalEnvVariations{
		TotalEvaluations: 1500,
		VariationCounts:  map[string]int64{"v0": 1000, "v1": 500},
	}

	details, err := Enrich(m.client(), scanWith("my-flag"), Options{
		ProjectKey: "default", Environments: []string{"production"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(details) != 1 {
		t.Fatalf("expected 1 flag, got %d", len(details))
	}

	// The batched POST must carry the flag, env, and a from<to window.
	if got := m.evalReq.FlagKeys; len(got) != 1 || got[0] != "my-flag" {
		t.Errorf("batched request flagKeys = %v", got)
	}
	if m.evalReq.From >= m.evalReq.To || m.evalReq.From == 0 {
		t.Errorf("batched request window invalid: from=%d to=%d", m.evalReq.From, m.evalReq.To)
	}

	env := details[0].Environments["production"]
	if env.Evaluations.Total7d != 1500 {
		t.Errorf("Total = %d, want 1500", env.Evaluations.Total7d)
	}
	// variationCounts keyed by _id must be relabeled to variation names.
	if env.Evaluations.ByVariation["On"] != 1000 || env.Evaluations.ByVariation["Off"] != 500 {
		t.Errorf("ByVariation = %v, want On:1000 Off:500", env.Evaluations.ByVariation)
	}
	// Default (non-series) mode must not populate per-bucket data.
	if len(env.Evaluations.Daily) != 0 {
		t.Errorf("expected no Daily buckets in batched mode, got %d", len(env.Evaluations.Daily))
	}
	// Exposures off by default.
	if env.UniqueContexts != nil {
		t.Errorf("expected no UniqueContexts when -exposures off, got %+v", env.UniqueContexts)
	}
}

func TestEnrichExposures(t *testing.T) {
	m := newMockLD(t)
	m.flagJSON = boolFlagJSON
	m.evalEnv["production"] = apiEvalEnvVariations{TotalEvaluations: 9999999}
	m.kinds = []string{"user", "account"}
	m.uniqByKind = map[string]int64{"user": 6971, "account": 12}

	details, err := Enrich(m.client(), scanWith("my-flag"), Options{
		ProjectKey: "default", Environments: []string{"production"},
		Exposures: true, EvalWindow: 6 * time.Hour, WindowLabel: "6h",
	})
	if err != nil {
		t.Fatal(err)
	}
	uc := details[0].Environments["production"].UniqueContexts
	if uc == nil {
		t.Fatal("expected UniqueContexts to be populated with -exposures")
	}
	if uc.Window != "6h" {
		t.Errorf("window label = %q, want 6h", uc.Window)
	}
	if uc.ByContextKind["user"].Count != 6971 || uc.ByContextKind["account"].Count != 12 {
		t.Errorf("byContextKind = %+v", uc.ByContextKind)
	}
	// Unique contexts must be wildly smaller than eval count — the whole point.
	if uc.ByContextKind["user"].Count >= details[0].Environments["production"].Evaluations.Total7d {
		t.Error("unique contexts should be far below total evaluations")
	}
	// The filter must scope both env and contextKind.
	foundUser := false
	for _, f := range m.contextsHits {
		if strings.Contains(f, "env equals production") && strings.Contains(f, "contextKind equals user") {
			foundUser = true
		}
	}
	if !foundUser {
		t.Errorf("contextsCount filter never scoped env+kind; saw %v", m.contextsHits)
	}
}

func TestEnrichExposuresExplicitKindsSkipsDiscovery(t *testing.T) {
	m := newMockLD(t)
	m.flagJSON = boolFlagJSON
	m.evalEnv["production"] = apiEvalEnvVariations{TotalEvaluations: 100}
	// kinds endpoint would return these, but we pass an explicit list instead.
	m.kinds = []string{"user", "account", "member"}
	m.uniqByKind = map[string]int64{"user": 50}

	details, err := Enrich(m.client(), scanWith("my-flag"), Options{
		ProjectKey: "default", Environments: []string{"production"},
		Exposures: true, ContextKinds: []string{"user"},
	})
	if err != nil {
		t.Fatal(err)
	}
	uc := details[0].Environments["production"].UniqueContexts
	if uc == nil || len(uc.ByContextKind) != 1 || uc.ByContextKind["user"].Count != 50 {
		t.Fatalf("explicit ContextKinds should query only user; got %+v", uc)
	}
}

func TestEnrichSeriesMode(t *testing.T) {
	m := newMockLD(t)
	m.flagJSON = boolFlagJSON
	// In series mode the usage endpoint is hit instead of evaluationSummaries.
	usageHit := false
	base := http.HandlerFunc(m.handle)
	m.server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/v2/usage/evaluations/") {
			usageHit = true
			json.NewEncoder(w).Encode(apiUsageResponse{
				TotalEvaluations: 700,
				Series: []map[string]any{
					{"time": float64(1700000000000), "0": float64(400)},
					{"time": float64(1700003600000), "0": float64(300)},
				},
			})
			return
		}
		base.ServeHTTP(w, r)
	})

	details, err := Enrich(m.client(), scanWith("my-flag"), Options{
		ProjectKey: "default", Environments: []string{"production"},
		Series: true, EvalWindow: 6 * time.Hour,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !usageHit {
		t.Error("series mode should hit the usage timeseries endpoint")
	}
	env := details[0].Environments["production"]
	if env.Evaluations.Total7d != 700 {
		t.Errorf("Total = %d, want 700", env.Evaluations.Total7d)
	}
	if len(env.Evaluations.Daily) != 2 {
		t.Errorf("expected 2 daily/hourly buckets, got %d", len(env.Evaluations.Daily))
	}
}

func TestEnrichOrphanedReference(t *testing.T) {
	m := newMockLD(t)
	m.server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r) // every flag GET 404s
	})
	details, err := Enrich(m.client(), scanWith("ghost-flag"), Options{
		ProjectKey: "default", Environments: []string{"production"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(details) != 1 || details[0].StaleState != "orphanedReference" {
		t.Fatalf("expected orphanedReference, got %+v", details)
	}
}
