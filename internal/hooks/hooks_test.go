package hooks

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tunsuy/glaude/pkg/types"
)

func cmd(c string) types.HookCommand { return types.HookCommand{Type: "command", Command: c} }

func dispatcher(t *testing.T, event types.HookType, matcher string, cmds ...string) *Dispatcher {
	t.Helper()
	var hs []types.HookCommand
	for _, c := range cmds {
		hs = append(hs, cmd(c))
	}
	return NewDispatcher(Config{
		Hooks:      map[types.HookType][]types.HookMatcher{event: {{Matcher: matcher, Hooks: hs}}},
		ProjectDir: t.TempDir(),
	})
}

func bashEvent() ToolEvent {
	return ToolEvent{ToolName: "Bash", ToolInput: json.RawMessage(`{"command":"ls"}`), ToolUseID: "tu_1", SessionID: "s1"}
}

func TestMatcher(t *testing.T) {
	cases := []struct {
		matcher string
		tool    string
		want    bool
	}{
		{"", "Bash", true},
		{"*", "Write", true},
		{"Bash", "Bash", true},
		{"Bash", "BashOutput", false}, // simple names are exact
		{"Write|Edit", "Edit", true},
		{"Write|Edit", "Read", false},
		{"mcp__.*", "mcp__github__search", true},
		{"Notebook.*", "NotebookEdit", true},
		{"mcp__memory__.*", "mcp__github__x", false},
		{"[invalid", "Bash", false},
		{"Edit, Write", "Write", true},
		{"Edit | Write", "Edit", true},
		{"my-tool", "my-tool", true},
		{"^Bash$", "Bash", true},
	}
	for _, c := range cases {
		d := dispatcher(t, types.HookPreToolUse, c.matcher, "true")
		if got := d.Has(types.HookPreToolUse, c.tool); got != c.want {
			t.Errorf("matcher %q tool %q: got %v want %v", c.matcher, c.tool, got, c.want)
		}
	}
}

func TestNilAndDisabled(t *testing.T) {
	var d *Dispatcher
	if d.Has(types.HookPreToolUse, "Bash") {
		t.Fatal("nil dispatcher should have no hooks")
	}
	if r := d.RunPreToolUse(context.Background(), bashEvent()); r.Decision != DecisionNone {
		t.Fatalf("nil dispatcher decision = %q", r.Decision)
	}
	off := NewDispatcher(Config{
		Hooks:    map[types.HookType][]types.HookMatcher{types.HookPreToolUse: {{Hooks: []types.HookCommand{cmd("exit 2")}}}},
		Disabled: true,
	})
	if off.Has(types.HookPreToolUse, "Bash") {
		t.Fatal("disabled dispatcher should run nothing")
	}
}

func TestUnsupportedTypeSkipped(t *testing.T) {
	d := NewDispatcher(Config{Hooks: map[types.HookType][]types.HookMatcher{
		types.HookPreToolUse: {{Hooks: []types.HookCommand{{Type: "prompt", Command: "x"}}}},
	}})
	if d.Has(types.HookPreToolUse, "Bash") {
		t.Fatal("prompt-type hooks are not supported and must be skipped")
	}
}

func TestPayloadAndEnv(t *testing.T) {
	dir := t.TempDir()
	in := filepath.Join(dir, "in.json")
	env := filepath.Join(dir, "env")
	d := NewDispatcher(Config{
		Hooks: map[types.HookType][]types.HookMatcher{types.HookPreToolUse: {{Hooks: []types.HookCommand{
			cmd("cat > " + in + "; echo \"$CLAUDE_PROJECT_DIR|$GLAUDE_PROJECT_DIR|$(pwd)\" > " + env),
		}}}},
		ProjectDir: "/proj",
		ModeFn:     func() string { return "acceptEdits" },
	})
	ev := bashEvent()
	ev.Cwd = dir
	ev.TranscriptPath = "/t.jsonl"
	d.RunPreToolUse(context.Background(), ev)

	var got map[string]any
	b, err := os.ReadFile(in)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("stdin not JSON: %v (%s)", err, b)
	}
	want := map[string]any{
		"session_id": "s1", "transcript_path": "/t.jsonl", "cwd": dir, "permission_mode": "acceptEdits",
		"hook_event_name": "PreToolUse", "tool_name": "Bash", "tool_use_id": "tu_1",
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("payload[%s] = %v, want %v", k, got[k], v)
		}
	}
	if ti, _ := got["tool_input"].(map[string]any); ti["command"] != "ls" {
		t.Errorf("tool_input = %v", got["tool_input"])
	}
	e, _ := os.ReadFile(env)
	realDir, _ := filepath.EvalSymlinks(dir)
	if s := strings.TrimSpace(string(e)); s != "/proj|/proj|"+dir && s != "/proj|/proj|"+realDir {
		t.Errorf("env/cwd = %q", s)
	}
}

func TestPreExitCodes(t *testing.T) {
	ctx := context.Background()
	r := dispatcher(t, types.HookPreToolUse, "Bash", "echo 'no rm' >&2; exit 2").RunPreToolUse(ctx, bashEvent())
	if r.Decision != DecisionDeny || !strings.Contains(r.Reason, "no rm") {
		t.Errorf("exit 2: decision=%q reason=%q", r.Decision, r.Reason)
	}
	r = dispatcher(t, types.HookPreToolUse, "Bash", "echo oops >&2; exit 1").RunPreToolUse(ctx, bashEvent())
	if r.Decision != DecisionNone || len(r.Notices) != 1 || !strings.Contains(r.Notices[0], "status code 1: oops") {
		t.Errorf("exit 1: decision=%q notices=%v", r.Decision, r.Notices)
	}
	r = dispatcher(t, types.HookPreToolUse, "Bash", "echo plain text").RunPreToolUse(ctx, bashEvent())
	if r.Decision != DecisionNone || len(r.Notices) != 0 {
		t.Errorf("plain stdout: %+v", r)
	}
}

func TestPreJSONDecisions(t *testing.T) {
	ctx := context.Background()
	out := func(s string) string { return "echo '" + s + "'" }
	r := dispatcher(t, types.HookPreToolUse, "", out(`{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"allow","permissionDecisionReason":"ok","updatedInput":{"command":"ls -la"},"additionalContext":"ctx"},"systemMessage":"hi"}`)).RunPreToolUse(ctx, bashEvent())
	if r.Decision != DecisionAllow || r.Reason != "ok" || string(r.UpdatedInput) != `{"command":"ls -la"}` {
		t.Errorf("allow: %+v", r)
	}
	if len(r.AdditionalContext) != 1 || r.AdditionalContext[0] != "ctx" || len(r.Notices) != 1 || r.Notices[0] != "hi" {
		t.Errorf("allow extras: %+v", r)
	}

	r = dispatcher(t, types.HookPreToolUse, "", out(`{"decision":"block","reason":"legacy no"}`)).RunPreToolUse(ctx, bashEvent())
	if r.Decision != DecisionDeny || r.Reason != "legacy no" {
		t.Errorf("legacy block: %+v", r)
	}
	r = dispatcher(t, types.HookPreToolUse, "", out(`{"decision":"approve"}`)).RunPreToolUse(ctx, bashEvent())
	if r.Decision != DecisionAllow {
		t.Errorf("legacy approve: %+v", r)
	}

	// continue:false stops processing but does not block the call itself.
	r = dispatcher(t, types.HookPreToolUse, "", out(`{"continue":false,"stopReason":"halt"}`)).RunPreToolUse(ctx, bashEvent())
	if r.Decision != DecisionNone || !r.PreventContinuation || r.StopReason != "halt" {
		t.Errorf("continue false: %+v", r)
	}

	// "defer" is no decision: it never hides another hook's ask.
	r = dispatcher(t, types.HookPreToolUse, "", out(`{"hookSpecificOutput":{"permissionDecision":"defer"}}`), out(`{"hookSpecificOutput":{"permissionDecision":"ask"}}`)).RunPreToolUse(ctx, bashEvent())
	if r.Decision != DecisionAsk {
		t.Errorf("defer + ask: %+v", r)
	}
	r = dispatcher(t, types.HookPreToolUse, "", out(`{"hookSpecificOutput":{"permissionDecision":"defer"}}`)).RunPreToolUse(ctx, bashEvent())
	if r.Decision != DecisionNone {
		t.Errorf("defer alone: %+v", r)
	}
}

func TestPrePrecedence(t *testing.T) {
	allow := `echo '{"hookSpecificOutput":{"permissionDecision":"allow"}}'`
	ask := `echo '{"hookSpecificOutput":{"permissionDecision":"ask","permissionDecisionReason":"sure?"}}'`
	deny := `echo '{"hookSpecificOutput":{"permissionDecision":"deny","permissionDecisionReason":"nope"}}'`
	ctx := context.Background()
	if r := dispatcher(t, types.HookPreToolUse, "", allow, ask).RunPreToolUse(ctx, bashEvent()); r.Decision != DecisionAsk || r.Reason != "sure?" {
		t.Errorf("allow+ask: %+v", r)
	}
	if r := dispatcher(t, types.HookPreToolUse, "", ask, deny, allow).RunPreToolUse(ctx, bashEvent()); r.Decision != DecisionDeny || r.Reason != "nope" {
		t.Errorf("ask+deny+allow: %+v", r)
	}
}

func TestParallelAndDedup(t *testing.T) {
	dir := t.TempDir()
	count := filepath.Join(dir, "count")
	slow := "sleep 0.5; echo x >> " + count
	d := NewDispatcher(Config{Hooks: map[types.HookType][]types.HookMatcher{types.HookPreToolUse: {
		{Matcher: "Bash", Hooks: []types.HookCommand{cmd(slow), cmd(slow + " ")}},
		{Matcher: "*", Hooks: []types.HookCommand{cmd(slow)}}, // duplicate command string
	}}})
	start := time.Now()
	d.RunPreToolUse(context.Background(), bashEvent())
	if el := time.Since(start); el > 900*time.Millisecond {
		t.Errorf("hooks did not run in parallel: %v", el)
	}
	b, _ := os.ReadFile(count)
	if n := strings.Count(string(b), "x"); n != 2 {
		t.Errorf("ran %d commands, want 2 (identical command de-duplicated)", n)
	}
}

func TestTimeout(t *testing.T) {
	d := NewDispatcher(Config{Hooks: map[types.HookType][]types.HookMatcher{types.HookPreToolUse: {
		{Hooks: []types.HookCommand{{Type: "command", Command: "sleep 5; exit 2", Timeout: 1}}},
	}}})
	start := time.Now()
	r := d.RunPreToolUse(context.Background(), bashEvent())
	if time.Since(start) > 3*time.Second {
		t.Fatal("timeout not enforced")
	}
	if r.Decision != DecisionNone || len(r.Notices) != 1 || !strings.Contains(r.Notices[0], "timed out") {
		t.Errorf("timeout result: %+v", r)
	}
}

func TestCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(200 * time.Millisecond); cancel() }()
	r := dispatcher(t, types.HookPreToolUse, "", "sleep 5").RunPreToolUse(ctx, bashEvent())
	if r.Decision != DecisionNone || len(r.Notices) != 0 {
		t.Errorf("cancelled hook should be silent: %+v", r)
	}
}

func TestPost(t *testing.T) {
	dir := t.TempDir()
	in := filepath.Join(dir, "in.json")
	ctx := context.Background()
	d := dispatcher(t, types.HookPostToolUse, "Bash",
		"cat > "+in+`; echo '{"decision":"block","reason":"lint failed","hookSpecificOutput":{"additionalContext":"see logs"}}'`,
		"echo fmt >&2; exit 2")
	r := d.RunPostToolUse(ctx, bashEvent(), json.RawMessage(`{"stdout":"a"}`))
	if len(r.Feedback) != 2 || !strings.Contains(r.Feedback[0], "lint failed") || !strings.Contains(r.Feedback[1], "fmt") {
		t.Errorf("feedback = %v", r.Feedback)
	}
	if len(r.AdditionalContext) != 1 || r.AdditionalContext[0] != "see logs" {
		t.Errorf("additionalContext = %v", r.AdditionalContext)
	}
	var got map[string]any
	b, _ := os.ReadFile(in)
	_ = json.Unmarshal(b, &got)
	if got["hook_event_name"] != "PostToolUse" {
		t.Errorf("event name = %v", got["hook_event_name"])
	}
	if resp, _ := got["tool_response"].(map[string]any); resp["stdout"] != "a" {
		t.Errorf("tool_response = %v", got["tool_response"])
	}

	mcp := dispatcher(t, types.HookPostToolUse, "", `echo '{"hookSpecificOutput":{"updatedMCPToolOutput":"redacted"}}'`)
	if r := mcp.RunPostToolUse(ctx, bashEvent(), nil); string(r.UpdatedMCPToolOutput) != `"redacted"` {
		t.Errorf("updatedMCPToolOutput = %s", r.UpdatedMCPToolOutput)
	}
}

func TestPostFailure(t *testing.T) {
	dir := t.TempDir()
	in := filepath.Join(dir, "in.json")
	d := dispatcher(t, types.HookPostToolUseFailure, "", "cat > "+in)
	if d.Has(types.HookPostToolUse, "Bash") {
		t.Fatal("failure hook must not register as PostToolUse")
	}
	d.RunPostToolUseFailure(context.Background(), bashEvent(), "boom", false)
	var got map[string]any
	b, _ := os.ReadFile(in)
	_ = json.Unmarshal(b, &got)
	if got["hook_event_name"] != "PostToolUseFailure" || got["error"] != "boom" || got["is_interrupt"] != false {
		t.Errorf("failure payload = %v", got)
	}
}

func TestExitCodeJSONRules(t *testing.T) {
	ctx := context.Background()
	// Exit 2 still blocks, even with an "allow" JSON, and its common fields apply.
	r := dispatcher(t, types.HookPreToolUse, "", `echo '{"systemMessage":"seen","hookSpecificOutput":{"permissionDecision":"allow"}}'; echo nope >&2; exit 2`).RunPreToolUse(ctx, bashEvent())
	if r.Decision != DecisionDeny || !strings.Contains(r.Reason, "nope") || len(r.Notices) != 1 || r.Notices[0] != "seen" {
		t.Errorf("exit 2 + JSON: %+v", r)
	}
	// Any other exit code with valid JSON: the fields decide.
	r = dispatcher(t, types.HookPreToolUse, "", `echo '{"hookSpecificOutput":{"permissionDecision":"deny","permissionDecisionReason":"json wins"}}'; exit 1`).RunPreToolUse(ctx, bashEvent())
	if r.Decision != DecisionDeny || r.Reason != "json wins" || len(r.Notices) != 0 {
		t.Errorf("exit 1 + JSON: %+v", r)
	}
	// Exit 0 with broken JSON: non-blocking parse failure.
	r = dispatcher(t, types.HookPreToolUse, "", `echo '{not json}'`).RunPreToolUse(ctx, bashEvent())
	if r.Decision != DecisionNone || len(r.Notices) != 1 || !strings.Contains(r.Notices[0], "invalid JSON") {
		t.Errorf("bad JSON: %+v", r)
	}
}

func TestOnFailureBlock(t *testing.T) {
	d := NewDispatcher(Config{Hooks: map[types.HookType][]types.HookMatcher{
		types.HookPreToolUse:  {{Hooks: []types.HookCommand{{Command: "exit 1", OnFailure: "block"}}}},
		types.HookPostToolUse: {{Hooks: []types.HookCommand{{Command: "sleep 3", Timeout: 1, OnFailure: "block"}}}},
	}})
	if r := d.RunPreToolUse(context.Background(), bashEvent()); r.Decision != DecisionDeny || !strings.Contains(r.Reason, "onFailure") {
		t.Errorf("pre onFailure block: %+v", r)
	}
	if r := d.RunPostToolUse(context.Background(), bashEvent(), nil); len(r.Feedback) != 1 || !strings.Contains(r.Feedback[0], "timed out") {
		t.Errorf("post onFailure block: %+v", r)
	}
	// Exit 1 with an "allow" JSON still blocks when onFailure is "block".
	d2 := NewDispatcher(Config{Hooks: map[types.HookType][]types.HookMatcher{types.HookPreToolUse: {{Hooks: []types.HookCommand{
		{Command: `echo '{"hookSpecificOutput":{"permissionDecision":"allow"}}'; exit 1`, OnFailure: "block"},
	}}}}})
	if r := d2.RunPreToolUse(context.Background(), bashEvent()); r.Decision != DecisionDeny {
		t.Errorf("onFailure block + exit 1 JSON: %+v", r)
	}
}

func TestArgsExecForm(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "out")
	d := NewDispatcher(Config{Hooks: map[types.HookType][]types.HookMatcher{
		types.HookPreToolUse: {{Hooks: []types.HookCommand{{Command: "/bin/sh", Args: []string{"-c", "cat > " + out + "; echo '{\"decision\":\"block\",\"reason\":\"via args\"}'"}}}}},
	}})
	r := d.RunPreToolUse(context.Background(), bashEvent())
	if r.Decision != DecisionDeny || r.Reason != "via args" {
		t.Errorf("args: %+v", r)
	}
	if b, _ := os.ReadFile(out); !strings.Contains(string(b), "PreToolUse") {
		t.Errorf("args hook did not get stdin: %q", b)
	}
}

func TestIfFilter(t *testing.T) {
	var seen []string
	d := NewDispatcher(Config{
		Hooks: map[types.HookType][]types.HookMatcher{types.HookPreToolUse: {{Hooks: []types.HookCommand{{Command: "exit 2", If: "Bash(git *)"}}}}},
		IfMatcher: func(rule, tool string, input json.RawMessage) bool {
			seen = append(seen, rule)
			return strings.Contains(string(input), "git ")
		},
	})
	if r := d.RunPreToolUse(context.Background(), bashEvent()); r.Decision != DecisionNone {
		t.Errorf("non-matching if ran the hook: %+v", r)
	}
	ev := bashEvent()
	ev.ToolInput = json.RawMessage(`{"command":"git push"}`)
	if r := d.RunPreToolUse(context.Background(), ev); r.Decision != DecisionDeny {
		t.Errorf("matching if did not run the hook: %+v", r)
	}
	if len(seen) == 0 || seen[0] != "Bash(git *)" {
		t.Errorf("IfMatcher calls = %v", seen)
	}
}

func TestWarningsShownOnce(t *testing.T) {
	// Only unsupported hooks: nothing is registered, but the warnings remain.
	d := NewDispatcher(Config{Hooks: map[types.HookType][]types.HookMatcher{types.HookPreToolUse: {{Hooks: []types.HookCommand{
		{Type: "http", Command: "x"},
		{Command: "true", If: "Bash(x)"}, // no IfMatcher configured
	}}}}})
	if d.Has(types.HookPreToolUse, "Bash") {
		t.Fatal("unsupported hooks must not register")
	}
	w := d.TakeWarnings()
	if len(w) != 2 || !strings.Contains(w[0]+w[1], "http") || !strings.Contains(w[0]+w[1], "if") {
		t.Errorf("warnings = %v", w)
	}
	if w := d.TakeWarnings(); len(w) != 0 {
		t.Errorf("warnings repeated: %v", w)
	}
}

// A hook that exits 0 while a background child keeps stdout open must keep
// its JSON decision.
func TestBackgroundChildKeepsDecision(t *testing.T) {
	r := dispatcher(t, types.HookPreToolUse, "", `echo '{"hookSpecificOutput":{"permissionDecision":"deny","permissionDecisionReason":"bg"}}'; (sleep 3 &)`).RunPreToolUse(context.Background(), bashEvent())
	if r.Decision != DecisionDeny || r.Reason != "bg" {
		t.Errorf("result = %+v", r)
	}
}

func TestAsyncDoesNotWait(t *testing.T) {
	d := NewDispatcher(Config{Hooks: map[types.HookType][]types.HookMatcher{types.HookPreToolUse: {{Hooks: []types.HookCommand{
		{Command: "sleep 2; exit 2", Async: true},
	}}}}})
	start := time.Now()
	r := d.RunPreToolUse(context.Background(), bashEvent())
	if time.Since(start) > time.Second || r.Decision != DecisionNone {
		t.Errorf("async hook blocked or decided: %+v after %v", r, time.Since(start))
	}
}
