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
	SendSetupStepStartedEvent(
		accessToken,
		baseURI string,
		optOut bool,
		step string,
	)
	SendSetupSDKSelectedEvent(
		accessToken,
		baseURI string,
		optOut bool,
		sdk string,
	)
	SendSetupFlagToggledEvent(
		accessToken,
		baseURI string,
		optOut,
		on bool,
		count int,
		duration_ms int64,
	)
}

type Client struct {
	ID            string
	HTTPClient    *http.Client
	Version       string
	sentHelpEvent bool
	sentRunEvent  bool
	wg            sync.WaitGroup
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
	req.Header.Add("User-Agent", fmt.Sprintf("launchdarkly-cli/%s", c.Version))
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
		action, ok := properties["action"]
		if ok && action == "help" {
			c.sentHelpEvent = true
		}
	}
}

func (c *Client) SendCommandCompletedEvent(
	accessToken,
	baseURI string,
	optOut bool,
	outcome string,
) {
	if c.sentRunEvent {
		if c.sentHelpEvent {
			outcome = HELP
		}

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

func (c *Client) SendSetupStepStartedEvent(
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

func (c *Client) SendSetupSDKSelectedEvent(
	accessToken,
	baseURI string,
	optOut bool,
	sdk string,
) {
	c.sendEvent(
		accessToken,
		baseURI,
		optOut,
		"CLI Setup SDK Selected",
		map[string]interface{}{
			"sdk": sdk,
		},
	)
}

func (c *Client) SendSetupFlagToggledEvent(
	accessToken,
	baseURI string,
	optOut,
	on bool,
	count int,
	duration_ms int64,
) {
	c.sendEvent(
		accessToken,
		baseURI,
		optOut,
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

func (c *NoopClient) SendSetupStepStartedEvent(
	accessToken,
	baseURI string,
	optOut bool,
	step string,
) {
}

func (c *NoopClient) SendSetupSDKSelectedEvent(
	accessToken,
	baseURI string,
	optOut bool,
	sdk string,
) {
}

func (c *NoopClient) SendSetupFlagToggledEvent(
	accessToken,
	baseURI string,
	optOut,
	on bool,
	count int,
	duration_ms int64,
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

func (m *MockTracker) SendSetupStepStartedEvent(
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

func (m *MockTracker) SendSetupSDKSelectedEvent(
	accessToken,
	baseURI string,
	optOut bool,
	sdk string,
) {
	m.sendEvent(
		accessToken,
		baseURI,
		optOut,
		"CLI Setup SDK Selected",
		map[string]interface{}{
			"sdk": sdk,
		},
	)
}

func (m *MockTracker) SendSetupFlagToggledEvent(
	accessToken,
	baseURI string,
	optOut,
	on bool,
	count int,
	duration_ms int64,
) {
	m.sendEvent(
		accessToken,
		baseURI,
		optOut,
		"CLI Setup Flag Toggled",
		map[string]interface{}{
			"on":          on,
			"count":       count,
			"duration_ms": duration_ms,
		},
	)
}
