// Package transcript persists conversation messages to session JSONL files.
//
// Product-side wiring for the agtkeel session store (BF-18): the kernel
// provides storage (session.SessionStorer); this package decides WHEN
// messages are written (turn boundaries, driven by the TUI/headless callers)
// and keeps a write baseline so history is never duplicated. The entry shape
// (type envelope + types.SerializedMessage) is exactly what the existing
// readers decode (bootstrap/session.go, commands/resume.go).
package transcript

import (
	"fmt"
	"sync"
	"time"

	"github.com/tunsuy/glaude/internal/session"
	"github.com/tunsuy/glaude/pkg/types"
)

// Recorder appends new conversation messages to a single session JSONL file.
// It is safe for concurrent use. A nil *Recorder is a valid no-op, used by
// tests and by sessions where persistence could not be initialised.
type Recorder struct {
	mu        sync.Mutex
	store     session.SessionStorer
	sid       types.SessionId
	cwd       string
	version   string
	persisted int // number of history messages already written to the file
}

// entry is the JSONL line shape: envelope type plus the message snapshot.
// Readers filter on Type and decode SerializedMessage, so both are required.
type entry struct {
	Type types.EntryType `json:"type"`
	types.SerializedMessage
}

// New creates a fresh session under cwd and returns its recorder.
func New(cwd, version string) (*Recorder, error) {
	sid, mgr, err := session.New(cwd)
	if err != nil {
		return nil, fmt.Errorf("transcript: new session: %w", err)
	}
	return &Recorder{store: mgr, sid: sid, cwd: cwd, version: version}, nil
}

// Resume reopens an existing session under cwd. existing is the number of
// loaded history messages already present in the file; they are never
// re-appended (the baseline starts past them).
func Resume(cwd, sessionID, version string, existing int) (*Recorder, error) {
	sid, mgr, err := session.Resume(sessionID, cwd)
	if err != nil {
		return nil, fmt.Errorf("transcript: resume session: %w", err)
	}
	return &Recorder{store: mgr, sid: sid, cwd: cwd, version: version, persisted: existing}, nil
}

// SessionID returns the current session id ("" for a nil recorder).
func (r *Recorder) SessionID() string {
	if r == nil {
		return ""
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return string(r.sid)
}

// Path returns the session JSONL file path ("" for a nil recorder or when
// it cannot be resolved).
func (r *Recorder) Path() string {
	if r == nil {
		return ""
	}
	r.mu.Lock()
	cwd, sid := r.cwd, r.sid
	r.mu.Unlock()
	p, err := session.TranscriptPath(cwd, sid)
	if err != nil {
		return ""
	}
	return p
}

// Record appends the not-yet-persisted tail of msgs (user/assistant roles
// only; other roles are skipped but still advance the baseline). On append
// failure the baseline stays at the failed message so a later call retries
// it. If msgs shrank below the baseline (e.g. /clear reset the engine
// history), the baseline resets without rewriting the file — the transcript
// is append-only (documented v1 limitation, design/tui.md §4.1).
func (r *Recorder) Record(msgs []types.Message) error {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.store == nil {
		return nil // closed; late turn-boundary calls are no-ops
	}
	if len(msgs) < r.persisted {
		r.persisted = len(msgs)
	}
	for i := r.persisted; i < len(msgs); i++ {
		m := msgs[i]
		var t types.EntryType
		switch m.Role {
		case types.RoleUser:
			t = types.EntryTypeUser
		case types.RoleAssistant:
			t = types.EntryTypeAssistant
		default:
			r.persisted = i + 1
			continue
		}
		e := entry{
			Type: t,
			SerializedMessage: types.SerializedMessage{
				Message:   m,
				CWD:       r.cwd,
				UserType:  "external",
				SessionId: r.sid,
				Timestamp: time.Now().UTC(),
				Version:   r.version,
			},
		}
		if err := r.store.AppendEntry(e); err != nil {
			return fmt.Errorf("transcript: append entry: %w", err)
		}
		r.persisted = i + 1
	}
	return nil
}

// SwitchSession adopts a different existing session (mid-session /resume):
// it closes the current store, opens the target and sets the baseline past
// the loaded history. The recorder's cwd and version are unchanged.
func (r *Recorder) SwitchSession(sessionID string, existing int) error {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	sid, mgr, err := session.Resume(sessionID, r.cwd)
	if err != nil {
		return fmt.Errorf("transcript: switch session: %w", err)
	}
	if r.store != nil {
		if cerr := r.store.Close(); cerr != nil {
			_ = mgr.Close()
			return fmt.Errorf("transcript: close previous session: %w", cerr)
		}
	}
	r.store = mgr
	r.sid = sid
	r.persisted = existing
	return nil
}

// Close closes the underlying store. Subsequent Record calls are no-ops.
func (r *Recorder) Close() error {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.store == nil {
		return nil
	}
	err := r.store.Close()
	r.store = nil
	if err != nil {
		return fmt.Errorf("transcript: close: %w", err)
	}
	return nil
}
