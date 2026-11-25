package tui

import (
	"fmt"

	tea "charm.land/bubbletea/v2"

	"github.com/tunsuy/glaude/internal/tools"
)

// memoryEventBuffer bounds pending memory events; when the TUI falls behind,
// further events are dropped rather than blocking a tool goroutine.
const memoryEventBuffer = 32

// memoryEventSink forwards memory tool events to the Update loop without
// blocking (tools.MemoryEventSink).
type memoryEventSink struct{ ch chan<- tools.MemoryEvent }

func (s memoryEventSink) MemoryEvent(ev tools.MemoryEvent) {
	select {
	case s.ch <- ev:
	default:
	}
}

// MemoryEventMsg carries one memory write/delete into the Update loop.
type MemoryEventMsg struct{ Event tools.MemoryEvent }

// listenForMemoryEvent waits for the next memory event.
func listenForMemoryEvent(ch <-chan tools.MemoryEvent) tea.Cmd {
	return func() tea.Msg {
		ev, ok := <-ch
		if !ok {
			return nil
		}
		return MemoryEventMsg{Event: ev}
	}
}

// memoryEventNotice is the transcript notice for ev, or ok=false when ev
// needs none: a successful foreground write already shows as its tool call.
// Background forks (memory extraction, dream) are otherwise invisible, so
// their writes and every failure get a line.
func memoryEventNotice(ev tools.MemoryEvent) (msg string, isErr, ok bool) {
	verb := "saved"
	if ev.Op == "delete" {
		verb = "deleted"
	}
	src := ev.QuerySource
	background := src != "" && src != "foreground"
	where := ""
	if background {
		where = " (" + src + ")"
	}
	switch {
	case ev.Err != "":
		return fmt.Sprintf("Error: memory %q could not be %s%s: %s", ev.Name, verb, where, ev.Err), true, true
	case ev.Warning != "":
		return fmt.Sprintf("Memory %q %s%s, but %s", ev.Name, verb, where, ev.Warning), false, true
	case background:
		return fmt.Sprintf("✻ Background memory%s: %s %q", where, verb, ev.Name), false, true
	}
	return "", false, false
}

// handleMemoryEvent appends ev's notice (if any) and re-arms the listener.
func (m AppModel) handleMemoryEvent(ev tools.MemoryEvent) (AppModel, tea.Cmd) {
	if text, isErr, ok := memoryEventNotice(ev); ok {
		notice := newSystemMessage(text)
		if isErr {
			notice = newSystemErrorMessage(text)
		}
		m.messages = append(m.messages, notice)
		m.syncViewportContent()
	}
	return m, listenForMemoryEvent(m.memEventCh)
}
