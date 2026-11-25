package tui

import (
	"bufio"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/tunsuy/glaude/internal/engine"
)

// readTurnLog returns each JSONL line of path decoded as a map.
func readTurnLog(t *testing.T, path string) []map[string]any {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open turn log: %v", err)
	}
	defer f.Close()
	var out []map[string]any
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var row map[string]any
		if err := json.Unmarshal(sc.Bytes(), &row); err != nil {
			t.Fatalf("decode line %q: %v", sc.Text(), err)
		}
		out = append(out, row)
	}
	return out
}

func TestAppendTurnRecord_CreatesDirAndAppends(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "turn-timings.jsonl")
	start := time.Date(2026, 11, 20, 10, 0, 0, 0, time.UTC)
	first := turnRecord{
		Start: start, End: start.Add(83123 * time.Millisecond), DurationMs: 83123,
		Outcome: "done", SessionID: "s1", Cwd: "/tmp", Model: "m",
		APIRequests: 2, ToolCalls: 1, InputTokens: 150, OutputTokens: 30,
	}
	if err := appendTurnRecord(path, first); err != nil {
		t.Fatalf("append first: %v", err)
	}
	if err := appendTurnRecord(path, turnRecord{Outcome: "error"}); err != nil {
		t.Fatalf("append second: %v", err)
	}
	rows := readTurnLog(t, path)
	if len(rows) != 2 {
		t.Fatalf("want 2 lines, got %d", len(rows))
	}
	if rows[0]["outcome"] != "done" || rows[0]["duration_ms"] != float64(83123) {
		t.Errorf("first row = %v", rows[0])
	}
	for _, k := range []string{"start", "end", "session_id", "cwd", "model", "api_requests", "tool_calls", "input_tokens", "output_tokens"} {
		if _, ok := rows[0][k]; !ok {
			t.Errorf("first row missing key %q", k)
		}
	}
	if rows[1]["outcome"] != "error" {
		t.Errorf("second row outcome = %v", rows[1]["outcome"])
	}
}

func TestWriteTurnRecordCmd_EmptyPathIsNil(t *testing.T) {
	if writeTurnRecordCmd("", turnRecord{}) != nil {
		t.Fatal("empty path should yield a nil Cmd")
	}
}

// fakeClock returns a controllable clock for turn-timing tests.
type fakeClock struct{ t time.Time }

func (c *fakeClock) now() time.Time { return c.t }

// newTimingModel returns a test model logging to a temp file with a fake clock.
func newTimingModel(t *testing.T) (AppModel, *fakeClock, string) {
	t.Helper()
	m := newTestModel()
	clk := &fakeClock{t: time.Date(2026, 11, 20, 10, 0, 0, 0, time.UTC)}
	m.now = clk.now
	path := filepath.Join(t.TempDir(), "t.jsonl")
	m.turnLogPath = path
	return m, clk, path
}

// startTimedTurn begins a query and opens a stream so handlers do not
// treat events as stale.
func startTimedTurn(m AppModel) AppModel {
	m = m.beginQuery()
	m.streamCh = make(chan engine.Msg)
	return m
}

// applyRun applies msg and runs every resulting Cmd (one Batch level deep)
// so the turn-log write happens synchronously.
func applyRun(m AppModel, msg tea.Msg) AppModel {
	updated, cmd := m.Update(msg)
	runCmd(cmd)
	return updated.(AppModel)
}

func runCmd(cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	done := make(chan tea.Msg, 1)
	go func() { done <- cmd() }()
	select {
	case out := <-done:
		if batch, ok := out.(tea.BatchMsg); ok {
			for _, c := range batch {
				runCmd(c)
			}
		}
	case <-time.After(200 * time.Millisecond):
		// Long-lived listeners (stream waits) never return; skip them.
	}
}

// lastTurnMsgText returns the text of the last message in m.
func lastTurnMsgText(m AppModel) string {
	if len(m.messages) == 0 {
		return ""
	}
	var s strings.Builder
	for _, b := range m.messages[len(m.messages)-1].Content {
		if b.Text != nil {
			s.WriteString(*b.Text)
		}
	}
	return s.String()
}

func anyMessageContains(m AppModel, word string) bool {
	for _, msg := range m.messages {
		for _, b := range msg.Content {
			if b.Text != nil && strings.Contains(*b.Text, word) {
				return true
			}
		}
	}
	return false
}

func TestTurnTiming_DoneShowsAndLogs(t *testing.T) {
	m, clk, path := newTimingModel(t)
	m = startTimedTurn(m)
	m = applyRun(m, StreamToolUseStartMsg{ToolName: "Bash"})
	m = applyRun(m, StreamAssistantTurnMsg{Usage: &TokenUsage{Input: 100, Output: 20, CacheRead: 4000, CacheCreated: 300}})
	clk.t = clk.t.Add(83 * time.Second)
	m = applyRun(m, StreamDoneMsg{Usage: &TokenUsage{Input: 50, Output: 10, CacheRead: 4300}})

	text := lastTurnMsgText(m)
	for _, w := range []string{"Worked", "1m23s"} {
		if !strings.Contains(text, w) {
			t.Errorf("last message %q missing %q", text, w)
		}
	}
	rows := readTurnLog(t, path)
	if len(rows) != 1 {
		t.Fatalf("want 1 row, got %d", len(rows))
	}
	r := rows[0]
	want := map[string]any{
		"outcome": "done", "duration_ms": float64(83000), "api_requests": float64(2),
		"tool_calls": float64(1), "input_tokens": float64(150), "output_tokens": float64(30),
		"model": "test-model", "cwd": "/tmp", "query_source": "foreground",
		"cache_read_tokens": float64(8300), "cache_creation_tokens": float64(300),
	}
	for k, v := range want {
		if r[k] != v {
			t.Errorf("%s = %v, want %v", k, r[k], v)
		}
	}
}

func TestTurnTiming_ErrorLogsWithoutExtraLine(t *testing.T) {
	m, _, path := newTimingModel(t)
	m = startTimedTurn(m)
	m = applyRun(m, StreamErrorMsg{Err: errors.New("boom")})
	if anyMessageContains(m, "Worked") {
		t.Error("error turn should not show a Worked line")
	}
	rows := readTurnLog(t, path)
	if len(rows) != 1 || rows[0]["outcome"] != "error" {
		t.Fatalf("rows = %v", rows)
	}
}

func TestTurnTiming_AbortThenStaleDone(t *testing.T) {
	m, clk, path := newTimingModel(t)
	m = startTimedTurn(m)
	clk.t = clk.t.Add(12 * time.Second)
	updated, cmd := m.doAbort()
	runCmd(cmd)
	m = updated.(AppModel)
	if text := lastTurnMsgText(m); !strings.Contains(text, "Interrupted") || !strings.Contains(text, "12s") {
		t.Errorf("last message %q should report the interruption", text)
	}
	m = applyRun(m, StreamDoneMsg{})
	rows := readTurnLog(t, path)
	if len(rows) != 1 || rows[0]["outcome"] != "interrupted" {
		t.Fatalf("rows = %v", rows)
	}
}

func TestTurnTiming_SecondTurnStartsFresh(t *testing.T) {
	m, _, path := newTimingModel(t)
	for i := 0; i < 2; i++ {
		m = startTimedTurn(m)
		m = applyRun(m, StreamToolUseStartMsg{ToolName: "Read"})
		m = applyRun(m, StreamDoneMsg{})
	}
	rows := readTurnLog(t, path)
	if len(rows) != 2 || rows[1]["tool_calls"] != float64(1) {
		t.Fatalf("rows = %v", rows)
	}
}

func TestTurnTiming_NoStartNoRecord(t *testing.T) {
	m, _, path := newTimingModel(t)
	m.streamCh = make(chan engine.Msg)
	m = applyRun(m, StreamDoneMsg{})
	if anyMessageContains(m, "Worked") {
		t.Error("no turn start: should not show a Worked line")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("turn log should not exist, stat err = %v", err)
	}
}

func TestTurnTiming_EmptyPathStillShows(t *testing.T) {
	m, _, _ := newTimingModel(t)
	m.turnLogPath = ""
	m = startTimedTurn(m)
	m = applyRun(m, StreamDoneMsg{})
	if !strings.Contains(lastTurnMsgText(m), "Worked") {
		t.Error("display must not depend on logging")
	}
}
