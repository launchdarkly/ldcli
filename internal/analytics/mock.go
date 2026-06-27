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

func (a *MockTracker) Wait() {}
