package analytics

type TrackerFn func(accessToken string, baseURI string, optOut bool) Tracker

type Tracker interface {
	SendCommandRunEvent(properties map[string]interface{})
	SendCommandCompletedEvent(outcome string)
	Wait()
}
