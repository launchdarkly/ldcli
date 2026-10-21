package prompt

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
)

func TestReportResource(t *testing.T) {
	id := syncdomain.VariationID("production", "support", "default")
	tests := map[string]struct {
		outcome ResourceOutcome
		err     string
	}{
		"succeeded": {outcome: ResourceOutcome{ID: id, Status: OutcomeSucceeded}},
		"failed API error sends only its message": {
			outcome: ResourceOutcome{
				ID: id, Status: OutcomeFailed,
				Error: `{"code":"invalid_request","message":"update config variation \"default\": Invalid key ''","statusCode":400}`,
			},
			err: `update config variation "default": Invalid key ''`,
		},
		"skipped is a failure": {
			outcome: ResourceOutcome{ID: id, Status: OutcomeSkipped, Error: "resource is not executable"},
			err:     "resource is not executable",
		},
		"failure without text": {
			outcome: ResourceOutcome{ID: id, Status: OutcomeFailed},
			err:     "sync did not apply the change",
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			resource := reportResource(test.outcome)
			assert.Equal(t, id, resource.ID)
			assert.Equal(t, test.err, resource.Error)
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
