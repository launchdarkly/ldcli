package analytics

const (
	SUCCESS = "success"
	ERROR   = "error"
)

func MockedTracker(name string, action string, flags []string, outcome string) *MockTracker {
	id := "test-id"
	tracker := MockTracker{ID: id}
	tracker.On("SendEvent", []interface{}{
		"testAccessToken",
		"http://test.com",
		"CLI Command Run",
		map[string]interface{}{
			"action":  action,
			"baseURI": "http://test.com",
			"flags":   flags,
			"id":      id,
			"name":    name,
		},
	}...)
	tracker.On("SendEvent", []interface{}{
		"testAccessToken",
		"http://test.com",
		"CLI Command Completed",
		map[string]interface{}{
			"id":      id,
			"outcome": outcome,
		},
	}...)
	return &tracker
}
