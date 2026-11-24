package codemode

import (
	"encoding/json"
	"sync"
	"sync/atomic"

	"github.com/tunsuy/glaude/internal/hooks"
)

// sideEffectCap bounds one recorded write-call input summary (CM-03 v3 audit).
const sideEffectCap = 500

// sideEffect records one executed write call for the execution audit trail
// (CM-03 v3). Only calls that actually ran are recorded — denied or failed
// calls made no changes.
type sideEffect struct {
	Tool  string `json:"tool"`
	Input string `json:"input"` // JSON summary, capped
}

// execState is the shared mutable state of ONE CodeMode execution (CM-03 v3).
// Nested executions (replay/snippets) share the same state: approval scope,
// tool usage and the audit trail stay execution-wide.
//
// All fields except depth are guarded by mu.
type execState struct {
	mu           sync.Mutex
	execAllow    map[string]bool // Ask→user-approved tools (execution scope)
	seen         map[string]bool
	usedTools    []string
	sideEffects  []sideEffect
	extraLogs    []string // logs from nested executions, merged into the result
	batchWrites  bool     // CM-03 write gate mode
	gateCh       chan struct{}
	gateApproved bool
	depth        atomic.Int32 // nested execution depth (replay/run recursion guard)
	hookStop     string       // set when a tool hook returned "continue": false
}

func newExecState(batchWrites bool) *execState {
	return &execState{
		execAllow:   make(map[string]bool),
		seen:        make(map[string]bool),
		batchWrites: batchWrites,
	}
}

// recordTool adds a tool to the deduplicated usage list.
func (s *execState) recordTool(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.seen[name] {
		s.seen[name] = true
		s.usedTools = append(s.usedTools, name)
	}
}

// recordSideEffect appends one executed write call to the audit trail.
func (s *execState) recordSideEffect(name string, input json.RawMessage) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sideEffects = append(s.sideEffects, sideEffect{
		Tool:  name,
		Input: capString(string(input), sideEffectCap),
	})
}

// snapshotTools returns a copy of the usage list.
func (s *execState) snapshotTools() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.usedTools...)
}

// snapshotSideEffects returns a copy of the audit trail.
func (s *execState) snapshotSideEffects() []sideEffect {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]sideEffect(nil), s.sideEffects...)
}

// hookCommon records a hook outcome: notices and additionalContext go to the
// execution log; "continue": false stops later calls of this execution.
func (s *execState) hookCommon(c hooks.Common) {
	s.hookLog(c.Notices)
	s.hookLog(c.AdditionalContext)
	if c.PreventContinuation {
		s.mu.Lock()
		if s.hookStop == "" {
			s.hookStop = c.StopReason
			if s.hookStop == "" {
				s.hookStop = "a hook stopped processing"
			}
		}
		s.mu.Unlock()
	}
}

// hookLog appends hook messages to the execution log.
func (s *execState) hookLog(lines []string) {
	if len(lines) == 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, l := range lines {
		s.extraLogs = append(s.extraLogs, "[hook] "+l)
	}
}

// hookLines returns the hook messages logged so far.
func (s *execState) hookLines() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []string
	for _, l := range s.extraLogs {
		if len(l) > 7 && l[:7] == "[hook] " {
			out = append(out, l)
		}
	}
	return out
}

// hookStopReason returns the stop reason once a hook stopped the execution.
func (s *execState) hookStopReason() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.hookStop
}

// appendNestedLogs merges nested-execution console output (prefixed).
func (s *execState) appendNestedLogs(label string, logs []string) {
	if len(logs) == 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, l := range logs {
		s.extraLogs = append(s.extraLogs, "["+label+"] "+l)
	}
}
