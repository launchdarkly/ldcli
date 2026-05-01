package analytics

import "log"

type LogClientFn struct{}

func (fn LogClientFn) Tracker(_ string, _ string, _ bool) Tracker {
	return &LogClient{}
}

type LogClient struct{}

func (c *LogClient) SendCommandRunEvent(properties map[string]interface{}) {
	log.Printf("SendCommandRunEvent, properties: %v", properties)
}
func (c *LogClient) SendCommandCompletedEvent(outcome string) {
	log.Printf("SendCommandCompletedEvent, outcome: %v", outcome)
}
func (a *LogClient) Wait() {}
