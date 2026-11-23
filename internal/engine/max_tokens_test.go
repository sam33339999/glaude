package engine

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"github.com/tunsuy/glaude/internal/api"
	"github.com/tunsuy/glaude/internal/tools"
	"github.com/tunsuy/glaude/pkg/types"
)

// cutAt rewrites a response's message_delta so it ends at max_tokens having
// produced outputTokens tokens.
func cutAt(events []*api.StreamEvent, outputTokens int) []*api.StreamEvent {
	for _, ev := range events {
		if ev.Type != api.EventMessageDelta {
			continue
		}
		ev.Data = mustMarshalRaw(map[string]any{
			"delta": map[string]any{"stop_reason": "max_tokens"},
			"usage": map[string]any{"output_tokens": outputTokens},
		})
		ev.MessageDelta.Delta.StopReason = "max_tokens"
		ev.MessageDelta.Usage = api.Usage{OutputTokens: outputTokens}
	}
	return events
}

// scriptedClient serves one response per request and records each request.
type scriptedClient struct {
	mu        sync.Mutex
	responses [][]*api.StreamEvent
	requests  []*api.MessageRequest
}

func (c *scriptedClient) Stream(_ context.Context, req *api.MessageRequest) (api.StreamReader, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.requests = append(c.requests, req)
	i := len(c.requests) - 1
	if i >= len(c.responses) {
		i = len(c.responses) - 1 // keep repeating the last response
	}
	return newStaticReader(c.responses[i]...), nil
}

func (c *scriptedClient) Complete(context.Context, *api.MessageRequest) (*api.MessageResponse, error) {
	return nil, nil
}

func runScripted(t *testing.T, c *scriptedClient, reg *tools.Registry) []Msg {
	t.Helper()
	if reg == nil {
		reg = tools.NewRegistry()
	}
	eng := New(Config{Client: c, Registry: reg, Model: "claude-test", MaxTokens: 8192})
	ch, err := eng.Query(context.Background(), QueryParams{
		Messages:       []types.Message{{Role: types.RoleUser, Content: []types.ContentBlock{{Type: types.ContentTypeText, Text: strPtr("go")}}}},
		ToolUseContext: &tools.UseContext{Ctx: context.Background()},
	})
	if err != nil {
		t.Fatal(err)
	}
	return drainMsgs(ch)
}

// lastMessageText returns the role and raw JSON of a request's last message.
func lastMessage(t *testing.T, req *api.MessageRequest) (string, string) {
	t.Helper()
	m := req.Messages[len(req.Messages)-1]
	return string(m.Role), string(m.Content)
}

// Two cuts in a row used to end the query silently: per-turn token counts
// were fed to a tracker that expects cumulative ones, so the second cut
// looked like "no progress". Each cut must get a resume nudge.
func TestMaxTokens_ConsecutiveCutsKeepResuming(t *testing.T) {
	c := &scriptedClient{responses: [][]*api.StreamEvent{
		cutAt(buildEndTurnEvents("part one"), 8192),
		cutAt(buildEndTurnEvents("part two"), 8192),
		buildEndTurnEvents("part three"),
	}}
	runScripted(t, c, nil)

	if len(c.requests) != 3 {
		t.Fatalf("requests = %d, want 3 (resume after each cut)", len(c.requests))
	}
	for _, req := range c.requests[1:] {
		role, content := lastMessage(t, req)
		if role != "user" || !strings.Contains(content, "do not restart or repeat") && !strings.Contains(content, "without repeating") {
			t.Errorf("request after a cut ends with %s %s, want a resume nudge", role, content)
		}
	}
}

// A provider that reports no usage must not make the loop send requests
// ending in an assistant message forever: every cut counts.
func TestMaxTokens_NoUsageStillBounded(t *testing.T) {
	c := &scriptedClient{responses: [][]*api.StreamEvent{
		cutAt(buildEndTurnEvents("cut"), 0),
	}}
	msgs := runScripted(t, c, nil)

	if got, max := len(c.requests), MaxContinuations+1; got > max {
		t.Fatalf("requests = %d, want at most %d", got, max)
	}
	for i, req := range c.requests[1:] {
		if role, _ := lastMessage(t, req); role != "user" {
			t.Errorf("request %d ends with a %s message, want the nudge", i+2, role)
		}
	}
	if m, ok := findMsg(msgs, MsgTypeSystemMessage); !ok || !strings.Contains(m.SystemText, "output token limit") {
		t.Errorf("want a system message saying the response stopped at the limit, got %+v", m)
	}
}

// A cut-off tool call is never run and must not stay in the history: a
// tool_use with no tool_result makes the next request fail.
func TestMaxTokens_CutToolCallDropped(t *testing.T) {
	reg := tools.NewRegistry()
	tool := &callCountTool{stubTool: stubTool{name: "Write"}}
	reg.Register(tool)

	c := &scriptedClient{responses: [][]*api.StreamEvent{
		cutAt(buildToolUseEvents("tu_cut", "Write", `{"path":"/tmp/x"}`), 8192),
		buildEndTurnEvents("done"),
	}}
	runScripted(t, c, reg)

	if tool.calls.Load() != 0 {
		t.Fatal("the cut-off tool call was executed")
	}
	if len(c.requests) != 2 {
		t.Fatalf("requests = %d, want 2", len(c.requests))
	}
	raw, _ := json.Marshal(c.requests[1].Messages)
	if strings.Contains(string(raw), "tu_cut") {
		t.Errorf("next request still carries the cut-off tool call: %s", raw)
	}
	if _, content := lastMessage(t, c.requests[1]); !strings.Contains(content, "were not run") {
		t.Errorf("nudge does not tell the model its tool calls were dropped: %s", content)
	}
}
