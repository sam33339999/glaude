// Package turnlog appends per-turn usage records to the turn-timing JSONL log
// (~/.glaude/turn-timings.jsonl). Both the interactive turn (TUI) and
// background forked agents (memory extraction, auto dream) write here, told
// apart by QuerySource, so their token costs can be compared from one file.
//
// Records never carry prompt text.
package turnlog

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// QuerySourceForeground tags the interactive turn. Lines written before the
// query_source field existed have no value and should be read as foreground.
const QuerySourceForeground = "foreground"

// Record is one JSONL line: the wall-clock span of a turn (or a forked
// agent run) plus what it did.
type Record struct {
	Start       time.Time `json:"start"`
	End         time.Time `json:"end"`
	DurationMs  int64     `json:"duration_ms"`
	Outcome     string    `json:"outcome"` // done | error | interrupted
	QuerySource string    `json:"query_source,omitempty"`
	SessionID   string    `json:"session_id"`
	Cwd         string    `json:"cwd"`
	Model       string    `json:"model"`
	APIRequests int       `json:"api_requests"`
	ToolCalls   int       `json:"tool_calls"`
	// InputTokens excludes cache hits (Anthropic semantics); the full prompt
	// size is InputTokens + CacheReadTokens + CacheCreationTokens.
	InputTokens         int `json:"input_tokens"`
	OutputTokens        int `json:"output_tokens"`
	CacheReadTokens     int `json:"cache_read_tokens"`
	CacheCreationTokens int `json:"cache_creation_tokens"`
}

// mu serialises appends within the process: the TUI and background forks
// write concurrently, and one Write per line keeps lines whole.
var mu sync.Mutex

// Append appends rec as one JSON line to path, creating the parent directory
// and file when missing.
func Append(path string, rec Record) error {
	line, err := json.Marshal(rec)
	if err != nil {
		return fmt.Errorf("turn log: marshal: %w", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("turn log: mkdir: %w", err)
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("turn log: open: %w", err)
	}
	if _, err := f.Write(append(line, '\n')); err != nil {
		_ = f.Close()
		return fmt.Errorf("turn log: write: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("turn log: close: %w", err)
	}
	return nil
}

// DefaultPath returns ~/.glaude/turn-timings.jsonl, or "" when the home
// directory cannot be resolved (logging is then disabled).
func DefaultPath() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, ".glaude", "turn-timings.jsonl")
}
