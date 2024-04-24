package analytics

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"

	"github.com/stretchr/testify/mock"
)

type Tracker interface {
	SendEvent(
		accessToken string,
		baseURI string,
		eventName string,
		properties map[string]interface{},
	)
	SendCommandRunEvent(
		name,
		accessToken,
		baseURI string,
		properties map[string]interface{},
	)
	SendCommandCompletedEvent(
		outcome,
		accessToken,
		baseURI string,
	)
}

type Client struct {
	ID         string
	HTTPClient *http.Client
	wg         sync.WaitGroup
}

// SendEvent makes an async request to track the given event with properties.
func (c *Client) SendEvent(
	accessToken string,
	baseURI string,
	eventName string,
	properties map[string]interface{},
) {
	properties["id"] = c.ID
	input := struct {
		Event      string                 `json:"event"`
		Properties map[string]interface{} `json:"properties"`
	}{
		Event:      eventName,
		Properties: properties,
	}

	c.wg.Add(1)
	body, err := json.Marshal(input)
	if err != nil { //nolint:staticcheck
		// TODO: log error
		c.wg.Done()
		return
	}

	req, err := http.NewRequest("POST", fmt.Sprintf("%s/api/v2/tracking", baseURI), bytes.NewBuffer(body))
	if err != nil { //nolint:staticcheck
		// TODO: log error
		c.wg.Done()
		return
	}

	req.Header.Add("Authorization", accessToken)
	req.Header.Add("Content-Type", "application/json")
	req.Header.Add("User-Agent", "launchdarkly-cli/v0.1.1")
	var resp *http.Response
	go func() {
		resp, err = c.HTTPClient.Do(req)
		if err != nil { //nolint:staticcheck
			// TODO: log error
		}
		if resp == nil {
			c.wg.Done()
			return
		}
		resp.Body.Close()

		_, err = io.ReadAll(resp.Body)
		if err != nil { //nolint:staticcheck
			// TODO: log error
		}
		c.wg.Done()
	}()
}

func (c *Client) SendCommandRunEvent(
	name,
	accessToken,
	baseURI string,
	properties map[string]interface{},
) {
	c.SendEvent(
		accessToken,
		baseURI,
		"CLI Command Run",
		properties,
	)
}

func (c *Client) SendCommandCompletedEvent(outcome, accessToken, baseURI string) {
	c.SendEvent(
		accessToken,
		baseURI,
		"CLI Command Completed",
		map[string]interface{}{
			"outcome": outcome,
		},
	)
}

func (a *Client) Wait() {
	a.wg.Wait()
}

type NoopClient struct{}

func (c *NoopClient) SendEvent(
	accessToken string,
	baseURI string,
	eventName string,
	properties map[string]interface{},
) {
}

func (c *NoopClient) SendCommandRunEvent(name,
	accessToken,
	baseURI string,
	properties map[string]interface{},
) {
}

func (c *NoopClient) SendCommandCompletedEvent(outcome, accessToken, baseURI string) {
}

type MockTracker struct {
	mock.Mock
	ID string
}

func (m *MockTracker) SendEvent(
	accessToken string,
	baseURI string,
	eventName string,
	properties map[string]interface{},
) {
	properties["id"] = m.ID
	m.Called(accessToken, baseURI, eventName, properties)
}

func (m *MockTracker) SendCommandRunEvent(name,
	accessToken,
	baseURI string,
	properties map[string]interface{},
) {
	m.SendEvent(
		accessToken,
		baseURI,
		"CLI Command Run",
		properties,
	)
}

func (m *MockTracker) SendCommandCompletedEvent(outcome, accessToken, baseURI string) {
	m.SendEvent(
		accessToken,
		baseURI,
		"CLI Command Completed",
		map[string]interface{}{
			"outcome": outcome,
		},
	)
}
