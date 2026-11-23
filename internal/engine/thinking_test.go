package engine

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/tunsuy/glaude/internal/api"
	"github.com/tunsuy/glaude/internal/session"
	"github.com/tunsuy/glaude/internal/tools"
	"github.com/tunsuy/glaude/internal/transcript"
	"github.com/tunsuy/glaude/pkg/types"
)

func onlyThinkingMsg() types.Message {
	return types.Message{Role: types.RoleAssistant, Content: []types.ContentBlock{
		{Type: types.ContentTypeThinking, Thinking: strPtr("x"), Signature: strPtr("sx")},
	}}
}

func assistantText(s string) types.Message {
	return types.Message{Role: types.RoleAssistant, Content: []types.ContentBlock{{Type: types.ContentTypeText, Text: strPtr(s)}}}
}

// wireMessages decodes the request's messages back into blocks, plus the raw
// JSON so tests can check what actually goes on the wire.
func wireMessages(t *testing.T, req *api.MessageRequest) ([]types.Message, string) {
	t.Helper()
	var out []types.Message
	var raw strings.Builder
	for _, p := range req.Messages {
		var content []types.ContentBlock
		if err := json.Unmarshal(p.Content, &content); err != nil {
			t.Fatal(err)
		}
		raw.Write(p.Content)
		out = append(out, types.Message{Role: types.Role(p.Role), Content: content})
	}
	return out, raw.String()
}

func lastStream(c *countingClient) *api.MessageRequest {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.streams[len(c.streams)-1]
}

func TestSetMainTarget_KeepsHistory(t *testing.T) {
	eng := New(Config{Client: &countingClient{}, Registry: tools.NewRegistry(), Model: "m"})
	eng.SetMessages([]types.Message{userText("q"), thinkingMsg(), userText("q2"), onlyThinkingMsg()})

	eng.SetMainTarget(MainTarget{Client: &countingClient{}, ProviderID: "zai", Model: "glm"})
	msgs := eng.GetMessages()
	if len(msgs) != 4 || !hasThinking(msgs) {
		t.Fatalf("history changed on provider switch: %d msgs, thinking=%v", len(msgs), hasThinking(msgs))
	}
	for _, i := range []int{1, 3} {
		if got := msgs[i].Content[0].ThinkingOrigin; got != MainProviderID {
			t.Errorf("msg %d: unknown-origin thinking attributed to %q, want %q", i, got, MainProviderID)
		}
	}
}

func TestRequest_LeavesOutForeignThinking(t *testing.T) {
	zai := &countingClient{}
	eng := New(Config{Client: &countingClient{}, Registry: tools.NewRegistry(), Model: "m"})
	eng.SetMessages([]types.Message{userText("q"), thinkingMsg(), userText("q2"), onlyThinkingMsg()})
	eng.SetMainTarget(MainTarget{Client: zai, ProviderID: "zai", Model: "glm"})

	runQuery(t, eng, QueryParams{Messages: append(eng.GetMessages(), userText("q3"))})
	msgs, raw := wireMessages(t, lastStream(zai))
	if hasThinking(msgs) {
		t.Error("thinking from the old provider was sent to the new one")
	}
	if strings.Contains(raw, "thinking_origin") {
		t.Error("thinking_origin leaked into the request")
	}
	// u, a(text), u2+u3 merged: the thinking-only message is omitted and roles alternate.
	if len(msgs) != 3 || msgs[2].Role != types.RoleUser || len(msgs[2].Content) != 2 {
		t.Fatalf("request = %+v", msgs)
	}
	for i := 1; i < len(msgs); i++ {
		if msgs[i].Role == msgs[i-1].Role {
			t.Errorf("roles do not alternate at %d", i)
		}
	}
	// The history keeps everything, plus the new turn.
	if h := eng.GetMessages(); len(h) != 6 || !hasThinking(h[:4]) {
		t.Errorf("history after query = %d msgs", len(h))
	}
}

func TestRequest_SwitchingBackSendsThinkingAgain(t *testing.T) {
	main := &countingClient{}
	eng := New(Config{Client: main, Registry: tools.NewRegistry(), Model: "m"})
	eng.SetMessages([]types.Message{userText("q"), thinkingMsg()})
	eng.SetMainTarget(MainTarget{Client: &countingClient{}, ProviderID: "zai", Model: "glm"})
	eng.SetMainTarget(MainTarget{Client: main, ProviderID: MainProviderID, Model: "m"})

	runQuery(t, eng, QueryParams{Messages: append(eng.GetMessages(), userText("q2"))})
	msgs, raw := wireMessages(t, lastStream(main))
	if !hasThinking(msgs) {
		t.Error("thinking of the original provider was not sent after switching back")
	}
	if strings.Contains(raw, "thinking_origin") {
		t.Error("thinking_origin leaked into the request")
	}
}

func TestThinkingOrigin_EndpointChangeRetiresThinking(t *testing.T) {
	a := MainTarget{ProviderID: "zai", Endpoint: "openai https://a"}
	b := MainTarget{ProviderID: "zai", Endpoint: "openai https://b"}
	if a.ThinkingOrigin() == b.ThinkingOrigin() {
		t.Fatal("an edited endpoint must change the thinking origin")
	}
	if a.ThinkingOrigin() != (MainTarget{ProviderID: "zai", Endpoint: "openai https://a"}).ThinkingOrigin() {
		t.Fatal("origin must be stable for the same endpoint")
	}
	hist := []types.Message{userText("q"), thinkingMsg(), userText("q2")}
	hist = stampUnknownThinking(hist, a.ThinkingOrigin())
	if got := thinkingForRequest(hist, b.ThinkingOrigin()); hasThinking(got) {
		t.Error("thinking signed by the old endpoint kept for the new one")
	}
	if got := thinkingForRequest(hist, a.ThinkingOrigin()); !hasThinking(got) {
		t.Error("own thinking dropped")
	}
	if !hasThinking(hist) {
		t.Error("thinkingForRequest mutated its input")
	}
}

// A query on an explicit tier client has no known origin: nothing is
// filtered (unchanged behaviour), nothing is stamped.
func TestThinkingForRequest_UnknownOriginKeepsAll(t *testing.T) {
	hist := stampUnknownThinking([]types.Message{userText("q"), onlyThinkingMsg(), userText("q2")}, "main")
	if got := thinkingForRequest(hist, ""); len(got) != 3 || !hasThinking(got) {
		t.Errorf("unknown origin filtered history: %+v", got)
	}
}

func TestStreamResponse_StampsThinkingOrigin(t *testing.T) {
	msg := buildAssistantMessage(&api.MessageResponse{Content: []api.ContentBlock{
		{Type: "thinking", Thinking: "t", Signature: "s"}, {Type: "text", Text: "a"},
	}})
	stampThinking(&msg, "zai@abcd")
	if msg.Content[0].ThinkingOrigin != "zai@abcd" || msg.Content[1].ThinkingOrigin != "" {
		t.Errorf("stamped = %+v", msg.Content)
	}
}

func TestCacheSafeParams_LeavesOutForeignThinking(t *testing.T) {
	zai := &countingClient{}
	eng := New(Config{Client: &countingClient{}, Registry: tools.NewRegistry(), Model: "m"})
	eng.SetMessages([]types.Message{userText("q"), thinkingMsg()})
	eng.SetMainTarget(MainTarget{Client: zai, ProviderID: "zai", Model: "glm"})
	uctx := &tools.UseContext{Ctx: context.Background()}
	runQuery(t, eng, QueryParams{Messages: append(eng.GetMessages(), userText("q2")), ToolUseContext: uctx})
	snap := uctx.CacheParams.(*CacheSafeParams)
	if hasThinking(snap.ContextMessages) || snap.ThinkingOrigin != "zai" {
		t.Errorf("fork snapshot: thinking=%v origin=%q", hasThinking(snap.ContextMessages), snap.ThinkingOrigin)
	}
}

// Regression (docs/project/discussions/2026-11-23-strip-thinking-transcript-gap.md):
// switching provider used to delete thinking-only messages from the history,
// shifting the recorder's index so the next turn's first messages never
// reached the JSONL.
func TestProviderSwitch_TranscriptKeepsEveryMessage(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cwd := t.TempDir()
	rec, err := transcript.New(cwd, "test")
	if err != nil {
		t.Fatal(err)
	}
	defer rec.Close()

	eng := New(Config{Client: &countingClient{}, Registry: tools.NewRegistry(), Model: "m"})
	eng.SetMessages([]types.Message{userText("u1"), onlyThinkingMsg(), userText("u2"), assistantText("a2")})
	if err := rec.Record(eng.GetMessages()); err != nil {
		t.Fatal(err)
	}

	eng.SetMainTarget(MainTarget{Client: &countingClient{}, ProviderID: "other", Model: "x"})
	eng.SetMessages(append(eng.GetMessages(), userText("u3"), assistantText("a3")))
	if err := rec.Record(eng.GetMessages()); err != nil {
		t.Fatal(err)
	}

	_, mgr, err := session.Resume(rec.SessionID(), cwd)
	if err != nil {
		t.Fatal(err)
	}
	defer mgr.Close()
	entries, err := mgr.ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, e := range entries {
		var sm struct {
			Message types.Message `json:"message"`
		}
		if err := json.Unmarshal(e.Raw, &sm); err != nil {
			t.Fatal(err)
		}
		switch b := sm.Message.Content[0]; {
		case b.Text != nil:
			got = append(got, *b.Text)
		case b.Type == types.ContentTypeThinking:
			if b.ThinkingOrigin != MainProviderID {
				t.Errorf("thinking persisted with origin %q", b.ThinkingOrigin)
			}
			got = append(got, "[thinking]")
		}
	}
	want := "u1 [thinking] u2 a2 u3 a3"
	if strings.Join(got, " ") != want {
		t.Errorf("jsonl = %v, want %s", got, want)
	}
}
