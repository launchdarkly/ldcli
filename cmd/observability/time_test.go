package observability

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseTime(t *testing.T) {
	now := time.Date(2026, 10, 29, 20, 0, 0, 0, time.UTC)

	for input, want := range map[string]time.Time{
		"":                          now,
		"now":                       now,
		"15m":                       now.Add(-15 * time.Minute),
		"-15m":                      now.Add(-15 * time.Minute),
		"2h30m":                     now.Add(-150 * time.Minute),
		"7d":                        now.Add(-7 * 24 * time.Hour),
		"1.5d":                      now.Add(-36 * time.Hour),
		"2w":                        now.Add(-14 * 24 * time.Hour),
		"2026-10-28T10:00:00Z":      time.Date(2026, 10, 28, 10, 0, 0, 0, time.UTC),
		"2026-10-28T10:00:00+02:00": time.Date(2026, 10, 28, 8, 0, 0, 0, time.UTC),
		"2026-10-28T10:00:00":       time.Date(2026, 10, 28, 10, 0, 0, 0, time.UTC),
		"2026-10-28":                time.Date(2026, 10, 28, 0, 0, 0, 0, time.UTC),
	} {
		got, err := parseTime(input, now)
		require.NoError(t, err, input)
		assert.True(t, want.Equal(got), "%q: want %s, got %s", input, want, got)
	}

	for _, input := range []string{"-", "yesterday", "5x", "d", "-5-d"} {
		_, err := parseTime(input, now)
		assert.Error(t, err, input)
	}
}
