package analytics

type NoopClientFn struct{}

func (fn NoopClientFn) Tracker() TrackerFn {
	return func(_ string, _ string, _ bool) Tracker {
		return &NoopClient{}
	}
}

type NoopClient struct{}

func (c *NoopClient) SendCommandRunEvent(properties map[string]interface{})           {}
func (c *NoopClient) SendCommandCompletedEvent(outcome string)                        {}
func (c *NoopClient) SendSetupStepStartedEvent(step string)                           {}
func (c *NoopClient) SendSetupSDKSelectedEvent(sdk string)                            {}
func (c *NoopClient) SendSetupFlagToggledEvent(on bool, count int, duration_ms int64) {}
func (a *NoopClient) Wait()                                                           {}
