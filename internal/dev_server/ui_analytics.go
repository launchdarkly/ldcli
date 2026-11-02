package dev_server

import (
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"sync"
	"time"

	"github.com/launchdarkly/ldcli/internal/analytics"
)

const (
	uiAnalyticsPath        = "/ui-analytics"
	uiAnalyticsMaxBody     = 2048
	uiAnalyticsPerMinute   = 60
	uiAnalyticsContentType = "application/json"
)

type uiAnalyticsPropertyKind int

const (
	uiAnalyticsString uiAnalyticsPropertyKind = iota
	uiAnalyticsBool
)

type uiAnalyticsProperty struct {
	kind    uiAnalyticsPropertyKind
	allowed []string
}

// uiAnalyticsEvents is the allowlist for browser analytics.
// An allowlist is a fixed list of permitted names.
var uiAnalyticsEvents = map[string]map[string]uiAnalyticsProperty{
	"Dev Server UI Page Viewed": {
		"page": {kind: uiAnalyticsString, allowed: []string{"flags", "events", "debug-sessions", "debug-session-events"}},
	},
	"Dev Server UI Flag Override Set": {
		"value_kind": {kind: uiAnalyticsString, allowed: []string{"boolean", "number", "string", "object", "array", "null"}},
		"control":    {kind: uiAnalyticsString, allowed: []string{"switch", "menu", "editor"}},
		"outcome":    {kind: uiAnalyticsString, allowed: []string{"success", "error"}},
	},
	"Dev Server UI Flag Override Removed": {
		"scope":   {kind: uiAnalyticsString, allowed: []string{"one", "all"}},
		"outcome": {kind: uiAnalyticsString, allowed: []string{"success", "error"}},
	},
	"Dev Server UI Project Added": {
		"outcome": {kind: uiAnalyticsString, allowed: []string{"success", "error"}},
	},
	"Dev Server UI Project Updated": {
		"changed": {kind: uiAnalyticsString, allowed: []string{"source_environment"}},
		"outcome": {kind: uiAnalyticsString, allowed: []string{"success", "error"}},
	},
	"Dev Server UI Project Synced": {
		"outcome": {kind: uiAnalyticsString, allowed: []string{"success", "error"}},
	},
	"Dev Server UI Project Removed": {
		"outcome": {kind: uiAnalyticsString, allowed: []string{"success", "error"}},
	},
	"Dev Server UI Project Imported": {
		"outcome": {kind: uiAnalyticsString, allowed: []string{"success", "error"}},
	},
	"Dev Server UI Context Updated": {
		"outcome": {kind: uiAnalyticsString, allowed: []string{"success", "error"}},
	},
	"Dev Server UI Events Stream Opened": {},
	"Dev Server UI Events Stream Changed": {
		"streaming": {kind: uiAnalyticsBool},
	},
}

type uiAnalyticsRequest struct {
	Event      string                 `json:"event"`
	Properties map[string]interface{} `json:"properties"`
}

type uiAnalyticsHandler struct {
	tracker analytics.Tracker
	mu      sync.Mutex
	hits    []time.Time
	now     func() time.Time
}

func newUIAnalyticsHandler(tracker analytics.Tracker) http.Handler {
	if tracker == nil {
		tracker = &analytics.NoopClient{}
	}
	return &uiAnalyticsHandler{
		tracker: tracker,
		now:     time.Now,
	}
}

func (h *uiAnalyticsHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	mediaType, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if mediaType != uiAnalyticsContentType {
		http.Error(w, "Set Content-Type to application/json.", http.StatusBadRequest)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, uiAnalyticsMaxBody)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		var maxBytes *http.MaxBytesError
		if errors.As(err, &maxBytes) {
			http.Error(w, "The request body is too large.", http.StatusBadRequest)
			return
		}
		http.Error(w, "The request body is not JSON.", http.StatusBadRequest)
		return
	}

	var payload uiAnalyticsRequest
	if err := json.Unmarshal(body, &payload); err != nil {
		http.Error(w, "The request body is not JSON.", http.StatusBadRequest)
		return
	}
	if payload.Properties == nil {
		payload.Properties = map[string]interface{}{}
	}
	if !uiAnalyticsAllowed(payload.Event, payload.Properties) {
		http.Error(w, "The event name or property is not allowed.", http.StatusBadRequest)
		return
	}
	if !h.allow() {
		http.Error(w, "Too many analytics events.", http.StatusTooManyRequests)
		return
	}

	h.tracker.SendDevServerUIEvent(payload.Event, payload.Properties)
	w.WriteHeader(http.StatusNoContent)
}

func uiAnalyticsAllowed(event string, properties map[string]interface{}) bool {
	spec, ok := uiAnalyticsEvents[event]
	if !ok {
		return false
	}
	if len(properties) != len(spec) {
		return false
	}
	for key, value := range properties {
		rule, ok := spec[key]
		if !ok {
			return false
		}
		switch rule.kind {
		case uiAnalyticsBool:
			if _, ok := value.(bool); !ok {
				return false
			}
		default:
			text, ok := value.(string)
			if !ok || !uiAnalyticsValueAllowed(rule.allowed, text) {
				return false
			}
		}
	}
	return true
}

func uiAnalyticsValueAllowed(allowed []string, value string) bool {
	for _, item := range allowed {
		if item == value {
			return true
		}
	}
	return false
}

func (h *uiAnalyticsHandler) allow() bool {
	h.mu.Lock()
	defer h.mu.Unlock()

	now := h.now()
	cutoff := now.Add(-time.Minute)
	kept := h.hits[:0]
	for _, hit := range h.hits {
		if hit.After(cutoff) {
			kept = append(kept, hit)
		}
	}
	h.hits = kept
	if len(h.hits) >= uiAnalyticsPerMinute {
		return false
	}
	h.hits = append(h.hits, now)
	return true
}
