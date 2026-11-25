package memdir

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/tunsuy/glaude/internal/engine"
	"github.com/tunsuy/glaude/internal/tools"
	"github.com/tunsuy/glaude/pkg/types"
)

// scriptedEngine replays fixed messages from Query (other methods unused).
type scriptedEngine struct {
	engine.QueryEngine
	msgs []types.Message
	seen *tools.UseContext
}

func (e *scriptedEngine) Query(_ context.Context, p engine.QueryParams) (<-chan engine.Msg, error) {
	e.seen = p.ToolUseContext
	ch := make(chan engine.Msg, len(e.msgs))
	for i := range e.msgs {
		m := e.msgs[i]
		typ := engine.MsgTypeAssistantMessage
		msg := engine.Msg{Type: typ, AssistantMsg: &m}
		if m.Role == types.RoleUser {
			msg = engine.Msg{Type: engine.MsgTypeUserMessage, UserMsg: &m}
		}
		ch <- msg
	}
	close(ch)
	return ch, nil
}

type recSink struct {
	mu  sync.Mutex
	evs []tools.MemoryEvent
}

func (r *recSink) MemoryEvent(ev tools.MemoryEvent) {
	r.mu.Lock()
	r.evs = append(r.evs, ev)
	r.mu.Unlock()
}

func refusedWrite(id, reason string) []types.Message {
	name, isErr := "MemoryWrite", true
	return []types.Message{
		{Role: types.RoleAssistant, Content: []types.ContentBlock{{Type: types.ContentTypeToolUse, ID: &id, Name: &name}}},
		{Role: types.RoleUser, Content: []types.ContentBlock{{
			Type: types.ContentTypeToolResult, ToolUseID: &id, IsError: &isErr,
			Content: []types.ContentBlock{textBlock(reason)},
		}}},
	}
}

func TestRunMemoryFork_SurfacesRefusedCalls(t *testing.T) {
	sink := &recSink{}
	parentCtx := &tools.UseContext{MemoryEvents: sink}
	eng := &scriptedEngine{msgs: refusedWrite("w1", "write operations are not allowed in plan mode")}

	refused, err := runMemoryFork(context.Background(), eng, engine.ForkedAgentConfig{
		CacheSafeParams: &engine.CacheSafeParams{ToolUseContext: parentCtx},
		QuerySource:     "extract_memories",
	})
	if err != nil {
		t.Fatal(err)
	}
	if refused == nil || !strings.Contains(refused.Error(), "plan mode") {
		t.Fatalf("refused = %v, want the plan-mode reason", refused)
	}
	if len(sink.evs) != 1 || sink.evs[0].Err == "" || sink.evs[0].QuerySource != "extract_memories" {
		t.Fatalf("events = %+v, want one error event", sink.evs)
	}
	if parentCtx.MemoryEvents != sink {
		t.Error("the parent UseContext must not be mutated")
	}
	if eng.seen == parentCtx {
		t.Error("the fork must run on a copy of the parent UseContext")
	}
}

func TestRunMemoryFork_ToolReportedFailureIsNotDoubled(t *testing.T) {
	sink := &recSink{}
	eng := &scriptedEngine{msgs: refusedWrite("w1", "write memory: disk full")}
	// The tool itself reports its failure through the fork's sink.
	eng2 := &reportingEngine{scriptedEngine: eng}

	refused, err := runMemoryFork(context.Background(), eng2, engine.ForkedAgentConfig{
		CacheSafeParams: &engine.CacheSafeParams{ToolUseContext: &tools.UseContext{MemoryEvents: sink}},
		QuerySource:     "extract_memories",
	})
	if err != nil || refused != nil {
		t.Fatalf("err=%v refused=%v, want neither", err, refused)
	}
	if len(sink.evs) != 1 {
		t.Fatalf("events = %+v, want only the tool's own event", sink.evs)
	}
}

// reportingEngine emits one tool error event through the fork's sink, as
// the memory tool would, before replaying.
type reportingEngine struct{ *scriptedEngine }

func (e *reportingEngine) Query(ctx context.Context, p engine.QueryParams) (<-chan engine.Msg, error) {
	p.ToolUseContext.MemoryEvents.MemoryEvent(tools.MemoryEvent{Op: "write", Name: "x", Err: "disk full"})
	return e.scriptedEngine.Query(ctx, p)
}

func TestTouchConsolidateLock_UpdatesMtime(t *testing.T) {
	lock := filepath.Join(t.TempDir(), consolidateLockName)
	if err := touchConsolidateLock(lock); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-48 * time.Hour)
	if err := os.Chtimes(lock, old, old); err != nil {
		t.Fatal(err)
	}
	if !shouldRunAutoDream(filepath.Dir(lock)) {
		t.Fatal("a 48h-old lock should allow a dream")
	}
	if err := touchConsolidateLock(lock); err != nil {
		t.Fatal(err)
	}
	if shouldRunAutoDream(filepath.Dir(lock)) {
		t.Fatal("touching the lock must restart the 24h window")
	}
}
