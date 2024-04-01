package analytics

type MockClient struct{}

func (c MockClient) Track(userID string, traits map[string]interface{}) error {
	return nil
}

func (c MockClient) Close() error {
	return nil
}

var _ AnalyticsTracker = &MockClient{}
