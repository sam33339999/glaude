package acpclient

import (
	"encoding/json"
	"testing"

	acp "github.com/coder/acp-go-sdk"
)

// decodeToolCall parses a toolCall object as sent in session/request_permission.
func decodeToolCall(t *testing.T, s string) acp.ToolCallUpdate {
	t.Helper()
	var tc acp.ToolCallUpdate
	if err := json.Unmarshal([]byte(s), &tc); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return tc
}

func inputOf(t *testing.T, q PermissionQuery) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(q.Input, &m); err != nil {
		t.Fatalf("input: %v", err)
	}
	return m
}

// Payloads below are trimmed copies of real requests captured from
// grok 1.0.92 (`grok agent stdio`) and opencode 1.19.5 (`opencode acp`).
func TestMapToolCall_RealPayloads(t *testing.T) {
	cases := []struct {
		name, agent, payload string
		tool                 string
		want                 map[string]any
	}{
		{
			name: "grok execute", agent: "grok", tool: "Bash",
			payload: `{"toolCallId":"c0","kind":"execute","title":"Execute ` + "`echo hi > a.txt`" + `","rawInput":{"variant":"Bash","command":"echo hi > a.txt","description":"x","is_background":false}}`,
			want:    map[string]any{"command": "echo hi > a.txt"},
		},
		{
			name: "grok search_replace", agent: "grok", tool: "Edit",
			payload: `{"toolCallId":"c1","kind":"edit","title":"Edit /p/a.txt","rawInput":{"variant":"SearchReplace","file_path":"/p/a.txt","old_string":"hi","new_string":"bye","replace_all":false}}`,
			want:    map[string]any{"file_path": "/p/a.txt", "old_string": "hi", "new_string": "bye"},
		},
		{
			name: "opencode bash", agent: "opencode", tool: "Bash",
			payload: `{"toolCallId":"o0","title":"echo hi > a.txt","kind":"execute","status":"pending","locations":[],"rawInput":{"command":"echo hi > a.txt"}}`,
			want:    map[string]any{"command": "echo hi > a.txt"},
		},
		{
			name: "opencode edit with diff", agent: "opencode", tool: "Edit",
			payload: `{"toolCallId":"o1","title":"/p/a.txt","kind":"edit","status":"pending","locations":[{"path":"/p/a.txt"}],"rawInput":{"filepath":"/p/a.txt","diff":"..."},"content":[{"type":"diff","path":"/p/a.txt","oldText":"hi\n","newText":"bye\n"}]}`,
			want:    map[string]any{"file_path": "/p/a.txt", "old_string": "hi\n", "new_string": "bye\n"},
		},
		{
			name: "new file diff becomes Write", agent: "opencode", tool: "Write",
			payload: `{"toolCallId":"o2","kind":"edit","content":[{"type":"diff","path":"/p/new.go","newText":"package p"}]}`,
			want:    map[string]any{"file_path": "/p/new.go", "content": "package p"},
		},
		{
			name: "read via locations", agent: "opencode", tool: "Read",
			payload: `{"toolCallId":"o3","kind":"read","locations":[{"path":"/p/x.go"}]}`,
			want:    map[string]any{"file_path": "/p/x.go"},
		},
		{
			name: "fetch", agent: "grok", tool: "WebFetch",
			payload: `{"toolCallId":"g4","kind":"fetch","title":"Fetch docs","rawInput":{"url":"https://example.com"}}`,
			want:    map[string]any{"url": "https://example.com", "prompt": "Fetch docs"},
		},
		{
			name: "search", agent: "grok", tool: "Grep",
			payload: `{"toolCallId":"g5","kind":"search","rawInput":{"pattern":"TODO","path":"/p"}}`,
			want:    map[string]any{"pattern": "TODO", "path": "/p"},
		},
		{
			name: "execute without command is generic", agent: "grok", tool: GenericToolName,
			payload: `{"toolCallId":"g6","kind":"execute","title":"run something"}`,
			want:    map[string]any{"agent": "grok", "kind": "execute", "title": "run something"},
		},
		{
			name: "other kind is generic", agent: "grok", tool: GenericToolName,
			payload: `{"toolCallId":"g7","kind":"other","title":"image_gen"}`,
			want:    map[string]any{"agent": "grok", "kind": "other", "title": "image_gen"},
		},
		{
			name: "agy run_command", agent: "agy", tool: "Bash",
			payload: `{"toolCallId":"a0","kind":"execute","title":"echo hello-agy-acp","rawInput":{"CommandLine":"echo hello-agy-acp","WaitMsBeforeAsync":5000,"toolSummary":"Echo test"}}`,
			want:    map[string]any{"command": "echo hello-agy-acp"},
		},
		{
			name: "agy edit_file", agent: "agy", tool: "Edit",
			payload: `{"toolCallId":"a1","kind":"edit","title":"Edit /p/a.go","rawInput":{"Path":"/p/a.go","Target":"old","Replacement":"new"}}`,
			want:    map[string]any{"file_path": "/p/a.go", "old_string": "old", "new_string": "new"},
		},
		{
			name: "agy write_file", agent: "agy", tool: "Write",
			payload: `{"toolCallId":"a2","kind":"edit","title":"Write /p/b.go","rawInput":{"Path":"/p/b.go","Content":"package b"}}`,
			want:    map[string]any{"file_path": "/p/b.go", "content": "package b"},
		},
		{
			name: "agy read_file", agent: "agy", tool: "Read",
			payload: `{"toolCallId":"a3","kind":"read","title":"Read /p/a.go","rawInput":{"Path":"/p/a.go"}}`,
			want:    map[string]any{"file_path": "/p/a.go"},
		},
		{
			name: "agy grep_search", agent: "agy", tool: "Grep",
			payload: `{"toolCallId":"a4","kind":"search","title":"Grep TODO","rawInput":{"Query":"TODO","SearchPath":"/p"}}`,
			want:    map[string]any{"pattern": "TODO", "path": "/p"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			q := MapToolCall(c.agent, decodeToolCall(t, c.payload))
			if q.ToolName != c.tool {
				t.Fatalf("tool = %q, want %q", q.ToolName, c.tool)
			}
			got := inputOf(t, q)
			if len(got) != len(c.want) {
				t.Fatalf("input = %v, want %v", got, c.want)
			}
			for k, v := range c.want {
				if got[k] != v {
					t.Fatalf("input[%s] = %v, want %v (full %v)", k, got[k], v, got)
				}
			}
			if q.Agent != c.agent || q.ToolCallID == "" {
				t.Errorf("query meta = %+v", q)
			}
		})
	}
}
