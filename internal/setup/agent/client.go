package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// serverUsername is the basic auth user name that OpenCode expects when
// OPENCODE_SERVER_PASSWORD is set and OPENCODE_SERVER_USERNAME is not.
const serverUsername = "opencode"

// client calls the HTTP API of one OpenCode server.
type client struct {
	baseURL  string
	password string
	http     *http.Client
}

func (c *client) newRequest(ctx context.Context, method, path string, body any) (*http.Request, error) {
	var r io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		r = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, r)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.SetBasicAuth(serverUsername, c.password)
	return req, nil
}

// do sends a request and decodes a JSON response into out, if out is not nil.
func (c *client) do(ctx context.Context, method, path string, body, out any) error {
	req, err := c.newRequest(ctx, method, path, body)
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return &statusError{method: method, path: path, code: resp.StatusCode, body: strings.TrimSpace(string(msg))}
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

type statusError struct {
	method, path string
	code         int
	body         string
}

func (e *statusError) Error() string {
	if e.body == "" {
		return fmt.Sprintf("OpenCode returned HTTP %d for %s %s", e.code, e.method, e.path)
	}
	return fmt.Sprintf("OpenCode returned HTTP %d for %s %s: %s", e.code, e.method, e.path, e.body)
}

func (c *client) createSession(ctx context.Context) (string, error) {
	var s struct {
		ID string `json:"id"`
	}
	if err := c.do(ctx, http.MethodPost, "/session", map[string]string{"title": "ldcli setup"}, &s); err != nil {
		return "", err
	}
	if s.ID == "" {
		return "", fmt.Errorf("OpenCode created a session without an ID")
	}
	return s.ID, nil
}

// promptAsync sends the task and returns when OpenCode accepts it. The reply
// arrives on the event stream.
func (c *client) promptAsync(ctx context.Context, sessionID string, cfg Config, text string) error {
	body := map[string]any{
		"parts": []map[string]string{{"type": "text", "text": text}},
	}
	if cfg.Instructions != "" {
		body["system"] = cfg.Instructions
	}
	if cfg.ProviderID != "" && cfg.ModelID != "" {
		body["model"] = map[string]string{"providerID": cfg.ProviderID, "modelID": cfg.ModelID}
	}
	return c.do(ctx, http.MethodPost, "/session/"+url.PathEscape(sessionID)+"/prompt_async", body, nil)
}

func (c *client) replyPermission(ctx context.Context, req PermissionRequest, allow bool) error {
	reply := "reject"
	if allow {
		reply = "once"
	}
	if req.legacy {
		path := "/session/" + url.PathEscape(req.SessionID) + "/permissions/" + url.PathEscape(req.ID)
		return c.do(ctx, http.MethodPost, path, map[string]string{"response": reply}, nil)
	}
	return c.do(ctx, http.MethodPost, "/permission/"+url.PathEscape(req.ID)+"/reply", map[string]string{"reply": reply}, nil)
}

// answerQuestion replies to a question request, or dismisses it when answers
// is nil.
func (c *client) answerQuestion(ctx context.Context, req QuestionRequest, answers [][]string) error {
	path := "/question/" + url.PathEscape(req.ID)
	if answers == nil {
		return c.do(ctx, http.MethodPost, path+"/reject", map[string]any{}, nil)
	}
	return c.do(ctx, http.MethodPost, path+"/reply", map[string]any{"answers": answers}, nil)
}

// subscribe opens the event stream. The caller must close the body.
func (c *client) subscribe(ctx context.Context) (io.ReadCloser, error) {
	req, err := c.newRequest(ctx, http.MethodGet, "/event", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "text/event-stream")
	// The stream stays open for the whole run, so it must not use the client
	// timeout of the other calls.
	resp, err := (&http.Client{Transport: c.http.Transport}).Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, &statusError{method: http.MethodGet, path: "/event", code: resp.StatusCode}
	}
	return resp.Body, nil
}
