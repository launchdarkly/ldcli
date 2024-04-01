package analytics

type AnalyticsTracker interface {
	Track(userID string, traits map[string]interface{}) error
	Close() error
}
