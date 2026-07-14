package render

import (
	"strings"
	"testing"
	"time"

	"github.com/launchdarkly/ldcli/internal/flagusage/enrich"
)

func intp(i int) *int { return &i }

// simpleEnv is a traffic-bearing on-toggle serving one variation.
func simpleEnv(total int64, variation int, status string) enrich.EnvStatus {
	return enrich.EnvStatus{
		On:          true,
		FlagStatus:  status,
		Evaluations: enrich.EvaluationCounts{Total7d: total},
		Targeting:   enrich.TargetingSummary{IsSimpleToggle: true, FallthroughVariation: intp(variation)},
	}
}

func TestRenderTable_HasBoxBordersAndSeparator(t *testing.T) {
	out := renderTable([]string{"A", "B"}, [][]string{
		{"one", "two"},
		nil, // separator
		{"three", "four"},
	}, 0)
	for _, glyph := range []string{"┌", "┬", "┐", "├", "┼", "┤", "└", "┴", "┘", "│", "─"} {
		if !strings.Contains(out, glyph) {
			t.Errorf("table missing box glyph %q\n%s", glyph, out)
		}
	}
	// Every rendered line should be the same display width (aligned box).
	var width int
	for i, line := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		w := len([]rune(line))
		if i == 0 {
			width = w
		} else if w != width {
			t.Errorf("line %d width %d != %d\n%s", i, w, width, out)
		}
	}
}

func TestRenderTable_FitsMaxWidth(t *testing.T) {
	const maxWidth = 40
	out := renderTable(
		[]string{"Flag", "Targeting"},
		[][]string{{"a-very-long-flag-key-that-overflows", "1 rules, 2 targets, 3 ctx-targets"}},
		maxWidth,
	)
	for i, line := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		if w := len([]rune(line)); w > maxWidth {
			t.Errorf("line %d width %d exceeds maxWidth %d\n%s", i, w, maxWidth, out)
		}
	}
	if !strings.Contains(out, "…") {
		t.Errorf("expected truncation ellipsis when shrinking to fit\n%s", out)
	}
}

func TestPrintEnriched_RemovalFlag_LifecycleAndRecommend(t *testing.T) {
	details := []enrich.FlagDetail{{
		Key:              "enable-foo",
		Temporary:        true,
		CallSites:        3,
		CreationDate:     time.Now().AddDate(0, 0, -62),
		StaleState:       "readyForCodeRemoval",
		Variations:       []enrich.Variation{{Value: true}, {Value: false}},
		RecommendedValue: true,
		Environments: map[string]enrich.EnvStatus{
			"production": simpleEnv(1_200_000, 0, "launched"),
			"staging":    simpleEnv(45_100, 0, "launched"),
		},
	}}

	var b strings.Builder
	Enriched(&b, details, "7d", []string{"production", "staging"}, 0)
	out := b.String()

	for _, want := range []string{
		"Lifecycle",      // leftmost combined column
		"code removable", // display label for readyForCodeRemoval
		"enable-foo",     // flag key on first env row
		"production", "staging",
		"launched",
		"1.2M",            // formatted eval count
		"serves true",     // simple-toggle serving variation 0 (=true)
		"hardcode true",   // recommendation, at the bottom of the Lifecycle cell
		"62d · 3× · temp", // meta continuation row
		"┌", "│",          // rendered as a box table
	} {
		if !strings.Contains(out, want) {
			t.Errorf("combined table missing %q\n%s", want, out)
		}
	}
	for _, absent := range []string{"(1)", "Recommend", "readyForCodeRemoval"} {
		if strings.Contains(out, absent) {
			t.Errorf("combined table should not contain %q\n%s", absent, out)
		}
	}
}

func TestLastEvalCell_RelativeDuration(t *testing.T) {
	if got := lastEvalCell(enrich.EnvStatus{}); got != "—" {
		t.Errorf("zero time: got %q, want —", got)
	}
	cases := []struct {
		ago  time.Duration
		want string
	}{
		{50 * time.Hour, "2d"},
		{3 * time.Hour, "3h"},
		{20 * time.Minute, "20m"},
		{10 * time.Second, "<1m"},
	}
	for _, c := range cases {
		got := lastEvalCell(enrich.EnvStatus{LastEvaluation: time.Now().Add(-c.ago)})
		if got != c.want {
			t.Errorf("%s ago: got %q, want %q", c.ago, got, c.want)
		}
	}
}

func TestPrintEnriched_OrphanedFlag_DashedRowInCombinedTable(t *testing.T) {
	details := []enrich.FlagDetail{{
		Key:        "ghost-flag",
		CallSites:  2,
		StaleState: "orphanedReference",
	}}
	var b strings.Builder
	Enriched(&b, details, "7d", []string{"production"}, 0)
	out := b.String()

	// Orphaned flags join the same table (Lifecycle=orphanedReference) with dashed
	// env cells rather than a separate note table.
	for _, want := range []string{"Lifecycle", "orphanedReference", "ghost-flag", "Last Eval", "—"} {
		if !strings.Contains(out, want) {
			t.Errorf("combined table missing %q\n%s", want, out)
		}
	}
}

func TestPrintEnriched_EnvAliasApplied(t *testing.T) {
	envAliases = map[string]string{"managed-eu-production": "EU prod"}
	defer func() { envAliases = map[string]string{} }()

	details := []enrich.FlagDetail{{
		Key: "a-flag", StaleState: "active",
		Environments: map[string]enrich.EnvStatus{"managed-eu-production": simpleEnv(10, 0, "active")},
	}}
	var b strings.Builder
	Enriched(&b, details, "7d", []string{"managed-eu-production"}, 0)
	out := b.String()

	if !strings.Contains(out, "EU prod") {
		t.Errorf("expected aliased env name 'EU prod'\n%s", out)
	}
	if strings.Contains(out, "managed-eu-production") {
		t.Errorf("raw env key should be replaced by its alias\n%s", out)
	}
}

func TestPrintEnriched_SortedByLifecycleThenKey(t *testing.T) {
	env := map[string]enrich.EnvStatus{"production": simpleEnv(10, 0, "active")}
	details := []enrich.FlagDetail{
		{Key: "zebra-active", StaleState: "active", Environments: env},
		{Key: "apple-active", StaleState: "active", Environments: env},
		{Key: "gamma-orphan", StaleState: "orphanedReference"},
	}
	var b strings.Builder
	Enriched(&b, details, "7d", []string{"production"}, 0)
	out := b.String()

	// orphanedReference outranks active; within active, apple before zebra.
	io, ia, iz := strings.Index(out, "gamma-orphan"), strings.Index(out, "apple-active"), strings.Index(out, "zebra-active")
	if !(io >= 0 && ia >= 0 && iz >= 0 && io < ia && ia < iz) {
		t.Errorf("expected order orphan<apple<zebra, got positions %d,%d,%d\n%s", io, ia, iz, out)
	}
}
