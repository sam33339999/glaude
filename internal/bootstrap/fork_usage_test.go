package bootstrap

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tunsuy/glaude/internal/engine"
)

func TestLogForkUsage_EmptyPathDisables(t *testing.T) {
	if logForkUsage("", nil) != nil {
		t.Fatal("empty path should yield a nil callback")
	}
}

func TestLogForkUsage_AppendsTaggedRecord(t *testing.T) {
	path := filepath.Join(t.TempDir(), "turn-timings.jsonl")
	start := time.Date(2026, 11, 25, 22, 0, 0, 0, time.UTC)
	logForkUsage(path, nil)(engine.ForkUsage{
		QuerySource: "extract_memories", Model: "glm-5.4", SessionID: "s1", WorkingDir: "/repo",
		Start: start, End: start.Add(1500 * time.Millisecond), Outcome: "done",
		APIRequests: 2, ToolCalls: 1, InputTokens: 70000, OutputTokens: 300, CacheRead: 1000, CacheCreated: 5,
	})

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var row map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(string(data))), &row); err != nil {
		t.Fatal(err)
	}
	want := map[string]any{
		"query_source": "extract_memories", "model": "glm-5.4", "session_id": "s1", "cwd": "/repo",
		"outcome": "done", "duration_ms": float64(1500), "api_requests": float64(2), "tool_calls": float64(1),
		"input_tokens": float64(70000), "output_tokens": float64(300),
		"cache_read_tokens": float64(1000), "cache_creation_tokens": float64(5),
	}
	for k, v := range want {
		if row[k] != v {
			t.Errorf("%s = %v, want %v", k, row[k], v)
		}
	}
}
