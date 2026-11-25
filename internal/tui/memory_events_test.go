package tui

import (
	"strings"
	"testing"

	"github.com/tunsuy/glaude/internal/tools"
)

func TestMemoryEventNotice(t *testing.T) {
	cases := []struct {
		name     string
		ev       tools.MemoryEvent
		wantOK   bool
		wantErr  bool
		contains string
	}{
		{"foreground success is silent", tools.MemoryEvent{Op: "write", Name: "a", QuerySource: "foreground"}, false, false, ""},
		{"unknown source success is silent", tools.MemoryEvent{Op: "write", Name: "a"}, false, false, ""},
		{"background write", tools.MemoryEvent{Op: "write", Name: "a", QuerySource: "extract_memories"}, true, false, `Background memory (extract_memories): saved "a"`},
		{"background delete", tools.MemoryEvent{Op: "delete", Name: "b", QuerySource: "auto_dream"}, true, false, `deleted "b"`},
		{"foreground error", tools.MemoryEvent{Op: "write", Name: "a", QuerySource: "foreground", Err: "disk full"}, true, true, "disk full"},
		{"background warning", tools.MemoryEvent{Op: "write", Name: "a", QuerySource: "extract_memories", Warning: "index rebuild failed: x"}, true, false, "index rebuild failed"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			text, isErr, ok := memoryEventNotice(c.ev)
			if ok != c.wantOK || isErr != c.wantErr {
				t.Fatalf("ok/isErr = %v/%v, want %v/%v (%q)", ok, isErr, c.wantOK, c.wantErr, text)
			}
			if c.contains != "" && !strings.Contains(text, c.contains) {
				t.Errorf("notice %q missing %q", text, c.contains)
			}
			if c.wantErr && !strings.HasPrefix(text, "Error:") {
				t.Errorf("error notice %q should start with Error:", text)
			}
		})
	}
}

func TestMemoryEventSink_DropsWhenFull(t *testing.T) {
	ch := make(chan tools.MemoryEvent, 1)
	sink := memoryEventSink{ch: ch}
	sink.MemoryEvent(tools.MemoryEvent{Name: "1"})
	sink.MemoryEvent(tools.MemoryEvent{Name: "2"}) // must not block
	if got := (<-ch).Name; got != "1" {
		t.Errorf("first event = %q", got)
	}
}

func TestHandleMemoryEvent_AppendsNoticeAndRearms(t *testing.T) {
	m := newTestModel()
	before := len(m.messages)
	m, cmd := m.handleMemoryEvent(tools.MemoryEvent{Op: "write", Name: "a", QuerySource: "extract_memories"})
	if len(m.messages) != before+1 {
		t.Fatalf("want one notice appended, got %d new", len(m.messages)-before)
	}
	if cmd == nil {
		t.Fatal("listener must be re-armed")
	}
}
