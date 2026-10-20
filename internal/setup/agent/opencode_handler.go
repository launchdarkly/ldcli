package agent

import (
	"context"
	"errors"
	"iter"
	"strings"

	"go.opentelemetry.io/otel/trace"

	"github.com/launchdarkly/go-ai-sdk/aiserver"
)

// The OpenCode handler is an AI SDK provider handler, like the Claude and
// OpenAI agent handlers in go-ai-sdk. The AI SDK resolves the AI Config and
// calls the handler, the handler runs one OpenCode session through
// SessionEvents, and the AI SDK records the run against the variation. It is
// written so that it can move into go-ai-sdk as its own package with the same
// contract.
const (
	// HandlerTracer is the handler's instrumentation scope. The AI SDK
	// requires it to start with aiserver.TracerPrefix.
	HandlerTracer = aiserver.TracerPrefix + "/opencodeagents"
	// HandlerSpanName is the name of the root span of one run.
	HandlerSpanName = "opencode.session"
)

// SessionPrompt is what the handler sends to OpenCode for one run.
type SessionPrompt struct {
	// System is the system prompt from the config.
	System string
	// ProviderID and ModelID select the OpenCode model. Both are empty if the
	// config names no model, and then OpenCode uses its default model.
	ProviderID string
	ModelID    string
	// Text is the user message.
	Text string
}

// SessionEvents runs one OpenCode session for the prompt and returns its
// events. The channel must end after EventDone or EventError.
type SessionEvents func(ctx context.Context, p SessionPrompt) (<-chan Event, error)

// NewOpenCodeHandler returns an AI SDK handler for agent-mode configs of any
// provider. The caller owns the OpenCode process and supplies its events.
//
// The handler does not run judges. A config that has judges must be called
// with SkipJudges, because the handler would also be chosen for the judge
// configs, and it cannot run a judge prompt.
func NewOpenCodeHandler(events SessionEvents) *aiserver.ProviderHandler {
	h := openCodeHandler{events: events}
	return aiserver.CreateHandlerInternal(
		aiserver.ProvidesFor{Provider: aiserver.WildcardProvider, Mode: aiserver.ModeAgent},
		h.invoke, h.stream, false,
	)
}

type openCodeHandler struct {
	events SessionEvents
}

func (h openCodeHandler) invoke(ctx context.Context, req aiserver.HandlerRequest) (aiserver.HandlerResult, error) {
	var res aiserver.HandlerResult
	for ev, err := range h.stream(ctx, req) {
		if err != nil {
			return res, err
		}
		if ev.Type == aiserver.EventDone {
			if ev.Output != nil {
				res.Output = *ev.Output
			}
			res.Usage = ev.Usage
		}
	}
	return res, nil
}

func (h openCodeHandler) stream(ctx context.Context, req aiserver.HandlerRequest) iter.Seq2[aiserver.HandlerStreamEvent, error] {
	return func(yield func(aiserver.HandlerStreamEvent, error) bool) {
		p := aiserver.ComposePrompt(req, aiserver.ModeAgent)
		providerID, modelID := openCodeModel(req.Config)
		system := providerID
		if system == "" {
			system = "opencode"
		}
		run := aiserver.StartHandlerRun(ctx, aiserver.HandlerSpanOptions{
			Tracer:       HandlerTracer,
			Name:         HandlerSpanName,
			System:       system,
			Operation:    "invoke_agent",
			RequestModel: modelID,
		}, req, p)
		defer run.EndOnPanic()
		tools := toolSpans{run: run, open: map[string]trace.Span{}}

		fail := func(err error) {
			tools.endAll(err)
			run.Fail(err)
			yield(aiserver.HandlerStreamEvent{}, err)
		}

		events, err := h.events(run.Context(), SessionPrompt{
			System:     p.System,
			ProviderID: providerID,
			ModelID:    modelID,
			Text:       userText(p),
		})
		if err != nil {
			fail(err)
			return
		}

		var output strings.Builder
		var usage aiserver.SpanUsage
		responseModel := modelID
		for ev := range events {
			switch ev.Kind {
			case EventText:
				output.WriteString(ev.Text)
				if !yield(aiserver.HandlerStreamEvent{Type: aiserver.EventChunk, Text: ev.Text}, nil) {
					tools.endAll(nil)
					run.Abandon()
					return
				}
			case EventTool:
				tools.update(ev.Tool)
			case EventUsage:
				// OpenCode reports the usage of each finished reply, so each one
				// becomes a chat span with its own token counts.
				turn := aiserver.SpanUsage{Input: float64(ev.Usage.InputTokens), Output: float64(ev.Usage.OutputTokens)}
				if ev.Usage.ModelID != "" {
					responseModel = ev.Usage.ModelID
				}
				run.EndChat(run.StartChat(responseModel), responseModel, turn, "")
				usage.Add(turn)
			case EventDone:
				tools.endAll(nil)
				text := output.String()
				run.Finish(responseModel, text)
				yield(aiserver.HandlerStreamEvent{
					Type:   aiserver.EventDone,
					Output: &text,
					Usage:  map[string]any{"input": usage.Input, "output": usage.Output},
				}, nil)
				return
			case EventError:
				fail(errors.New(ev.Text))
				return
			}
		}
		fail(errors.New("the OpenCode session ended without a result"))
	}
}

// userText is the user message for OpenCode: the last user turn of the
// composed prompt. OpenCode keeps the conversation itself, so the handler
// sends only the new message.
func userText(p aiserver.Prompt) string {
	for i := len(p.Turns) - 1; i >= 0; i-- {
		if p.Turns[i].Role == "user" {
			return aiserver.ContentText(p.Turns[i])
		}
	}
	return ""
}

// toolSpans keeps one execute_tool span open for each running tool call.
type toolSpans struct {
	run  *aiserver.HandlerRun
	open map[string]trace.Span
}

func (t *toolSpans) update(tc ToolCall) {
	if _, ok := t.open[tc.ID]; !ok {
		t.open[tc.ID] = t.run.StartTool(tc.Name, tc.ID)
	}
	switch tc.Status {
	case "completed":
		t.end(tc.ID, nil)
	case "error":
		msg := tc.Title
		if msg == "" {
			msg = tc.Name + " failed"
		}
		t.end(tc.ID, errors.New(msg))
	}
}

func (t *toolSpans) end(id string, err error) {
	if span, ok := t.open[id]; ok {
		t.run.EndTool(span, err)
		delete(t.open, id)
	}
}

func (t *toolSpans) endAll(err error) {
	for id := range t.open {
		t.end(id, err)
	}
}
