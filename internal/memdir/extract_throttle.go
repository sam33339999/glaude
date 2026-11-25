package memdir

import (
	"encoding/json"
	"hash/fnv"
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
	// mark fingerprints messages[cursor-1], so a history that was compacted
	// or cleared and then grew past the cursor is still recognised as new.
	mark     uint64
	turns    int
	inFlight bool
	// prev is the cursor (and mark) before the in-flight run, restored if
	// that run fails so its messages go to the next run.
	prevCursor int
	prevMark   uint64
	runCursor  int
}

// NewExtractThrottle returns a throttle with the default triggers.
func NewExtractThrottle() *ExtractThrottle {
	return &ExtractThrottle{MinNewChars: extractMinNewChars, MaxTurns: extractMaxTurnsBetween}
}

// sync resets the cursor when the session changed or the history is no
// longer the one the cursor points into: it shrank, or the message before
// the cursor changed (compaction or /clear, even if the history has since
// grown past the cursor). A reset makes everything count as new, so after a
// compaction its summary is extracted once more. Callers hold mu.
func (t *ExtractThrottle) sync(sessionID string, messages []types.Message) {
	if sessionID != t.sessionID || len(messages) < t.cursor ||
		(t.cursor > 0 && fingerprint(messages[t.cursor-1]) != t.mark) {
		t.sessionID, t.cursor, t.mark, t.turns = sessionID, 0, 0, 0
	}
}

// setCursor moves the cursor to n, fingerprinting the message before it.
// Callers hold mu.
func (t *ExtractThrottle) setCursor(messages []types.Message, n int) {
	t.cursor, t.mark = n, 0
	if n > 0 {
		t.mark = fingerprint(messages[n-1])
	}
}

// fingerprint hashes a message's content (FNV-64a over its JSON).
func fingerprint(m types.Message) uint64 {
	h := fnv.New64a()
	b, _ := json.Marshal(m)
	_, _ = h.Write(b)
	return h.Sum64()
}

// begin records one finished foreground turn and reports whether extraction
// should run now. When it should, from is the cursor to summarise from; the
// cursor moves to the end of messages and the caller must call done.
// A run already in flight defers the next one (its turn still counts).
func (t *ExtractThrottle) begin(sessionID string, messages []types.Message) (from int, run bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.sync(sessionID, messages)
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
	t.prevCursor, t.prevMark = t.cursor, t.mark
	t.setCursor(messages, len(messages))
	t.runCursor = t.cursor
	t.turns, t.inFlight = 0, true
	return from, true
}

// done ends the run begun by begin. A failed run (ok=false) moves the cursor
// back so its messages are summarised again next time — unless the cursor
// has moved since (reset or captured), which already decided for them.
func (t *ExtractThrottle) done(ok bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.inFlight = false
	if !ok && t.cursor == t.runCursor {
		t.cursor, t.mark = t.prevCursor, t.prevMark
	}
}

// captured marks everything so far as handled without running, e.g. when the
// main agent already wrote memories this turn.
func (t *ExtractThrottle) captured(sessionID string, messages []types.Message) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.sync(sessionID, messages)
	t.setCursor(messages, len(messages))
	t.turns = 0
	// An in-flight run must not roll this back.
	t.runCursor = -1
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
