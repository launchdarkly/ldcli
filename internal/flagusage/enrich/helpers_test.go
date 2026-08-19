package enrich

import (
	"testing"
	"time"
)

func TestChunkStrings(t *testing.T) {
	in := []string{"a", "b", "c", "d", "e"}
	got := chunkStrings(in, 2)
	if len(got) != 3 || len(got[0]) != 2 || len(got[2]) != 1 {
		t.Fatalf("chunkStrings(%v,2) = %v", in, got)
	}
	if n := len(chunkStrings(in, 10)); n != 1 {
		t.Errorf("oversize chunk should yield 1 slice, got %d", n)
	}
	if n := len(chunkStrings(nil, 5)); n != 0 {
		t.Errorf("nil input should yield 0 chunks, got %d", n)
	}
}

func TestFormatWindow(t *testing.T) {
	cases := map[time.Duration]string{
		0:                  "7d", // default
		6 * time.Hour:      "6h",
		18 * time.Hour:     "18h",
		24 * time.Hour:     "1d",
		7 * 24 * time.Hour: "7d",
		90 * time.Minute:   "1h30m0s",
	}
	for d, want := range cases {
		if got := formatWindow(d); got != want {
			t.Errorf("formatWindow(%v) = %q, want %q", d, got, want)
		}
	}
}

func TestVariationLabels(t *testing.T) {
	labels := variationLabels([]apiVariation{
		{ID: "v0", Name: "On", Value: true},
		{ID: "v1", Value: false}, // no name → stringified value
	})
	if labels["v0"] != "On" {
		t.Errorf("named variation label = %q, want On", labels["v0"])
	}
	if labels["v1"] != "false" {
		t.Errorf("unnamed variation label = %q, want false", labels["v1"])
	}
}

// evalWindowRange must be stable within a bucket (so the request URL / cache key
// doesn't change between same-bucket runs) but advance across buckets, for both
// the daily (>=24h) and hourly (<24h) alignments.
func TestEvalWindowRangeStability(t *testing.T) {
	// Hourly alignment: two times in the same UTC hour → identical range.
	a := time.Date(2026, 6, 16, 9, 5, 0, 0, time.UTC)
	b := time.Date(2026, 6, 16, 9, 55, 0, 0, time.UTC)
	fa, ta := evalWindowRange(a, 6*time.Hour)
	fb, tb := evalWindowRange(b, 6*time.Hour)
	if fa != fb || ta != tb {
		t.Errorf("same-hour range unstable: (%d,%d) vs (%d,%d)", fa, ta, fb, tb)
	}
	if fa >= ta {
		t.Errorf("from must precede to: from=%d to=%d", fa, ta)
	}
	if ta-fa != (6 * time.Hour).Milliseconds() {
		t.Errorf("window width = %dms, want %dms", ta-fa, (6 * time.Hour).Milliseconds())
	}

	// Next hour → different range.
	c := time.Date(2026, 6, 16, 10, 5, 0, 0, time.UTC)
	fc, _ := evalWindowRange(c, 6*time.Hour)
	if fc == fa {
		t.Error("range should advance to the next hourly bucket")
	}

	// Daily alignment: two times on the same UTC day → identical range.
	d1 := time.Date(2026, 6, 16, 9, 0, 0, 0, time.UTC)
	d2 := time.Date(2026, 6, 16, 23, 0, 0, 0, time.UTC)
	f1, _ := evalWindowRange(d1, defaultEvalWindow)
	f2, _ := evalWindowRange(d2, defaultEvalWindow)
	if f1 != f2 {
		t.Error("same-day default-window range must be stable")
	}
}
