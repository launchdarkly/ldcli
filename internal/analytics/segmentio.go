package analytics

import "github.com/segmentio/analytics-go/v3"

type SegmentioClient struct {
	client analytics.Client
}

func NewSegmentioClient(client analytics.Client) SegmentioClient {
	return SegmentioClient{
		client: client,
	}
}

func (c SegmentioClient) Track(userID string, traits map[string]interface{}) error {
	return c.client.Enqueue(analytics.Identify{
		UserId: userID,
		Traits: traits,
	})
}

func (c SegmentioClient) Close() error {
	return c.client.Close()
}

var _ AnalyticsTracker = &SegmentioClient{}
