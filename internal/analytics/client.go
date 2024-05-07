package analytics

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/davecgh/go-spew/spew"
	"github.com/stretchr/testify/mock"
)

type TrackerFn func(accessToken string, baseURI string, optOut bool) Tracker

type ClientFn struct {
	ID string
}

func (fn ClientFn) Tracker(version string) TrackerFn {
	return func(accessToken string, baseURI string, optOut bool) Tracker {
		if optOut {
			return &NoopClient{}
		}

		return &Client{
			HTTPClient: &http.Client{
				Timeout: time.Second * 3,
			},
			ID:          fn.ID,
			Version:     version,
			accessToken: accessToken,
			baseURI:     baseURI,
		}
	}
}

type NoopClientFn struct{}

func (fn NoopClientFn) Tracker() TrackerFn {
	return func(_ string, _ string, _ bool) Tracker {
		return &NoopClient{}
	}
}

type Tracker interface {
	SendCommandRunEvent(properties map[string]interface{})
	SendCommandCompletedEvent(outcome string)
	SendSetupStepStartedEvent(step string)
	SendSetupSDKSelectedEvent(sdk string)
	SendSetupFlagToggledEvent(on bool, count int, duration_ms int64)
	Wait()
}

type Client struct {
	ID            string
	HTTPClient    *http.Client
	Version       string
	sentHelpEvent bool
	wg            sync.WaitGroup

	accessToken string
	baseURI     string
}

// SendEvent makes an async request to track the given event with properties.
func (c *Client) sendEvent(eventName string, properties map[string]interface{}) {
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

	req, err := http.NewRequest("POST", fmt.Sprintf("%s/api/v2/tracking", c.baseURI), bytes.NewBuffer(body))
	if err != nil { //nolint:staticcheck
		// TODO: log error
		c.wg.Done()
		return
	}

	req.Header.Add("Authorization", c.accessToken)
	req.Header.Add("Content-Type", "application/json")
	req.Header.Add("User-Agent", fmt.Sprintf("launchdarkly-cli/%s", c.Version))
	var resp *http.Response
	go func() {
		fmt.Println(">>> sending event", c.accessToken, c.baseURI)
		spew.Dump(properties)
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

func (c *Client) SendCommandRunEvent(properties map[string]interface{}) {
	c.sendEvent(
		"CLI Command Run",
		properties,
	)
	action, ok := properties["action"]
	if ok && action == "help" {
		c.sentHelpEvent = true
	}
}

func (c *Client) SendCommandCompletedEvent(outcome string) {
	fmt.Println(">>> SendCommandCompletedEvent", outcome, c.sentHelpEvent)
	if c.sentHelpEvent {
		outcome = HELP
	}

	c.sendEvent(
		"CLI Command Completed",
		map[string]interface{}{
			"outcome": outcome,
		},
	)
}

func (c *Client) SendSetupStepStartedEvent(step string) {
	c.sendEvent(
		"CLI Setup Step Started",
		map[string]interface{}{
			"step": step,
		},
	)
}

func (c *Client) SendSetupSDKSelectedEvent(sdk string) {
	c.sendEvent(
		"CLI Setup SDK Selected",
		map[string]interface{}{
			"sdk": sdk,
		},
	)
}

func (c *Client) SendSetupFlagToggledEvent(on bool, count int, duration_ms int64) {
	c.sendEvent(
		"CLI Setup Flag Toggled",
		map[string]interface{}{
			"on":          on,
			"count":       count,
			"duration_ms": duration_ms,
		},
	)
}

func (a *Client) Wait() {
	a.wg.Wait()
}

type NoopClient struct{}

func (c *NoopClient) SendCommandRunEvent(properties map[string]interface{})           {}
func (c *NoopClient) SendCommandCompletedEvent(outcome string)                        {}
func (c *NoopClient) SendSetupStepStartedEvent(step string)                           {}
func (c *NoopClient) SendSetupSDKSelectedEvent(sdk string)                            {}
func (c *NoopClient) SendSetupFlagToggledEvent(on bool, count int, duration_ms int64) {}
func (a *NoopClient) Wait()                                                           {}

type MockTracker struct {
	mock.Mock
	ID string
}

func (m *MockTracker) sendEvent(eventName string, properties map[string]interface{}) {
	properties["id"] = m.ID
	m.Called(eventName, properties)
}

func (m *MockTracker) SendCommandRunEvent(properties map[string]interface{}) {
	m.sendEvent(
		"CLI Command Run",
		properties,
	)
}

func (m *MockTracker) SendCommandCompletedEvent(outcome string) {
	m.sendEvent(
		"CLI Command Completed",
		map[string]interface{}{
			"outcome": outcome,
		},
	)
}

func (m *MockTracker) SendSetupStepStartedEvent(step string) {
	m.sendEvent(
		"CLI Setup Step Started",
		map[string]interface{}{
			"step": step,
		},
	)
}

func (m *MockTracker) SendSetupSDKSelectedEvent(sdk string) {
	m.sendEvent(
		"CLI Setup SDK Selected",
		map[string]interface{}{
			"sdk": sdk,
		},
	)
}

func (m *MockTracker) SendSetupFlagToggledEvent(on bool, count int, duration_ms int64) {
	m.sendEvent(
		"CLI Setup Flag Toggled",
		map[string]interface{}{
			"on":          on,
			"count":       count,
			"duration_ms": duration_ms,
		},
	)
}

func (a *MockTracker) Wait() {}
