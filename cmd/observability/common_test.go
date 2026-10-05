package observability

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseTime(t *testing.T) {
	now := time.Date(2024, 1, 15, 12, 0, 0, 0, time.UTC)

	tests := map[string]time.Time{
		"":                          now,
		"now":                       now,
		"2024-01-15T10:00:00Z":      time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC),
		"2024-01-15T10:00:00-08:00": time.Date(2024, 1, 15, 18, 0, 0, 0, time.UTC),
		"2024-01-10":                time.Date(2024, 1, 10, 0, 0, 0, 0, time.UTC),
		"30m":                       now.Add(-30 * time.Minute),
		"24h":                       now.Add(-24 * time.Hour),
		"7d":                        now.AddDate(0, 0, -7),
	}
	for in, want := range tests {
		t.Run(in, func(t *testing.T) {
			got, err := parseTime(in, now)
			require.NoError(t, err)
			assert.Equal(t, want, got)
		})
	}

	for _, in := range []string{"yesterday", "-1h", "0d", "1y"} {
		t.Run("invalid "+in, func(t *testing.T) {
			_, err := parseTime(in, now)
			assert.Error(t, err)
		})
	}
}

func TestLookupAttr(t *testing.T) {
	flat := map[string]interface{}{"feature_flag.key": "a"}
	nested := map[string]interface{}{"feature_flag": map[string]interface{}{"result": map[string]interface{}{"value": true}}}

	assert.Equal(t, "a", lookupAttr(flat, "feature_flag.key"))
	assert.Equal(t, true, lookupAttr(nested, "feature_flag.result.value"))
	assert.Nil(t, lookupAttr(nested, "feature_flag.key"))
}
