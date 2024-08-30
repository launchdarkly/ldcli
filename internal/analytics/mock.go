package analytics

import "github.com/stretchr/testify/mock"

type MockTracker struct {
	mock.Mock
	ID string
}

func (m *MockTracker) sendEvent(eventName string, properties map[string]interface{}) {
	properties["id"] = m.ID
	m.Called(eventName, properties)
}

func (m *MockTracker) SendCommandRunEvent(properties map[string]interface{}) {
	m.sendEvent(
		"CLI Command Run",
		properties,
	)
}

func (m *MockTracker) SendCommandCompletedEvent(outcome string) {
	m.sendEvent(
		"CLI Command Completed",
		map[string]interface{}{
			"outcome": outcome,
		},
	)
}

func (m *MockTracker) SendSetupStepStartedEvent(step string) {
	m.sendEvent(
		"CLI Setup Step Started",
		map[string]interface{}{
			"step": step,
		},
	)
}

func (m *MockTracker) SendSetupSDKSelectedEvent(sdk string) {
	m.sendEvent(
		"CLI Setup SDK Selected",
		map[string]interface{}{
			"sdk": sdk,
		},
	)
}

func (m *MockTracker) SendSetupFlagToggledEvent(on bool, count int, duration_ms int64) {
	m.sendEvent(
		"CLI Setup Flag Toggled",
		map[string]interface{}{
			"on":          on,
			"count":       count,
			"duration_ms": duration_ms,
		},
	)
}

func (a *MockTracker) Wait() {}
