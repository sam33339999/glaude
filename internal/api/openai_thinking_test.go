package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// ─────────────────────────────────────────────────────────────────────────────
// reasoning_content → thinking display chain (OpenAI-compatible providers)
// ─────────────────────────────────────────────────────────────────────────────

// drainToAccumulator reads all events from a client stream into an Accumulator.
func drainToAccumulator(t *testing.T, client Client, req *MessageRequest) *MessageResponse {
	t.Helper()
	reader, err := client.Stream(context.Background(), req)
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	defer reader.Close()
	acc := &Accumulator{}
	for {
		ev, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("Next: %v", err)
		}
		if ev == nil {
			continue
		}
		if err := acc.Process(ev); err != nil {
			t.Fatalf("Process: %v", err)
		}
	}
	return acc.Result()
}

func thinkingTestClient(t *testing.T, sseData string) Client {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(sseData))
	}))
	t.Cleanup(server.Close)
	client, err := NewClient(ClientConfig{Provider: ProviderOpenAI, APIKey: "k", BaseURL: server.URL}, nil)
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func TestOpenAIStream_ThinkingThenText(t *testing.T) {
	sse := `data: {"id":"t1","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{"role":"assistant","reasoning_content":"Let me think"},"finish_reason":null}]}

data: {"id":"t1","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{"reasoning_content":" about this."},"finish_reason":null}]}

data: {"id":"t1","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{"content":"The answer."},"finish_reason":null}]}

data: {"id":"t1","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}

data: [DONE]

`
	result := drainToAccumulator(t, thinkingTestClient(t, sse), &MessageRequest{Model: "m", MaxTokens: 10})
	if len(result.Content) != 2 {
		t.Fatalf("content blocks = %d, want 2 (thinking + text)", len(result.Content))
	}
	if result.Content[0].Type != "thinking" || result.Content[0].Thinking != "Let me think about this." {
		t.Errorf("thinking block = %+v, want accumulated reasoning", result.Content[0])
	}
	if result.Content[1].Type != "text" || result.Content[1].Text != "The answer." {
		t.Errorf("text block = %+v", result.Content[1])
	}
	if result.StopReason != "end_turn" {
		t.Errorf("stop reason = %q, want end_turn", result.StopReason)
	}
}

func TestOpenAIStream_ThinkingTextToolIndices(t *testing.T) {
	sse := `data: {"id":"t2","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{"reasoning_content":"hmm"},"finish_reason":null}]}

data: {"id":"t2","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{"content":"calling"},"finish_reason":null}]}

data: {"id":"t2","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"Read","arguments":"{}"}}]},"finish_reason":null}]}

data: {"id":"t2","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}

data: [DONE]

`
	reader, err := thinkingTestClient(t, sse).Stream(context.Background(), &MessageRequest{Model: "m", MaxTokens: 10})
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()

	// Track block start indices per type.
	starts := map[string]int{}
	for {
		ev, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if ev == nil || ev.Type != EventContentBlockStart {
			continue
		}
		var bs struct {
			Index        int          `json:"index"`
			ContentBlock ContentBlock `json:"content_block"`
		}
		if err := json.Unmarshal(ev.Data, &bs); err != nil {
			t.Fatal(err)
		}
		starts[bs.ContentBlock.Type] = bs.Index
	}
	if starts["thinking"] != 0 || starts["text"] != 1 || starts["tool_use"] != 2 {
		t.Errorf("block indices = %v, want thinking 0 / text 1 / tool_use 2", starts)
	}
}

func TestOpenAIStream_ReasoningAndContentSameChunk(t *testing.T) {
	// A single chunk carrying both fields must not lose either.
	sse := `data: {"id":"t3","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{"reasoning_content":"think","content":"say"},"finish_reason":null}]}

data: {"id":"t3","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}

data: [DONE]

`
	result := drainToAccumulator(t, thinkingTestClient(t, sse), &MessageRequest{Model: "m", MaxTokens: 10})
	if len(result.Content) != 2 {
		t.Fatalf("content blocks = %d, want 2", len(result.Content))
	}
	if result.Content[0].Thinking != "think" || result.Content[1].Text != "say" {
		t.Errorf("blocks = %+v, want thinking 'think' + text 'say'", result.Content)
	}
}

func TestOpenAIStream_NoReasoningKeepsLegacyIndices(t *testing.T) {
	// Without reasoning_content, text stays index 0 and tools index 1
	// (backward compatibility with the pre-thinking scheme).
	sse := `data: {"id":"t4","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{"content":"hi"},"finish_reason":null}]}

data: {"id":"t4","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"c","type":"function","function":{"name":"Bash","arguments":"{}"}}]},"finish_reason":null}]}

data: {"id":"t4","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}

data: [DONE]

`
	reader, err := thinkingTestClient(t, sse).Stream(context.Background(), &MessageRequest{Model: "m", MaxTokens: 10})
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	starts := map[string]int{}
	for {
		ev, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if ev == nil || ev.Type != EventContentBlockStart {
			continue
		}
		var bs struct {
			Index        int          `json:"index"`
			ContentBlock ContentBlock `json:"content_block"`
		}
		if err := json.Unmarshal(ev.Data, &bs); err != nil {
			t.Fatal(err)
		}
		starts[bs.ContentBlock.Type] = bs.Index
	}
	if starts["text"] != 0 || starts["tool_use"] != 1 {
		t.Errorf("legacy indices = %v, want text 0 / tool_use 1", starts)
	}
}

func TestOpenAIComplete_ReasoningContentBecomesThinkingBlock(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"c1","object":"chat.completion","created":1,"model":"m","choices":[{"index":0,"message":{"role":"assistant","reasoning_content":"deep thoughts","content":"answer"},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}}`))
	}))
	defer server.Close()
	client, err := NewClient(ClientConfig{Provider: ProviderOpenAI, APIKey: "k", BaseURL: server.URL}, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := client.Complete(context.Background(), &MessageRequest{Model: "m", MaxTokens: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Content) != 2 {
		t.Fatalf("content blocks = %d, want 2", len(resp.Content))
	}
	if resp.Content[0].Type != "thinking" || resp.Content[0].Thinking != "deep thoughts" {
		t.Errorf("first block = %+v, want thinking 'deep thoughts'", resp.Content[0])
	}
	if resp.Content[1].Text != "answer" {
		t.Errorf("second block = %+v, want text 'answer'", resp.Content[1])
	}
}

// TestConvertMessage_ThinkingReplayedAsReasoningContent guards the fix for
// reasoning leaking into replies: replaying past reasoning as assistant text
// ("[Thinking]: ...") taught the model to write that marker into its answers.
func TestConvertMessage_ThinkingReplayedAsReasoningContent(t *testing.T) {
	client := &openaiClient{}
	cases := []struct {
		name, content, wantText, wantReasoning string
		wantTools                              int
	}{
		{"text and tool", `[{"type":"thinking","thinking":"plan A"},{"type":"thinking","thinking":"plan B"},{"type":"text","text":"Doing it."},{"type":"tool_use","id":"c1","name":"Read","input":{}}]`, "Doing it.", "plan A\nplan B", 1},
		{"thinking only", `[{"type":"thinking","thinking":"hmm"}]`, "", "hmm", 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := client.convertMessage(MessageParam{Role: "assistant", Content: json.RawMessage(tc.content)})
			if len(got) != 1 {
				t.Fatalf("got %d messages, want 1 (reasoning must not be a separate message)", len(got))
			}
			m := got[0]
			if m.Content == nil || *m.Content != tc.wantText {
				t.Errorf("content = %v, want %q", m.Content, tc.wantText)
			}
			if m.ReasoningContent != tc.wantReasoning {
				t.Errorf("reasoning_content = %q, want %q", m.ReasoningContent, tc.wantReasoning)
			}
			if len(m.ToolCalls) != tc.wantTools {
				t.Errorf("tool calls = %d, want %d", len(m.ToolCalls), tc.wantTools)
			}
			raw, _ := json.Marshal(m)
			if strings.Contains(string(raw), "[Thinking]") {
				t.Errorf("serialized message leaks a [Thinking] marker: %s", raw)
			}
		})
	}
}
