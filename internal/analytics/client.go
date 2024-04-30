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
	SendCommandRunEvent(
		accessToken,
		baseURI string,
		optOut bool,
		properties map[string]interface{},
	)
	SendCommandCompletedEvent(
		accessToken,
		baseURI string,
		optOut bool,
		outcome string,
	)
	SendSetupStartedEvent(
		accessToken,
		baseURI string,
		optOut bool,
		step string,
	)
}

type Client struct {
	ID           string
	HTTPClient   *http.Client
	sentRunEvent bool
	wg           sync.WaitGroup
}

// SendEvent makes an async request to track the given event with properties.
func (c *Client) sendEvent(
	accessToken string,
	baseURI string,
	optOut bool,
	eventName string,
	properties map[string]interface{},
) {
	if optOut {
		return
	}
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
	accessToken,
	baseURI string,
	optOut bool,
	properties map[string]interface{},
) {
	c.sendEvent(
		accessToken,
		baseURI,
		optOut,
		"CLI Command Run",
		properties,
	)
	if !optOut {
		c.sentRunEvent = true
	}
}

func (c *Client) SendCommandCompletedEvent(
	accessToken,
	baseURI string,
	optOut bool,
	outcome string,
) {
	if c.sentRunEvent {
		c.sendEvent(
			accessToken,
			baseURI,
			optOut,
			"CLI Command Completed",
			map[string]interface{}{
				"outcome": outcome,
			},
		)
	}
}

func (c *Client) SendSetupStartedEvent(
	accessToken,
	baseURI string,
	optOut bool,
	step string,
) {
	c.sendEvent(
		accessToken,
		baseURI,
		optOut,
		"CLI Setup Step Started",
		map[string]interface{}{
			"step": step,
		},
	)
}

func (a *Client) Wait() {
	a.wg.Wait()
}

type NoopClient struct{}

func (c *NoopClient) SendCommandRunEvent(
	accessToken,
	baseURI string,
	optOut bool,
	properties map[string]interface{},
) {
}

func (c *NoopClient) SendCommandCompletedEvent(
	accessToken,
	baseURI string,
	optOut bool,
	outcome string,
) {
}

func (c *NoopClient) SendSetupStartedEvent(
	accessToken,
	baseURI string,
	optOut bool,
	step string,
) {
}

type MockTracker struct {
	mock.Mock
	ID string
}

func (m *MockTracker) sendEvent(
	accessToken string,
	baseURI string,
	optOut bool,
	eventName string,
	properties map[string]interface{},
) {
	properties["id"] = m.ID
	m.Called(accessToken, baseURI, eventName, properties)
}

func (m *MockTracker) SendCommandRunEvent(
	accessToken,
	baseURI string,
	optOut bool,
	properties map[string]interface{},
) {
	m.sendEvent(
		accessToken,
		baseURI,
		optOut,
		"CLI Command Run",
		properties,
	)
}

func (m *MockTracker) SendCommandCompletedEvent(
	accessToken,
	baseURI string,
	optOut bool,
	outcome string,
) {
	m.sendEvent(
		accessToken,
		baseURI,
		optOut,
		"CLI Command Completed",
		map[string]interface{}{
			"outcome": outcome,
		},
	)
}

func (m *MockTracker) SendSetupStartedEvent(
	accessToken,
	baseURI string,
	optOut bool,
	step string,
) {
	m.sendEvent(
		accessToken,
		baseURI,
		optOut,
		"CLI Setup Step Started",
		map[string]interface{}{
			"step": step,
		},
	)
}
