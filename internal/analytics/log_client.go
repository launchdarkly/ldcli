package analytics

import "log"

type LogClientFn struct{}

func (fn LogClientFn) Tracker() TrackerFn {
	return func(_ string, _ string, _ bool) Tracker {
		return &LogClient{}
	}
}

type LogClient struct{}

func (c *LogClient) SendCommandRunEvent(properties map[string]interface{}) {
	log.Printf("SendCommandRunEvent, properties: %v", properties)
}
func (c *LogClient) SendCommandCompletedEvent(outcome string) {
	log.Printf("SendCommandCompletedEvent, outcome: %v", outcome)
}
func (c *LogClient) SendSetupStepStartedEvent(step string) {
	log.Printf("SendSetupStepStartedEvent, step: %v", step)
}
func (c *LogClient) SendSetupSDKSelectedEvent(sdk string) {
	log.Printf("SendSetupSDKSelectedEvent, sdk: %v", sdk)
}
func (c *LogClient) SendSetupFlagToggledEvent(on bool, count int, duration_ms int64) {
	log.Printf("SendSetupFlagToggledEvent, count: %v", count)
}
func (a *LogClient) Wait() {}
