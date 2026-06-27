package analytics

type NoopClientFn struct{}

func (fn NoopClientFn) Tracker() TrackerFn {
	return func(_ string, _ string, _ bool) Tracker {
		return &NoopClient{}
	}
}

type NoopClient struct{}

func (c *NoopClient) SendCommandRunEvent(properties map[string]interface{}) {}
func (c *NoopClient) SendCommandCompletedEvent(outcome string)              {}
func (a *NoopClient) Wait()                                                 {}
