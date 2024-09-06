package analytics

type TrackerFn func(accessToken string, baseURI string, optOut bool) Tracker

type Tracker interface {
	SendCommandRunEvent(properties map[string]interface{})
	SendCommandCompletedEvent(outcome string)
	SendSetupStepStartedEvent(step string)
	SendSetupSDKSelectedEvent(sdk string)
	SendSetupFlagToggledEvent(on bool, count int, duration_ms int64)
	Wait()
}
