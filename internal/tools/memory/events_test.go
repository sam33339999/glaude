package memory_test

import (
	"os"
	"sync"
	"testing"

	"github.com/tunsuy/glaude/internal/memdir"
	"github.com/tunsuy/glaude/internal/tools"
	"github.com/tunsuy/glaude/internal/tools/memory"
)

type eventRecorder struct {
	mu  sync.Mutex
	evs []tools.MemoryEvent
}

func (r *eventRecorder) MemoryEvent(ev tools.MemoryEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.evs = append(r.evs, ev)
}

func TestMemoryTools_EmitEventsWithQuerySource(t *testing.T) {
	store := memdir.NewMemoryStoreWithPath(t.TempDir())
	rec := &eventRecorder{}
	uctx := &tools.UseContext{MemoryStore: store, MemoryEvents: rec, QuerySource: "extract_memories"}

	res, err := memory.MemoryWriteTool.Call(newInput(map[string]any{
		"title": "Prefers tabs", "content": "User prefers tabs.", "type": "user",
	}), uctx, nil)
	if err != nil || res.IsError {
		t.Fatalf("write: %v %v", err, res)
	}
	res, err = memory.MemoryDeleteTool.Call(newInput(map[string]any{"name": "prefers_tabs"}), uctx, nil)
	if err != nil || res.IsError {
		t.Fatalf("delete: %v %v", err, res)
	}

	if len(rec.evs) != 2 {
		t.Fatalf("got %d events, want 2: %+v", len(rec.evs), rec.evs)
	}
	want := []tools.MemoryEvent{
		{Op: "write", Name: "Prefers tabs", QuerySource: "extract_memories"},
		{Op: "delete", Name: "prefers_tabs", QuerySource: "extract_memories"},
	}
	for i, w := range want {
		if rec.evs[i] != w {
			t.Errorf("event %d = %+v, want %+v", i, rec.evs[i], w)
		}
	}
}

func TestMemoryWriteTool_EmitsErrorEvent(t *testing.T) {
	// A memory dir under a regular file cannot be created.
	file := t.TempDir() + "/not-a-dir"
	if err := writeFile(file); err != nil {
		t.Fatal(err)
	}
	rec := &eventRecorder{}
	uctx := &tools.UseContext{MemoryStore: memdir.NewMemoryStoreWithPath(file + "/memory"), MemoryEvents: rec}

	res, _ := memory.MemoryWriteTool.Call(newInput(map[string]any{
		"title": "X", "content": "y", "type": "project",
	}), uctx, nil)
	if !res.IsError {
		t.Fatal("want error result")
	}
	if len(rec.evs) != 1 || rec.evs[0].Err == "" || rec.evs[0].Op != "write" {
		t.Fatalf("events = %+v, want one write error", rec.evs)
	}
}

func writeFile(path string) error { return os.WriteFile(path, []byte("x"), 0o644) }
