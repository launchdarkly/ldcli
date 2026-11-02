package analytics

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync"
	"sync/atomic"
	"time"
)

const maxUIAnalyticsInFlight = 10

type ClientFn struct {
	ID           string
	Version      string
	AgentContext string
}

func (fn ClientFn) Tracker(accessToken string, baseURI string, optOut bool) Tracker {
	if optOut {
		return &NoopClient{}
	}

	return &Client{
		httpClient: &http.Client{
			Timeout: time.Second * 3,
		},
		id:           fn.ID,
		version:      fn.Version,
		accessToken:  accessToken,
		baseURI:      baseURI,
		agentContext: fn.AgentContext,
	}
}

type Client struct {
	accessToken  string
	agentContext string
	baseURI      string
	httpClient   *http.Client
	id           string
	version      string
	wg           sync.WaitGroup
	uiInFlight   atomic.Int32
}

// sendEvent makes an async request to track the given event with properties.
func (c *Client) sendEvent(eventName string, properties map[string]interface{}, release func()) {
	started := false
	if release != nil {
		defer func() {
			if !started {
				release()
			}
		}()
	}

	properties["id"] = c.id
	if c.agentContext != "" {
		properties["agent_context"] = c.agentContext
	}
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

	path, _ := url.JoinPath(
		c.baseURI,
		"internal/tracking",
	)
	req, err := http.NewRequest("POST", path, bytes.NewBuffer(body))
	if err != nil { //nolint:staticcheck
		// TODO: log error
		c.wg.Done()
		return
	}

	req.Header.Add("Authorization", c.accessToken)
	req.Header.Add("Content-Type", "application/json")
	req.Header.Add("User-Agent", fmt.Sprintf("launchdarkly-cli/%s", c.version))
	var resp *http.Response
	started = true
	go func() {
		defer c.wg.Done()
		if release != nil {
			defer release()
		}
		resp, err = c.httpClient.Do(req)
		if err != nil { //nolint:staticcheck
			// TODO: log error
		}
		if resp == nil {
			return
		}

		_, err = io.ReadAll(resp.Body)
		if err != nil { //nolint:staticcheck
			// TODO: log error
		}
		resp.Body.Close()
	}()
}

func (c *Client) reserveUIAnalyticsSlot() bool {
	for {
		current := c.uiInFlight.Load()
		if current >= maxUIAnalyticsInFlight {
			return false
		}
		if c.uiInFlight.CompareAndSwap(current, current+1) {
			return true
		}
	}
}

func (c *Client) releaseUIAnalyticsSlot() {
	c.uiInFlight.Add(-1)
}

func (c *Client) SendCommandRunEvent(properties map[string]interface{}) {
	c.sendEvent(
		"CLI Command Run",
		properties,
		nil,
	)
}

func (c *Client) SendCommandCompletedEvent(outcome string) {
	c.sendEvent(
		"CLI Command Completed",
		map[string]interface{}{
			"outcome": outcome,
		},
		nil,
	)
}

func (c *Client) SendSetupStepStartedEvent(step string) {
	c.sendEvent(
		"CLI Setup Step Started",
		map[string]interface{}{
			"step": step,
		},
		nil,
	)
}

func (c *Client) SendSetupSDKSelectedEvent(sdk string) {
	c.sendEvent(
		"CLI Setup SDK Selected",
		map[string]interface{}{
			"sdk": sdk,
		},
		nil,
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
		nil,
	)
}

func (c *Client) SendDevServerUIEvent(name string, properties map[string]interface{}) {
	if !c.reserveUIAnalyticsSlot() {
		return
	}
	c.sendEvent(name, properties, c.releaseUIAnalyticsSlot)
}

func (a *Client) Wait() {
	a.wg.Wait()
}
