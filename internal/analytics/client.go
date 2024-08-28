package analytics

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync"
	"time"
)

type ClientFn struct {
	ID      string
	Version string
}

func (fn ClientFn) Tracker(accessToken string, baseURI string, optOut bool) Tracker {
	if optOut {
		return &NoopClient{}
	}

	return &Client{
		httpClient: &http.Client{
			Timeout: time.Second * 3,
		},
		id:          fn.ID,
		version:     fn.Version,
		accessToken: accessToken,
		baseURI:     baseURI,
	}
}

type Client struct {
	accessToken string
	baseURI     string
	httpClient  *http.Client
	id          string
	version     string
	wg          sync.WaitGroup
}

// SendEvent makes an async request to track the given event with properties.
func (c *Client) sendEvent(eventName string, properties map[string]interface{}) {
	properties["id"] = c.id
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
	go func() {
		resp, err = c.httpClient.Do(req)
		if err != nil { //nolint:staticcheck
			// TODO: log error
		}
		if resp == nil {
			c.wg.Done()
			return
		}

		_, err = io.ReadAll(resp.Body)
		if err != nil { //nolint:staticcheck
			// TODO: log error
		}
		resp.Body.Close()
		c.wg.Done()
	}()
}

func (c *Client) SendCommandRunEvent(properties map[string]interface{}) {
	c.sendEvent(
		"CLI Command Run",
		properties,
	)
}

func (c *Client) SendCommandCompletedEvent(outcome string) {
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
