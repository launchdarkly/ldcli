package agent

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"strings"
)

// rawEvent is one OpenCode bus event as the /event stream sends it.
type rawEvent struct {
	Type       string          `json:"type"`
	Properties json.RawMessage `json:"properties"`
}

// decoder turns OpenCode bus events into Events for one session. OpenCode
// sends events for every session on the server, so the decoder drops the
// events of other sessions. It also drops repeats: OpenCode sends a part again
// each time that the part changes.
type decoder struct {
	sessionID string
	// partTypes records the type of each part, because a text delta does not
	// say whether it belongs to a text part or a reasoning part.
	partTypes map[string]string
	// toolStatus records the last status sent for each tool call.
	toolStatus map[string]string
	usageSent  map[string]bool
	finished   bool
}

func newDecoder(sessionID string) *decoder {
	return &decoder{
		sessionID:  sessionID,
		partTypes:  map[string]string{},
		toolStatus: map[string]string{},
		usageSent:  map[string]bool{},
	}
}

// decode returns the Events for one raw event. After the decoder returns
// EventDone or EventError, it returns nothing.
func (d *decoder) decode(data []byte) []Event {
	if d.finished {
		return nil
	}
	var ev rawEvent
	if err := json.Unmarshal(data, &ev); err != nil {
		return nil
	}
	var out []Event
	switch ev.Type {
	case "message.part.updated":
		out = d.partUpdated(ev.Properties)
	case "message.part.delta":
		out = d.partDelta(ev.Properties)
	case "message.updated":
		out = d.messageUpdated(ev.Properties)
	case "permission.asked":
		out = d.permissionAsked(ev.Properties)
	case "permission.updated":
		out = d.legacyPermission(ev.Properties)
	case "file.edited":
		var p struct {
			File string `json:"file"`
		}
		if json.Unmarshal(ev.Properties, &p) == nil && p.File != "" {
			out = []Event{{Kind: EventFileEdited, File: p.File}}
		}
	case "session.idle":
		var p struct {
			SessionID string `json:"sessionID"`
		}
		if json.Unmarshal(ev.Properties, &p) == nil && p.SessionID == d.sessionID {
			out = []Event{{Kind: EventDone}}
		}
	case "session.error":
		out = d.sessionError(ev.Properties)
	}
	for _, e := range out {
		if e.Kind == EventDone || e.Kind == EventError {
			d.finished = true
		}
	}
	return out
}

type part struct {
	ID        string `json:"id"`
	SessionID string `json:"sessionID"`
	Type      string `json:"type"`
	CallID    string `json:"callID"`
	Tool      string `json:"tool"`
	State     struct {
		Status string `json:"status"`
		Title  string `json:"title"`
	} `json:"state"`
}

func (d *decoder) partUpdated(props json.RawMessage) []Event {
	var p struct {
		Part part `json:"part"`
		// Delta is set by OpenCode versions that send text changes on the part
		// update and not as a separate delta event.
		Delta string `json:"delta"`
	}
	if json.Unmarshal(props, &p) != nil || p.Part.SessionID != d.sessionID {
		return nil
	}
	d.partTypes[p.Part.ID] = p.Part.Type
	switch p.Part.Type {
	case "text":
		if p.Delta != "" {
			return []Event{{Kind: EventText, Text: p.Delta}}
		}
	case "tool":
		status := p.Part.State.Status
		// A pending call has no input yet, so there is nothing to show.
		if status == "pending" || status == "" || d.toolStatus[p.Part.CallID] == status {
			return nil
		}
		d.toolStatus[p.Part.CallID] = status
		return []Event{{Kind: EventTool, Tool: ToolCall{
			ID:     p.Part.CallID,
			Name:   p.Part.Tool,
			Status: status,
			Title:  p.Part.State.Title,
		}}}
	}
	return nil
}

func (d *decoder) partDelta(props json.RawMessage) []Event {
	var p struct {
		SessionID string `json:"sessionID"`
		PartID    string `json:"partID"`
		Field     string `json:"field"`
		Delta     string `json:"delta"`
	}
	if json.Unmarshal(props, &p) != nil || p.SessionID != d.sessionID {
		return nil
	}
	// Show only the reply text, not the model's reasoning.
	if p.Field != "text" || d.partTypes[p.PartID] != "text" || p.Delta == "" {
		return nil
	}
	return []Event{{Kind: EventText, Text: p.Delta}}
}

func (d *decoder) messageUpdated(props json.RawMessage) []Event {
	var p struct {
		Info struct {
			ID         string  `json:"id"`
			SessionID  string  `json:"sessionID"`
			Role       string  `json:"role"`
			ProviderID string  `json:"providerID"`
			ModelID    string  `json:"modelID"`
			Cost       float64 `json:"cost"`
			Time       struct {
				Completed float64 `json:"completed"`
			} `json:"time"`
			Tokens struct {
				Input  int `json:"input"`
				Output int `json:"output"`
			} `json:"tokens"`
		} `json:"info"`
	}
	if json.Unmarshal(props, &p) != nil {
		return nil
	}
	info := p.Info
	if info.SessionID != d.sessionID || info.Role != "assistant" || info.Time.Completed == 0 || d.usageSent[info.ID] {
		return nil
	}
	d.usageSent[info.ID] = true
	return []Event{{Kind: EventUsage, Usage: Usage{
		ProviderID:   info.ProviderID,
		ModelID:      info.ModelID,
		InputTokens:  info.Tokens.Input,
		OutputTokens: info.Tokens.Output,
		Cost:         info.Cost,
	}}}
}

func (d *decoder) permissionAsked(props json.RawMessage) []Event {
	var p struct {
		ID         string   `json:"id"`
		SessionID  string   `json:"sessionID"`
		Permission string   `json:"permission"`
		Patterns   []string `json:"patterns"`
	}
	if json.Unmarshal(props, &p) != nil || p.SessionID != d.sessionID {
		return nil
	}
	return []Event{{Kind: EventPermission, Permission: PermissionRequest{
		ID:         p.ID,
		SessionID:  p.SessionID,
		Permission: p.Permission,
		Patterns:   p.Patterns,
	}}}
}

// legacyPermission reads the permission event of OpenCode versions before
// permission.asked. Its pattern is a string or a list of strings.
func (d *decoder) legacyPermission(props json.RawMessage) []Event {
	var p struct {
		ID        string          `json:"id"`
		SessionID string          `json:"sessionID"`
		Type      string          `json:"type"`
		Pattern   json.RawMessage `json:"pattern"`
		Title     string          `json:"title"`
	}
	if json.Unmarshal(props, &p) != nil || p.SessionID != d.sessionID {
		return nil
	}
	var patterns []string
	var one string
	if json.Unmarshal(p.Pattern, &one) == nil && one != "" {
		patterns = []string{one}
	} else {
		_ = json.Unmarshal(p.Pattern, &patterns)
	}
	if len(patterns) == 0 && p.Title != "" {
		patterns = []string{p.Title}
	}
	return []Event{{Kind: EventPermission, Permission: PermissionRequest{
		ID:         p.ID,
		SessionID:  p.SessionID,
		Permission: p.Type,
		Patterns:   patterns,
		legacy:     true,
	}}}
}

func (d *decoder) sessionError(props json.RawMessage) []Event {
	var p struct {
		SessionID string `json:"sessionID"`
		Error     struct {
			Name string `json:"name"`
			Data struct {
				Message string `json:"message"`
			} `json:"data"`
		} `json:"error"`
	}
	if json.Unmarshal(props, &p) != nil {
		return nil
	}
	// An error without a session ID can come from any session, but on a server
	// that ldcli started for one run, it belongs to this run.
	if p.SessionID != "" && p.SessionID != d.sessionID {
		return nil
	}
	msg := p.Error.Data.Message
	if msg == "" {
		msg = p.Error.Name
	}
	if msg == "" {
		msg = "OpenCode reported an error."
	}
	return []Event{{Kind: EventError, Text: msg}}
}

// readSSE reads a server-sent event stream and calls fn with the data of each
// event. It stops when fn returns false or the stream ends.
func readSSE(r io.Reader, fn func(data []byte) bool) error {
	scanner := bufio.NewScanner(r)
	// A part update can carry a whole file diff, which is longer than the
	// scanner's default line limit.
	scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	var data bytes.Buffer
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			if data.Len() > 0 {
				if !fn(data.Bytes()) {
					return nil
				}
				data.Reset()
			}
			continue
		}
		value, ok := strings.CutPrefix(line, "data:")
		if !ok {
			continue // event names, IDs, and comments carry nothing that we use
		}
		if data.Len() > 0 {
			data.WriteByte('\n')
		}
		data.WriteString(strings.TrimPrefix(value, " "))
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	if data.Len() > 0 {
		fn(data.Bytes())
	}
	return nil
}
