package tui

import (
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/tunsuy/glaude/internal/turnlog"
)

// turnStats accumulates per-turn counters between query start and finish.
type turnStats struct {
	apiRequests  int
	toolCalls    int
	inputTokens  int
	outputTokens int
	cacheRead    int
	cacheCreated int
}

// turnRecord is one turn-timing log line (see turnlog.Record).
type turnRecord = turnlog.Record

// appendTurnRecord appends rec as one JSON line to path.
func appendTurnRecord(path string, rec turnRecord) error { return turnlog.Append(path, rec) }

// writeTurnRecordCmd appends rec off the Update loop. An empty path disables
// logging (nil Cmd); write failures are dropped so timing never disturbs
// the conversation.
func writeTurnRecordCmd(path string, rec turnRecord) tea.Cmd {
	if path == "" {
		return nil
	}
	return func() tea.Msg {
		_ = appendTurnRecord(path, rec)
		return nil
	}
}

// defaultTurnLogPath returns ~/.glaude/turn-timings.jsonl, or "" when the
// home directory cannot be resolved (logging is then disabled).
func defaultTurnLogPath() string { return turnlog.DefaultPath() }

// clock returns the current time from the injected clock (time.Now by default).
func (m AppModel) clock() time.Time {
	if m.now != nil {
		return m.now()
	}
	return time.Now()
}

// beginQuery switches the model into the loading state and starts timing a
// new turn. Every query start point goes through here so the timer and the
// spinner can never disagree about when a turn began.
func (m AppModel) beginQuery() AppModel {
	m.isLoading = true
	m.showSpinner = true
	m.spinner = m.spinner.Reset()
	m.turnStart = m.clock()
	m.turnStats = turnStats{}
	// Bubbles appended after this call stay out of GetMessages until the loop
	// exits. Queued user bubbles from earlier stay in unsentUsers.
	m.inflightBase = len(m.messages)
	m.inflightUser = -1
	return m
}

// noteTurnUsage folds one API response's usage into the running turn.
func (m AppModel) noteTurnUsage(u *TokenUsage) AppModel {
	if u == nil {
		return m
	}
	m.turnStats.apiRequests++
	m.turnStats.inputTokens += u.Input
	m.turnStats.outputTokens += u.Output
	m.turnStats.cacheRead += u.CacheRead
	m.turnStats.cacheCreated += u.CacheCreated
	return m
}

// finishTurn ends the timed turn with outcome (done | error | interrupted):
// it appends the display-only duration line (done/interrupted) and returns
// the Cmd that logs the record. Without a running turn it is a no-op, which
// also makes stale end events harmless. The line lives only in m.messages —
// never in engine messages or the transcript — so the prompt is unaffected.
func (m AppModel) finishTurn(outcome string) (AppModel, tea.Cmd) {
	if m.turnStart.IsZero() {
		return m, nil
	}
	end := m.clock()
	dur := end.Sub(m.turnStart)
	rec := turnRecord{
		Start:               m.turnStart,
		End:                 end,
		DurationMs:          dur.Milliseconds(),
		Outcome:             outcome,
		QuerySource:         turnlog.QuerySourceForeground,
		SessionID:           m.sessionID,
		Cwd:                 m.statusBar.cwd,
		Model:               m.statusBar.model,
		APIRequests:         m.turnStats.apiRequests,
		ToolCalls:           m.turnStats.toolCalls,
		InputTokens:         m.turnStats.inputTokens,
		OutputTokens:        m.turnStats.outputTokens,
		CacheReadTokens:     m.turnStats.cacheRead,
		CacheCreationTokens: m.turnStats.cacheCreated,
	}
	switch outcome {
	case "done":
		m.messages = append(m.messages, newSystemMessage("✻ Worked for "+formatDuration(dur)))
	case "interrupted":
		m.messages = append(m.messages, newSystemMessage("✻ Interrupted after "+formatDuration(dur)))
	}
	m.turnStart = time.Time{}
	m.turnStats = turnStats{}
	return m, writeTurnRecordCmd(m.turnLogPath, rec)
}
