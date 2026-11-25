package memdir

import (
	"context"
	"fmt"
	"sync"

	"github.com/tunsuy/glaude/internal/engine"
	"github.com/tunsuy/glaude/internal/tools"
	"github.com/tunsuy/glaude/pkg/types"
)

// memoryToolNames are the only tools memory forks may run.
var memoryToolNames = []string{"MemoryRead", "MemoryWrite", "MemoryDelete"}

// errorCountingSink forwards memory events and counts the failed ones.
type errorCountingSink struct {
	next tools.MemoryEventSink
	mu   sync.Mutex
	errs int
}

func (s *errorCountingSink) MemoryEvent(ev tools.MemoryEvent) {
	if ev.Err != "" {
		s.mu.Lock()
		s.errs++
		s.mu.Unlock()
	}
	if s.next != nil {
		s.next.MemoryEvent(ev)
	}
}

// runMemoryFork runs a memory fork (extraction or dream) and makes refused
// memory-tool calls visible. A call stopped before the tool ran (permission
// mode, deny rule, hook) emits no MemoryEvent of its own, so after the run
// failed MemoryWrite/MemoryDelete results beyond the failures the tools
// reported are surfaced as one error event. The returned refused error is
// non-nil in that case (err is the fork's own error).
func runMemoryFork(ctx context.Context, eng engine.QueryEngine, cfg engine.ForkedAgentConfig) (refused, err error) {
	// Fork from a copy: the parent's CacheSafeParams and UseContext are
	// shared with the main loop and must not be mutated.
	var sink *errorCountingSink
	if p := cfg.CacheSafeParams; p != nil && p.ToolUseContext != nil {
		pc := *p
		uc := *p.ToolUseContext
		sink = &errorCountingSink{next: uc.MemoryEvents}
		uc.MemoryEvents = sink
		pc.ToolUseContext = &uc
		cfg.CacheSafeParams = &pc
	}

	msgs, err := engine.RunForkedAgent(ctx, eng, cfg)

	failed, firstReason := failedMemoryCalls(msgs)
	reported := 0
	if sink != nil {
		sink.mu.Lock()
		reported = sink.errs
		sink.mu.Unlock()
	}
	if n := failed - reported; n > 0 {
		refused = fmt.Errorf("%d memory tool call(s) refused: %s", n, firstReason)
		if sink != nil && sink.next != nil {
			sink.next.MemoryEvent(tools.MemoryEvent{Op: "write", Name: "(" + cfg.QuerySource + ")", QuerySource: cfg.QuerySource, Err: refused.Error()})
		}
	}
	return refused, err
}

// failedMemoryCalls counts error tool_results answering MemoryWrite or
// MemoryDelete calls in msgs, with the first one's text.
func failedMemoryCalls(msgs []types.Message) (n int, first string) {
	writes := map[string]bool{}
	for _, m := range msgs {
		for _, b := range m.Content {
			if b.Type == types.ContentTypeToolUse && b.ID != nil && b.Name != nil &&
				(*b.Name == "MemoryWrite" || *b.Name == "MemoryDelete") {
				writes[*b.ID] = true
			}
		}
	}
	for _, m := range msgs {
		for _, b := range m.Content {
			if b.Type != types.ContentTypeToolResult || b.ToolUseID == nil || !writes[*b.ToolUseID] ||
				b.IsError == nil || !*b.IsError {
				continue
			}
			n++
			if first == "" {
				for _, c := range b.Content {
					if c.Text != nil {
						first = *c.Text
						break
					}
				}
			}
		}
	}
	return n, first
}
