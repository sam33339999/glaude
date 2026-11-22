package acpclient

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	acp "github.com/coder/acp-go-sdk"
)

func testSpecs() map[string]AgentSpec {
	return map[string]AgentSpec{
		"fake": {Name: "fake", Command: "fake", Model: "m1", SessionMeta: map[string]any{"yoloMode": false}},
	}
}

func newTestManager(t *testing.T, ps *pipeStarter, perm PermissionFunc) *Manager {
	t.Helper()
	m := NewManager(testSpecs(), Options{Start: ps.start, Permission: perm, StartupTimeout: 5 * time.Second})
	t.Cleanup(func() { _ = m.Close() })
	return m
}

func promptCtx(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	return ctx
}

func TestManager_SessionPersistsAcrossPrompts(t *testing.T) {
	ps := &pipeStarter{}
	m := newTestManager(t, ps, nil)
	ctx := promptCtx(t)

	r1, err := m.Prompt(ctx, "fake", "/w", "echo:a", nil)
	if err != nil {
		t.Fatalf("prompt 1: %v", err)
	}
	if r1.Text != "turn 1: a" || !r1.NewSession || r1.Restarted || r1.StopReason != "end_turn" {
		t.Fatalf("r1 = %+v", r1)
	}
	r2, err := m.Prompt(ctx, "fake", "/w", "echo:b", nil)
	if err != nil {
		t.Fatalf("prompt 2: %v", err)
	}
	if r2.Text != "turn 2: b" || r2.NewSession {
		t.Fatalf("r2 = %+v, want same session turn 2", r2)
	}
	if ps.starts != 1 {
		t.Fatalf("starts = %d, want 1 (one long-lived process)", ps.starts)
	}
	a := ps.agent(0)
	if len(a.Models()) != 1 || a.Models()[0] != "m1" {
		t.Errorf("model config = %v, want [m1]", a.Models())
	}
	var yolo bool
	if err := a.Meta()[0].Get("yoloMode", &yolo); err != nil || yolo {
		t.Errorf("session meta yoloMode = %v (err %v), want false", yolo, err)
	}
	if a.Cwds()[0] != "/w" {
		t.Errorf("cwd = %q", a.Cwds()[0])
	}
}

func TestManager_SeparateSessionPerCwd(t *testing.T) {
	ps := &pipeStarter{}
	m := newTestManager(t, ps, nil)
	ctx := promptCtx(t)
	if _, err := m.Prompt(ctx, "fake", "/a", "echo:x", nil); err != nil {
		t.Fatal(err)
	}
	r, err := m.Prompt(ctx, "fake", "/b", "echo:y", nil)
	if err != nil {
		t.Fatal(err)
	}
	if r.Text != "turn 1: y" || ps.starts != 2 {
		t.Fatalf("r = %+v starts = %d, want a fresh session for another cwd", r, ps.starts)
	}
}

func TestManager_StreamsEvents(t *testing.T) {
	ps := &pipeStarter{}
	m := newTestManager(t, ps, nil)
	var mu sync.Mutex
	var kinds []EventKind
	var toolTitles []string
	r, err := m.Prompt(promptCtx(t), "fake", "/w", "tools", func(e Event) {
		mu.Lock()
		defer mu.Unlock()
		kinds = append(kinds, e.Kind)
		if e.Kind == EventToolUpdate {
			toolTitles = append(toolTitles, e.Title+"/"+e.Status)
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if r.Text != "done" {
		t.Fatalf("text = %q", r.Text)
	}
	want := []EventKind{EventNotice, EventThought, EventPlan, EventToolCall, EventToolUpdate, EventText}
	if len(kinds) != len(want) {
		t.Fatalf("kinds = %v, want %v", kinds, want)
	}
	for i := range want {
		if kinds[i] != want[i] {
			t.Fatalf("kinds = %v, want %v", kinds, want)
		}
	}
	if len(toolTitles) != 1 || toolTitles[0] != "Read main.go/completed" {
		t.Errorf("tool update = %v, want title carried over from the tool call", toolTitles)
	}
}

func TestManager_PermissionAllowAndDeny(t *testing.T) {
	ps := &pipeStarter{}
	var mu sync.Mutex
	var seen []PermissionQuery
	perm := func(_ context.Context, q PermissionQuery) (bool, error) {
		mu.Lock()
		seen = append(seen, q)
		mu.Unlock()
		return strings.Contains(string(q.Input), "go test"), nil
	}
	m := newTestManager(t, ps, perm)
	ctx := promptCtx(t)

	r, err := m.Prompt(ctx, "fake", "/w", "perm:go test ./...", nil)
	if err != nil || r.Text != "allowed" {
		t.Fatalf("allow: r=%+v err=%v (allow_once must be picked over allow_always)", r, err)
	}
	r, err = m.Prompt(ctx, "fake", "/w", "perm:rm -rf /", nil)
	if err != nil || r.Text != "denied" {
		t.Fatalf("deny: r=%+v err=%v", r, err)
	}
	if len(seen) != 2 || seen[0].ToolName != "Bash" || seen[0].Agent != "fake" {
		t.Fatalf("queries = %+v", seen)
	}
	var in map[string]string
	_ = json.Unmarshal(seen[1].Input, &in)
	if in["command"] != "rm -rf /" {
		t.Errorf("bash input = %v", in)
	}
}

func TestManager_NilPermissionRejects(t *testing.T) {
	m := newTestManager(t, &pipeStarter{}, nil)
	r, err := m.Prompt(promptCtx(t), "fake", "/w", "write:/w/new.txt", nil)
	if err != nil || r.Text != "denied" {
		t.Fatalf("r=%+v err=%v, want denied without a permission func", r, err)
	}
}

func TestManager_RestartAfterCrash(t *testing.T) {
	ps := &pipeStarter{}
	m := newTestManager(t, ps, nil)
	ctx := promptCtx(t)
	if _, err := m.Prompt(ctx, "fake", "/w", "echo:a", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Prompt(ctx, "fake", "/w", "crash", nil); err == nil {
		t.Fatal("crash prompt: want error")
	}
	var notices []string
	r, err := m.Prompt(ctx, "fake", "/w", "echo:b", func(e Event) {
		if e.Kind == EventNotice {
			notices = append(notices, e.Text)
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if !r.Restarted || !r.NewSession || r.Text != "turn 1: b" || ps.starts != 2 {
		t.Fatalf("r = %+v starts = %d, want restarted fresh session", r, ps.starts)
	}
	if len(notices) == 0 || !strings.Contains(notices[0], "previous context lost") {
		t.Errorf("notices = %v", notices)
	}
}

func TestManager_CancelPrompt(t *testing.T) {
	m := newTestManager(t, &pipeStarter{}, nil)
	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		_, err := m.Prompt(ctx, "fake", "/w", "block", nil)
		errCh <- err
	}()
	time.Sleep(100 * time.Millisecond)
	cancel()
	select {
	case err := <-errCh:
		if err == nil {
			t.Fatal("want error after cancel")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("prompt did not return after cancel")
	}
	// The session survives a cancelled turn.
	r, err := m.Prompt(promptCtx(t), "fake", "/w", "echo:after", nil)
	if err != nil || r.NewSession {
		t.Fatalf("after cancel: r=%+v err=%v, want same session", r, err)
	}
}

func TestManager_SerializesPrompts(t *testing.T) {
	ps := &pipeStarter{}
	m := newTestManager(t, ps, nil)
	ctx := promptCtx(t)
	var wg sync.WaitGroup
	results := make([]string, 4)
	for i := range results {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			r, err := m.Prompt(ctx, "fake", "/w", "echo:x", nil)
			if err != nil {
				t.Error(err)
			}
			results[i] = r.Text
		}(i)
	}
	wg.Wait()
	seen := map[string]bool{}
	for _, r := range results {
		seen[r] = true
	}
	if len(seen) != 4 || ps.starts != 1 {
		t.Fatalf("results = %v starts = %d, want 4 distinct turns in one session", results, ps.starts)
	}
}

// fastModelRetries removes the model-switch backoff for the test.
func fastModelRetries(t *testing.T) {
	t.Helper()
	old := modelRetryDelays
	modelRetryDelays = []time.Duration{0, time.Millisecond, time.Millisecond}
	t.Cleanup(func() { modelRetryDelays = old })
}

func TestManager_ModelSwitchRetriesColdStart(t *testing.T) {
	fastModelRetries(t)
	ps := &pipeStarter{}
	m := NewManager(map[string]AgentSpec{"fake": {Command: "fake", Model: "flaky-model"}}, Options{Start: ps.start})
	t.Cleanup(func() { _ = m.Close() })
	var notices []string
	if _, err := m.Prompt(promptCtx(t), "fake", "/w", "echo:a", func(e Event) {
		if e.Kind == EventNotice {
			notices = append(notices, e.Text)
		}
	}); err != nil {
		t.Fatal(err)
	}
	if got := ps.agent(0).Models(); len(got) != 2 {
		t.Fatalf("model attempts = %v, want a retry after the first rejection", got)
	}
	if len(notices) != 1 {
		t.Fatalf("notices = %v, want only the start notice (retry succeeded)", notices)
	}
}

func TestManager_ModelErrorIsNotice(t *testing.T) {
	fastModelRetries(t)
	ps := &pipeStarter{}
	m := NewManager(map[string]AgentSpec{"fake": {Command: "fake", Model: "bad-model"}}, Options{Start: ps.start})
	t.Cleanup(func() { _ = m.Close() })
	var notices []string
	r, err := m.Prompt(promptCtx(t), "fake", "/w", "echo:a", func(e Event) {
		if e.Kind == EventNotice {
			notices = append(notices, e.Text)
		}
	})
	if err != nil || r.Text != "turn 1: a" {
		t.Fatalf("r=%+v err=%v", r, err)
	}
	if len(notices) != 2 || !strings.Contains(notices[1], "bad-model") {
		t.Errorf("notices = %v", notices)
	}
	if got := ps.agent(0).Models(); len(got) != len(modelRetryDelays) {
		t.Errorf("model attempts = %v, want %d", got, len(modelRetryDelays))
	}
}

func TestManager_StartFailure(t *testing.T) {
	ps := &pipeStarter{fail: errors.New("boom")}
	m := newTestManager(t, ps, nil)
	if _, err := m.Prompt(promptCtx(t), "fake", "/w", "echo:a", nil); err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("err = %v", err)
	}
	if _, err := m.Prompt(promptCtx(t), "nope", "/w", "x", nil); err == nil {
		t.Fatal("unknown agent: want error")
	}
}

func TestManager_CloseStopsAndRejects(t *testing.T) {
	ps := &pipeStarter{}
	m := NewManager(testSpecs(), Options{Start: ps.start})
	if _, err := m.Prompt(promptCtx(t), "fake", "/w", "echo:a", nil); err != nil {
		t.Fatal(err)
	}
	if err := m.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-ps.agent(0).Connection().Done():
	case <-time.After(5 * time.Second):
		t.Fatal("agent still connected after Close")
	}
	if _, err := m.Prompt(promptCtx(t), "fake", "/w", "echo:a", nil); !errors.Is(err, ErrClosed) {
		t.Fatalf("err = %v, want ErrClosed", err)
	}
	_ = m.Close() // idempotent
}

// TestExecStart_RealProcess runs the test binary as a fake agent over real
// stdio pipes, covering ExecStart, env injection and process shutdown.
func TestExecStart_RealProcess(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Skip(err)
	}
	specs := map[string]AgentSpec{"bin": {Command: exe, Env: map[string]string{"GLAUDE_ACP_FAKE_AGENT": "1"}}}
	m := NewManager(specs, Options{})
	defer m.Close()
	ctx := promptCtx(t)
	r, err := m.Prompt(ctx, "bin", t.TempDir(), "echo:hi", nil)
	if err != nil || r.Text != "turn 1: hi" {
		t.Fatalf("r=%+v err=%v", r, err)
	}
	r, err = m.Prompt(ctx, "bin", r2cwd(m), "echo:again", nil)
	if err != nil || r.Text != "turn 2: again" {
		t.Fatalf("second prompt r=%+v err=%v", r, err)
	}
	// crash → error carries a stderr hint or exit; next prompt restarts.
	if _, err := m.Prompt(ctx, "bin", r2cwd(m), "crash", nil); err == nil {
		t.Fatal("crash: want error")
	}
	r, err = m.Prompt(ctx, "bin", r2cwd(m), "echo:back", nil)
	if err != nil || !r.Restarted || r.Text != "turn 1: back" {
		t.Fatalf("after crash r=%+v err=%v", r, err)
	}
}

// r2cwd returns the cwd of the manager's only slot (test helper).
func r2cwd(m *Manager) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	for k := range m.slots {
		return k.cwd
	}
	return ""
}

func TestExecStart_MissingCommand(t *testing.T) {
	_, err := ExecStart(context.Background(), AgentSpec{Command: "definitely-not-a-real-agent-xyz"}, t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "not found in PATH") {
		t.Fatalf("err = %v", err)
	}
}

func TestPickOption(t *testing.T) {
	opts := []acp.PermissionOption{
		{OptionId: "aa", Kind: acp.PermissionOptionKindAllowAlways},
		{OptionId: "ra", Kind: acp.PermissionOptionKindRejectAlways},
	}
	if id, ok := PickOption(opts, true); !ok || id != "aa" {
		t.Errorf("allow fallback = %q %v", id, ok)
	}
	if id, ok := PickOption(opts, false); !ok || id != "ra" {
		t.Errorf("reject fallback = %q %v", id, ok)
	}
	if _, ok := PickOption(nil, true); ok {
		t.Error("empty options: want !ok")
	}
}

func TestManager_PromptWithModel_ColdStartAndRuntimeSwitch(t *testing.T) {
	ps := &pipeStarter{}
	m := newTestManager(t, ps, nil)
	ctx := promptCtx(t)

	// Cold start with model override replaces spec.Model ("m1") in one call.
	if _, err := m.PromptWithModel(ctx, "fake", "/w", "gemini-4-argon-medium", "echo:1", nil); err != nil {
		t.Fatal(err)
	}
	// Same model on next turn does not re-send set_config_option.
	if _, err := m.PromptWithModel(ctx, "fake", "/w", "gemini-4-argon-medium", "echo:2", nil); err != nil {
		t.Fatal(err)
	}
	// Switching to another model mid-session sends set_config_option.
	if _, err := m.PromptWithModel(ctx, "fake", "/w", "gemini-3.8-flash", "echo:3", nil); err != nil {
		t.Fatal(err)
	}
	got := ps.agent(0).Models()
	if len(got) != 2 || got[0] != "gemini-4-argon-medium" || got[1] != "gemini-3.8-flash" {
		t.Fatalf("models = %v, want [gemini-4-argon-medium gemini-3.8-flash]", got)
	}

	cat, err := m.Models(ctx, "fake", "/w")
	if err != nil {
		t.Fatal(err)
	}
	if cat.Current != "gemini-3.8-flash" || len(cat.Models) != 3 || cat.ThoughtLevel != "high" || len(cat.ThoughtLevels) != 3 {
		t.Fatalf("catalog = %+v", cat)
	}
}
