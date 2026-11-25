package memdir

import (
	"context"
	"strings"
	"testing"

	"github.com/tunsuy/glaude/internal/engine"
	"github.com/tunsuy/glaude/internal/tools"
	"github.com/tunsuy/glaude/pkg/types"
)

// turn appends one user/assistant exchange with n bytes of user text.
func turn(msgs []types.Message, n int) []types.Message {
	return append(msgs,
		types.Message{Role: types.RoleUser, Content: []types.ContentBlock{textBlock(strings.Repeat("u", n))}},
		types.Message{Role: types.RoleAssistant, Content: []types.ContentBlock{textBlock("ok")}},
	)
}

func TestExtractThrottle_ShortTurnsWaitForMaxTurns(t *testing.T) {
	th := &ExtractThrottle{MinNewChars: 2000, MaxTurns: 3}
	var msgs []types.Message
	for i := 1; i <= 2; i++ {
		msgs = turn(msgs, 10)
		if _, run := th.begin("s", msgs); run {
			t.Fatalf("turn %d: short turn must not run before MaxTurns", i)
		}
	}
	msgs = turn(msgs, 10)
	from, run := th.begin("s", msgs)
	if !run || from != 0 {
		t.Fatalf("turn 3: run=%v from=%d, want run from 0", run, from)
	}
	th.done()
}

func TestExtractThrottle_EnoughTextRunsAndMovesCursor(t *testing.T) {
	th := &ExtractThrottle{MinNewChars: 100, MaxTurns: 10}
	msgs := turn(nil, 150)
	if from, run := th.begin("s", msgs); !run || from != 0 {
		t.Fatalf("first: run=%v from=%d", run, from)
	}
	th.done()
	n := len(msgs)
	msgs = turn(msgs, 150)
	from, run := th.begin("s", msgs)
	if !run || from != n {
		t.Fatalf("second: run=%v from=%d, want from=%d (only new messages)", run, from, n)
	}
	th.done()
}

func TestExtractThrottle_NoNewTextNeverRuns(t *testing.T) {
	th := &ExtractThrottle{MinNewChars: 100, MaxTurns: 1}
	msgs := turn(nil, 150)
	th.begin("s", msgs)
	th.done()
	// Same history again (e.g. a tool-only turn adds no text).
	if _, run := th.begin("s", msgs); run {
		t.Fatal("no new text: must not run even past MaxTurns")
	}
}

func TestExtractThrottle_InFlightDefers(t *testing.T) {
	th := &ExtractThrottle{MinNewChars: 10, MaxTurns: 10}
	msgs := turn(nil, 50)
	if _, run := th.begin("s", msgs); !run {
		t.Fatal("first should run")
	}
	msgs = turn(msgs, 50)
	if _, run := th.begin("s", msgs); run {
		t.Fatal("must not start a second run while one is in flight")
	}
	th.done()
	if _, run := th.begin("s", msgs); !run {
		t.Fatal("after done, pending text should run")
	}
	th.done()
}

func TestExtractThrottle_ResetsOnShrinkAndSessionChange(t *testing.T) {
	th := &ExtractThrottle{MinNewChars: 10, MaxTurns: 10}
	msgs := turn(turn(nil, 50), 50)
	th.begin("s", msgs)
	th.done()

	// Compaction: history shrank below the cursor.
	compacted := turn(nil, 50)
	if from, run := th.begin("s", compacted); !run || from != 0 {
		t.Fatalf("after shrink: run=%v from=%d, want run from 0", run, from)
	}
	th.done()

	// New session with a longer history: the cursor must not carry over.
	other := turn(turn(turn(nil, 50), 50), 50)
	if from, run := th.begin("s2", other); !run || from != 0 {
		t.Fatalf("new session: run=%v from=%d, want run from 0", run, from)
	}
	th.done()
}

func TestExtractThrottle_CapturedAdvancesCursor(t *testing.T) {
	th := &ExtractThrottle{MinNewChars: 100, MaxTurns: 10}
	msgs := turn(nil, 500)
	th.captured("s", msgs)
	msgs = turn(msgs, 10)
	if _, run := th.begin("s", msgs); run {
		t.Fatal("text before captured() must not count again")
	}
}

func TestExecuteExtractMemories_ThrottleSkipsShortTurn(t *testing.T) {
	t.Parallel()

	var logged []string
	cfg := ExtractMemoriesConfig{
		Enabled:  true,
		Store:    NewMemoryStoreWithPath(t.TempDir()),
		Throttle: NewExtractThrottle(),
		Logf:     func(f string, a ...any) { logged = append(logged, f) },
	}
	hookCtx := &engine.StopHookContext{
		QuerySource:    foregroundQuerySource,
		ToolUseContext: &tools.UseContext{SessionID: "s"},
		Messages:       turn(nil, 20),
		// Engine nil: reaching RunForkedAgent would log an error.
	}
	ExecuteExtractMemories(context.Background(), hookCtx, cfg)
	if len(logged) != 0 {
		t.Fatalf("short first turn should be throttled, but extraction ran: %q", logged)
	}
}

func TestBuildConversationSummary_KeepsNewestWhenOverCap(t *testing.T) {
	t.Parallel()

	old := strings.Repeat("old ", 3000) // ~12,000 bytes, over the cap alone
	msgs := []types.Message{
		{Role: types.RoleUser, Content: []types.ContentBlock{textBlock(old)}},
		{Role: types.RoleUser, Content: []types.ContentBlock{textBlock("NEWEST: remember I use fnm")}},
	}
	summary := buildConversationSummary(msgs, 10)
	if !strings.Contains(summary, "NEWEST: remember I use fnm") {
		t.Fatal("newest message must survive the cap")
	}
	if len(summary) > maxSummaryChars+10 {
		t.Errorf("summary length %d over cap %d", len(summary), maxSummaryChars)
	}
	if i, j := strings.Index(summary, "old"), strings.Index(summary, "NEWEST"); i < 0 || i > j {
		t.Error("kept messages should stay in chronological order")
	}
}
