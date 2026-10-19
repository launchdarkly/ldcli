package prompt

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
	syncapi "github.com/launchdarkly/ldcli/internal/sync/api"
)

func TestReportResource(t *testing.T) {
	id := syncdomain.VariationID("production", "support", "default")
	tests := map[string]struct {
		outcome  ResourceOutcome
		expected syncapi.SyncReportResource
	}{
		"succeeded with version": {
			outcome:  ResourceOutcome{ID: id, Status: OutcomeSucceeded, Version: 7},
			expected: syncapi.SyncReportResource{Status: syncapi.ReportStatusSucceeded, Version: 7},
		},
		"failed API error sends only its message": {
			outcome: ResourceOutcome{
				ID: id, Status: OutcomeFailed, Version: 7,
				Error: `{"code":"invalid_request","message":"update config variation \"default\": Invalid key ''","statusCode":400}`,
			},
			expected: syncapi.SyncReportResource{
				Status: syncapi.ReportStatusFailed, Error: `update config variation "default": Invalid key ''`,
			},
		},
		"skipped is a failure": {
			outcome:  ResourceOutcome{ID: id, Status: OutcomeSkipped, Error: "resource is not executable"},
			expected: syncapi.SyncReportResource{Status: syncapi.ReportStatusFailed, Error: "resource is not executable"},
		},
		"failure without text": {
			outcome:  ResourceOutcome{ID: id, Status: OutcomeFailed},
			expected: syncapi.SyncReportResource{Status: syncapi.ReportStatusFailed, Error: "sync did not apply the change"},
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			test.expected.ResourceKind, test.expected.ResourceLookupKey = syncdomain.KindVariation, "support/default"
			assert.Equal(t, test.expected, reportResource(test.outcome))
		})
	}
}

func TestReportResourceLimitsErrorLength(t *testing.T) {
	outcome := ResourceOutcome{
		ID:     syncdomain.VariationID("production", "support", "default"),
		Status: OutcomeFailed,
		Error:  strings.Repeat("é", 1500),
	}

	assert.Equal(t, strings.Repeat("é", 1000), reportResource(outcome).Error)
}
