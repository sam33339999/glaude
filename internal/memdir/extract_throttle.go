package memdir

import (
	"sync"

	"github.com/tunsuy/glaude/pkg/types"
)

// Extraction runs once enough new conversation has piled up since the last
// run, instead of after every turn: each run re-sends the whole history.
const (
	// extractMinNewChars is the new user/assistant text (bytes) since the
	// last extraction that triggers the next one.
	extractMinNewChars = 2000
	// extractMaxTurnsBetween forces a run after this many foreground turns
	// with any new text, however short, so small facts are not held forever.
	extractMaxTurnsBetween = 5
)

// ExtractThrottle decides when memory extraction runs. It keeps a cursor
// (the history length at the last run) per session; only text after the
// cursor counts as new and goes into the extraction summary. It is safe for
// concurrent use (stop hooks run in their own goroutines).
type ExtractThrottle struct {
	// MinNewChars and MaxTurns are the two triggers (see the constants);
	// zero values use the defaults.
	MinNewChars int
	MaxTurns    int

	mu        sync.Mutex
	sessionID string
	cursor    int
	turns     int
	inFlight  bool
}

// NewExtractThrottle returns a throttle with the default triggers.
func NewExtractThrottle() *ExtractThrottle {
	return &ExtractThrottle{MinNewChars: extractMinNewChars, MaxTurns: extractMaxTurnsBetween}
}

// sync resets the cursor when the session changed or the history shrank
// (compaction, /clear): the old index no longer points into this history.
// Callers hold mu.
func (t *ExtractThrottle) sync(sessionID string, n int) {
	if sessionID != t.sessionID || n < t.cursor {
		t.sessionID, t.cursor, t.turns = sessionID, 0, 0
	}
}

// begin records one finished foreground turn and reports whether extraction
// should run now. When it should, from is the cursor to summarise from; the
// cursor moves to the end of messages and the caller must call done.
// A run already in flight defers the next one (its turn still counts).
func (t *ExtractThrottle) begin(sessionID string, messages []types.Message) (from int, run bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.sync(sessionID, len(messages))
	t.turns++
	if t.inFlight {
		return 0, false
	}
	newChars := conversationTextBytes(messages[t.cursor:])
	if newChars == 0 {
		return 0, false
	}
	minChars, maxTurns := t.MinNewChars, t.MaxTurns
	if minChars <= 0 {
		minChars = extractMinNewChars
	}
	if maxTurns <= 0 {
		maxTurns = extractMaxTurnsBetween
	}
	if newChars < minChars && t.turns < maxTurns {
		return 0, false
	}
	from = t.cursor
	t.cursor, t.turns, t.inFlight = len(messages), 0, true
	return from, true
}

// done ends the run begun by begin.
func (t *ExtractThrottle) done() {
	t.mu.Lock()
	t.inFlight = false
	t.mu.Unlock()
}

// captured marks everything so far as handled without running, e.g. when the
// main agent already wrote memories this turn.
func (t *ExtractThrottle) captured(sessionID string, messages []types.Message) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.sync(sessionID, len(messages))
	t.cursor, t.turns = len(messages), 0
}

// conversationTextBytes sums the user/assistant text that a summary would
// include.
func conversationTextBytes(messages []types.Message) int {
	n := 0
	for _, msg := range messages {
		if msg.Role == types.RoleUser || msg.Role == types.RoleAssistant {
			n += len(extractTextContent(msg))
		}
	}
	return n
}
