package permissions

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/tunsuy/glaude/internal/tools"
	"github.com/tunsuy/glaude/pkg/types"
)

// newAskChecker returns a checker that asks for every "Agent" call, wired to
// fresh channels the way bootstrap wires the TUI (capacity 1 each).
func newAskChecker() (Checker, chan AskRequest, chan AskResponse) {
	askCh := make(chan AskRequest, 1)
	respCh := make(chan AskResponse, 1)
	c := NewChecker(CheckerConfig{
		PermCtx: types.ToolPermissionContext{
			Mode: types.PermissionModeDefault,
			AlwaysAskRules: types.ToolPermissionRulesBySource{
				types.RuleSourceUser: {"Agent"},
			},
		},
		AskCh:  askCh,
		RespCh: respCh,
	})
	return c, askCh, respCh
}

// requestAll issues one permission request per ID in parallel, the way the
// engine runs a batch of concurrency-safe tool calls.
func requestAll(ctx context.Context, c Checker, ids []string) map[string]tools.PermissionResult {
	var mu sync.Mutex
	var wg sync.WaitGroup
	out := make(map[string]tools.PermissionResult, len(ids))
	for _, id := range ids {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			r, _ := c.RequestPermission(ctx, PermissionRequest{ToolName: "Agent", ToolUseID: id}, nil)
			mu.Lock()
			out[id] = r
			mu.Unlock()
		}(id)
	}
	wg.Wait()
	return out
}

// fakeTUI models the real TUI: it keeps reading askCh, each new ask replaces
// the dialog on screen, and a human answers whatever dialog is showing after
// a short pause. answer picks the decision for a request ID. It returns the
// number of dialogs the human answered, read after stop() is called.
func fakeTUI(askCh <-chan AskRequest, respCh chan<- AskResponse, answer func(id string) AskResponse) (stop func() int) {
	var mu sync.Mutex
	var current *AskRequest
	answered := 0
	done := make(chan struct{})
	go func() {
		for {
			select {
			case req := <-askCh:
				mu.Lock()
				r := req
				current = &r
				mu.Unlock()
			case <-done:
				return
			}
		}
	}()
	go func() {
		t := time.NewTicker(30 * time.Millisecond)
		defer t.Stop()
		for {
			select {
			case <-t.C:
				mu.Lock()
				req := current
				current = nil
				if req != nil {
					answered++
				}
				mu.Unlock()
				if req != nil {
					resp := answer(req.ID)
					resp.ID = req.ID
					go func() { respCh <- resp }()
				}
			case <-done:
				return
			}
		}
	}()
	return func() int {
		close(done)
		mu.Lock()
		defer mu.Unlock()
		return answered
	}
}

// TestRequestPermission_ParallelAsksEachGetTheirOwnAnswer reproduces four
// parallel Agent calls asking at once. The TUI shows one dialog at a time;
// before askSem, later asks replaced the open dialog and answers reached
// whichever caller read respCh first, so three calls hung until the query was
// cancelled. Now each ask is shown in turn and gets its own answer.
func TestRequestPermission_ParallelAsksEachGetTheirOwnAnswer(t *testing.T) {
	c, askCh, respCh := newAskChecker()
	ids := []string{"a1", "a2", "a3", "a4"}
	stop := fakeTUI(askCh, respCh, func(id string) AskResponse {
		if id == "a3" {
			return AskResponse{Decision: tools.PermissionDeny}
		}
		return AskResponse{Decision: tools.PermissionAllow}
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	got := requestAll(ctx, c, ids)
	answered := stop()

	for _, id := range ids {
		want := tools.PermissionAllow
		if id == "a3" {
			want = tools.PermissionDeny
		}
		if got[id].Behavior != want {
			t.Errorf("%s: behavior = %q (%s), want %q", id, got[id].Behavior, got[id].Reason, want)
		}
	}
	if answered != len(ids) {
		t.Errorf("user answered %d dialogs, want %d", answered, len(ids))
	}
}

// TestRequestPermission_AlwaysAllowCoversWaitingAsks: answering "always
// allow" on the first of several identical parallel calls lets the waiting
// ones through without another dialog.
func TestRequestPermission_AlwaysAllowCoversWaitingAsks(t *testing.T) {
	c, askCh, respCh := newAskChecker()
	ids := []string{"b1", "b2", "b3", "b4"}
	stop := fakeTUI(askCh, respCh, func(string) AskResponse {
		return AskResponse{Decision: tools.PermissionAllow, AlwaysAllow: true}
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	got := requestAll(ctx, c, ids)
	answered := stop()

	for _, id := range ids {
		if got[id].Behavior != tools.PermissionAllow {
			t.Errorf("%s: behavior = %q (%s), want allow", id, got[id].Behavior, got[id].Reason)
		}
	}
	if answered != 1 {
		t.Errorf("dialogs answered = %d, want 1", answered)
	}
}

// TestRequestPermission_StaleResponseIgnored: an answer to a dialog whose
// caller already gave up must not be read as the next ask's decision.
func TestRequestPermission_StaleResponseIgnored(t *testing.T) {
	c, askCh, respCh := newAskChecker()

	// The stale answer (allow) is already waiting when c2 asks.
	respCh <- AskResponse{ID: "gone", Decision: tools.PermissionAllow}
	go func() {
		req := <-askCh
		respCh <- AskResponse{ID: req.ID, Decision: tools.PermissionDeny}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	r, err := c.RequestPermission(ctx, PermissionRequest{ToolName: "Agent", ToolUseID: "c2"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if r.Behavior != tools.PermissionDeny {
		t.Fatalf("behavior = %q (%s), want the user's deny for c2, not the stale allow", r.Behavior, r.Reason)
	}
}

// TestRequestPermission_CancelWhileQueued: a call waiting behind another
// dialog stops waiting when its context is cancelled.
func TestRequestPermission_CancelWhileQueued(t *testing.T) {
	c, askCh, _ := newAskChecker()
	ctx, cancel := context.WithCancel(context.Background())

	first := make(chan tools.PermissionResult, 1)
	go func() {
		r, _ := c.RequestPermission(ctx, PermissionRequest{ToolName: "Agent", ToolUseID: "d1"}, nil)
		first <- r
	}()
	<-askCh // d1's dialog is open and never answered

	second := make(chan tools.PermissionResult, 1)
	go func() {
		r, _ := c.RequestPermission(ctx, PermissionRequest{ToolName: "Agent", ToolUseID: "d2"}, nil)
		second <- r
	}()
	cancel()

	for i, ch := range []chan tools.PermissionResult{first, second} {
		select {
		case r := <-ch:
			if r.Behavior != tools.PermissionDeny {
				t.Errorf("call %d: behavior = %q, want deny after cancel", i+1, r.Behavior)
			}
		case <-time.After(5 * time.Second):
			t.Fatal(fmt.Sprintf("call %d still waiting after cancel", i+1))
		}
	}
}
