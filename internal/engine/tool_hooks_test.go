package engine

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/tunsuy/glaude/internal/api"
	"github.com/tunsuy/glaude/internal/hooks"
	"github.com/tunsuy/glaude/internal/permissions"
	"github.com/tunsuy/glaude/internal/tools"
	"github.com/tunsuy/glaude/pkg/types"
)

// recTool records the input of each Call and can fail on demand.
type recTool struct {
	stubTool
	mu     sync.Mutex
	inputs []string
	fail   bool
}

func (r *recTool) Call(in tools.Input, _ *tools.UseContext, _ tools.OnProgressFn) (*tools.Result, error) {
	r.mu.Lock()
	r.inputs = append(r.inputs, string(in))
	r.mu.Unlock()
	if r.fail {
		return nil, errors.New("tool exploded")
	}
	return &tools.Result{Content: map[string]string{"stdout": "done"}}, nil
}

func (r *recTool) MapResultToToolResultBlock(_ any, _ string) (json.RawMessage, error) {
	return json.RawMessage(`{"content":"done"}`), nil
}

func (r *recTool) calls() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.inputs...)
}

// hookChecker is a permissions.Checker that answers every request with
// behavior and records requests; hookAllowed is what CanUseToolHookAllowed
// returns (zero value = allow).
type hookChecker struct {
	behavior    tools.PermissionBehavior
	hookAllowed tools.PermissionResult
	mu          sync.Mutex
	reqs        []permissions.PermissionRequest
}

func (c *hookChecker) CanUseTool(context.Context, string, tools.Input, *tools.UseContext) (tools.PermissionResult, error) {
	return tools.PermissionResult{Behavior: c.behavior, Reason: "checker says " + string(c.behavior)}, nil
}

func (c *hookChecker) RequestPermission(_ context.Context, req permissions.PermissionRequest, _ *tools.UseContext) (tools.PermissionResult, error) {
	c.mu.Lock()
	c.reqs = append(c.reqs, req)
	c.mu.Unlock()
	if req.ForceAsk {
		return tools.PermissionResult{Behavior: tools.PermissionAllow, Reason: "user approved"}, nil
	}
	return tools.PermissionResult{Behavior: c.behavior, Reason: "checker says " + string(c.behavior)}, nil
}

func (c *hookChecker) GetDenialCount() int { return 0 }

func (c *hookChecker) CanUseToolHookAllowed(context.Context, string, tools.Input, *tools.UseContext) (tools.PermissionResult, error) {
	if c.hookAllowed.Behavior == "" {
		return tools.PermissionResult{Behavior: tools.PermissionAllow}, nil
	}
	return c.hookAllowed, nil
}

func (c *hookChecker) MatchRule(string, string, tools.Input) bool { return true }

func hookDispatcher(event types.HookType, command string) *hooks.Dispatcher {
	return hooks.NewDispatcher(hooks.Config{Hooks: map[types.HookType][]types.HookMatcher{
		event: {{Matcher: "Bash", Hooks: []types.HookCommand{{Type: "command", Command: command}}}},
	}})
}

func runOne(t *testing.T, tool *recTool, chk permissions.Checker, d *hooks.Dispatcher, uctx *tools.UseContext) (types.ContentBlock, *toolHooks, []Msg) {
	t.Helper()
	if uctx == nil {
		uctx = &tools.UseContext{Ctx: context.Background()}
	}
	msgCh := make(chan Msg, 64)
	th := newToolHooks(d)
	block, err := executeOneTool(context.Background(), toolCall{id: "tu_1", name: "Bash", input: json.RawMessage(`{"command":"rm -rf /"}`)},
		newTestRegistry(tool), uctx, msgCh, chk, th)
	if err != nil {
		t.Fatalf("executeOneTool: %v", err)
	}
	close(msgCh)
	var msgs []Msg
	for m := range msgCh {
		msgs = append(msgs, m)
	}
	return block, th, msgs
}

func blockText(b types.ContentBlock) string {
	var parts []string
	for _, c := range b.Content {
		if c.Text != nil {
			parts = append(parts, *c.Text)
		}
	}
	return strings.Join(parts, "\n")
}

func TestToolHooks_PreDenyBlocksCall(t *testing.T) {
	tool := &recTool{stubTool: stubTool{name: "Bash"}}
	d := hookDispatcher(types.HookPreToolUse, "echo 'dangerous command' >&2; exit 2")
	block, _, _ := runOne(t, tool, &hookChecker{behavior: tools.PermissionAllow}, d, nil)
	if len(tool.calls()) != 0 {
		t.Fatal("tool ran despite PreToolUse deny")
	}
	if block.IsError == nil || !*block.IsError || !strings.Contains(blockText(block), "dangerous command") {
		t.Errorf("deny result = %q", blockText(block))
	}
}

func TestToolHooks_PreUpdatedInput(t *testing.T) {
	tool := &recTool{stubTool: stubTool{name: "Bash"}}
	d := hookDispatcher(types.HookPreToolUse, `echo '{"hookSpecificOutput":{"updatedInput":{"command":"echo safe"}}}'`)
	runOne(t, tool, &hookChecker{behavior: tools.PermissionAllow}, d, nil)
	if got := tool.calls(); len(got) != 1 || got[0] != `{"command":"echo safe"}` {
		t.Errorf("tool got input %v", got)
	}
}

func TestToolHooks_PreAllowSkipsPrompt(t *testing.T) {
	allow := `echo '{"hookSpecificOutput":{"permissionDecision":"allow"}}'`
	tool := &recTool{stubTool: stubTool{name: "Bash"}}
	chk := &hookChecker{behavior: tools.PermissionDeny} // would deny without the hook
	runOne(t, tool, chk, hookDispatcher(types.HookPreToolUse, allow), nil)
	if len(tool.calls()) != 1 || len(chk.reqs) != 0 {
		t.Errorf("hook allow: calls=%d permission requests=%d", len(tool.calls()), len(chk.reqs))
	}

	// A deny from the rules (deny rule, plan mode, tool check) still wins.
	tool2 := &recTool{stubTool: stubTool{name: "Bash"}}
	chk2 := &hookChecker{behavior: tools.PermissionAllow, hookAllowed: tools.PermissionResult{Behavior: tools.PermissionDeny, Reason: "deny rule matched"}}
	block, _, _ := runOne(t, tool2, chk2, hookDispatcher(types.HookPreToolUse, allow), nil)
	if len(tool2.calls()) != 0 || !strings.Contains(blockText(block), "deny rule matched") {
		t.Errorf("deny rule vs hook allow: calls=%d result=%q", len(tool2.calls()), blockText(block))
	}

	// An ask rule still prompts: the normal permission flow runs.
	tool3 := &recTool{stubTool: stubTool{name: "Bash"}}
	chk3 := &hookChecker{behavior: tools.PermissionAllow, hookAllowed: tools.PermissionResult{Behavior: tools.PermissionAsk}}
	runOne(t, tool3, chk3, hookDispatcher(types.HookPreToolUse, allow), nil)
	if len(chk3.reqs) != 1 || chk3.reqs[0].ForceAsk {
		t.Errorf("ask rule vs hook allow: requests = %+v, want one normal request", chk3.reqs)
	}
}

func TestToolHooks_PreAskForcesPrompt(t *testing.T) {
	ask := `echo '{"hookSpecificOutput":{"permissionDecision":"ask","permissionDecisionReason":"really?"}}'`
	tool := &recTool{stubTool: stubTool{name: "Bash"}}
	chk := &hookChecker{behavior: tools.PermissionAllow}
	runOne(t, tool, chk, hookDispatcher(types.HookPreToolUse, ask), nil)
	if len(chk.reqs) != 1 || !chk.reqs[0].ForceAsk || chk.reqs[0].HookReason != "really?" {
		t.Fatalf("permission requests = %+v", chk.reqs)
	}
	if len(tool.calls()) != 1 {
		t.Error("approved call did not run")
	}

	// Non-interactive (sub-agent) queries cannot ask: the call is denied.
	tool2 := &recTool{stubTool: stubTool{name: "Bash"}}
	runOne(t, tool2, &hookChecker{behavior: tools.PermissionAllow}, hookDispatcher(types.HookPreToolUse, ask),
		&tools.UseContext{Ctx: context.Background(), NonInteractivePermissions: true})
	if len(tool2.calls()) != 0 {
		t.Error("non-interactive hook ask must deny")
	}
}

func TestToolHooks_PostFeedbackAndNotices(t *testing.T) {
	tool := &recTool{stubTool: stubTool{name: "Bash"}}
	d := hooks.NewDispatcher(hooks.Config{Hooks: map[types.HookType][]types.HookMatcher{
		types.HookPostToolUse: {{Hooks: []types.HookCommand{
			{Command: `echo '{"decision":"block","reason":"tests failed","systemMessage":"post ran"}'`},
		}}},
	}})
	block, _, msgs := runOne(t, tool, &hookChecker{behavior: tools.PermissionAllow}, d, nil)
	if txt := blockText(block); !strings.Contains(txt, "done") || !strings.Contains(txt, "tests failed") {
		t.Errorf("tool result = %q, want output + hook feedback", txt)
	}
	var notice bool
	var shown string
	for _, m := range msgs {
		if m.Type == MsgTypeSystemMessage && m.SystemText == "post ran" {
			notice = true
		}
		if m.Type == MsgTypeToolResult {
			shown = m.ToolResult.Content
		}
	}
	if !notice {
		t.Error("systemMessage not shown to the user")
	}
	if !strings.Contains(shown, "tests failed") {
		t.Errorf("TUI tool result %q lacks hook feedback", shown)
	}
}

func TestToolHooks_PostFailureEvent(t *testing.T) {
	tool := &recTool{stubTool: stubTool{name: "Bash"}, fail: true}
	d := hooks.NewDispatcher(hooks.Config{Hooks: map[types.HookType][]types.HookMatcher{
		types.HookPostToolUse:        {{Hooks: []types.HookCommand{{Command: "echo success-hook >&2; exit 2"}}}},
		types.HookPostToolUseFailure: {{Hooks: []types.HookCommand{{Command: "echo failure-hook >&2; exit 2"}}}},
	}})
	block, _, _ := runOne(t, tool, &hookChecker{behavior: tools.PermissionAllow}, d, nil)
	txt := blockText(block)
	if !strings.Contains(txt, "failure-hook") || strings.Contains(txt, "success-hook") {
		t.Errorf("failed call result = %q, want only the failure hook", txt)
	}
}

func TestToolHooks_ContinueFalseStopsLoop(t *testing.T) {
	var streams atomic.Int32
	client := &mockClient{streamFn: func(_ context.Context, _ *api.MessageRequest) (api.StreamReader, error) {
		if streams.Add(1) == 1 {
			return newStaticReader(buildToolUseEvents("tu_1", "Bash", `{"command":"ls"}`)...), nil
		}
		return newStaticReader(buildEndTurnEvents("next turn")...), nil
	}}
	d := hooks.NewDispatcher(hooks.Config{Hooks: map[types.HookType][]types.HookMatcher{
		types.HookPostToolUse: {{Hooks: []types.HookCommand{{Command: `echo '{"continue":false,"stopReason":"hook says stop"}'`}}}},
	}})
	tool := &recTool{stubTool: stubTool{name: "Bash"}}
	eng := New(Config{Client: client, Registry: newTestRegistry(tool), Model: "claude-test", ToolHooks: d})
	ch, err := eng.Query(context.Background(), QueryParams{
		Messages:       []types.Message{{Role: types.RoleUser, Content: []types.ContentBlock{{Type: types.ContentTypeText, Text: strPtr("go")}}}},
		ToolUseContext: &tools.UseContext{Ctx: context.Background()},
	})
	if err != nil {
		t.Fatal(err)
	}
	msgs := drainMsgs(ch)
	if n := streams.Load(); n != 1 {
		t.Errorf("LLM called %d times, want 1 (loop must stop after the tool batch)", n)
	}
	var stopped bool
	for _, m := range msgs {
		if m.Type == MsgTypeSystemMessage && m.SystemText == "hook says stop" {
			stopped = true
		}
	}
	if !stopped {
		t.Error("stopReason not shown")
	}
	hist := eng.GetMessages()
	if last := hist[len(hist)-1]; last.Role != types.RoleUser || len(last.Content) == 0 || last.Content[0].Type != types.ContentTypeToolResult {
		t.Errorf("history must end with the tool result, got %+v", last)
	}
}

func TestToolHooks_PreContinueFalseRunsCallThenSkipsRest(t *testing.T) {
	tool := &recTool{stubTool: stubTool{name: "Bash"}}
	d := hookDispatcher(types.HookPreToolUse, `echo '{"continue":false,"stopReason":"enough"}'`)
	msgCh := make(chan Msg, 64)
	th := newToolHooks(d)
	calls := []toolCall{
		{id: "a", name: "Bash", input: json.RawMessage(`{"command":"one"}`)},
		{id: "b", name: "Bash", input: json.RawMessage(`{"command":"two"}`)},
	}
	results, err := runTools(context.Background(), partitionToolCalls(calls, newTestRegistry(tool)), newTestRegistry(tool),
		&tools.UseContext{Ctx: context.Background()}, msgCh, &hookChecker{behavior: tools.PermissionAllow}, th)
	if err != nil {
		t.Fatal(err)
	}
	if got := tool.calls(); len(got) != 1 || got[0] != `{"command":"one"}` {
		t.Errorf("calls = %v, want only the first (continue:false does not block it)", got)
	}
	if len(results) != 2 || !strings.Contains(blockText(results[1]), "skipped: enough") {
		t.Errorf("second result = %q, want a skipped tool_result", blockText(results[1]))
	}
	if stop, reason := th.stopped(); !stop || reason != "enough" {
		t.Errorf("stopped = %v %q", stop, reason)
	}
}

func TestToolHooks_InterruptedPreDoesNotRun(t *testing.T) {
	tool := &recTool{stubTool: stubTool{name: "Bash"}}
	d := hookDispatcher(types.HookPreToolUse, "sleep 5")
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(200 * time.Millisecond); cancel() }()
	msgCh := make(chan Msg, 64)
	block, err := executeOneTool(ctx, toolCall{id: "tu", name: "Bash", input: json.RawMessage(`{}`)}, newTestRegistry(tool),
		&tools.UseContext{Ctx: ctx}, msgCh, &hookChecker{behavior: tools.PermissionAllow}, newToolHooks(d))
	if err != nil {
		t.Fatal(err)
	}
	if len(tool.calls()) != 0 || !strings.Contains(blockText(block), "interrupted") {
		t.Errorf("calls=%d result=%q", len(tool.calls()), blockText(block))
	}
}

func TestToolHooks_RewrittenInputRechecked(t *testing.T) {
	rewrite := `echo '{"hookSpecificOutput":{"updatedInput":{"command":"write"}}}'`
	// ExecFilter (e.g. /btw read-only) applies to the rewritten input.
	tool := &recTool{stubTool: stubTool{name: "Bash"}}
	uctx := &tools.UseContext{Ctx: context.Background(), ExecFilter: func(_ tools.Tool, in tools.Input) (bool, string) {
		return !strings.Contains(string(in), "write"), "writes are not allowed here"
	}}
	block, _, _ := runOne(t, tool, &hookChecker{behavior: tools.PermissionAllow}, hookDispatcher(types.HookPreToolUse, rewrite), uctx)
	if len(tool.calls()) != 0 || !strings.Contains(blockText(block), "not allowed here") {
		t.Errorf("ExecFilter: calls=%d result=%q", len(tool.calls()), blockText(block))
	}

	// A call in a concurrent batch whose rewritten input is not
	// concurrency-safe is refused.
	tool2 := &recTool{stubTool: stubTool{name: "Bash", safe: false}}
	msgCh := make(chan Msg, 64)
	res, err := executeOneTool(context.Background(), toolCall{id: "tu", name: "Bash", input: json.RawMessage(`{}`), concurrent: true},
		newTestRegistry(tool2), &tools.UseContext{Ctx: context.Background()}, msgCh, &hookChecker{behavior: tools.PermissionAllow},
		newToolHooks(hookDispatcher(types.HookPreToolUse, rewrite)))
	if err != nil {
		t.Fatal(err)
	}
	if len(tool2.calls()) != 0 || !strings.Contains(blockText(res), "parallel") {
		t.Errorf("concurrency: calls=%d result=%q", len(tool2.calls()), blockText(res))
	}
}

func TestToolHooks_AdditionalContextAndUpdatedOutput(t *testing.T) {
	tool := &recTool{stubTool: stubTool{name: "Bash"}}
	d := hooks.NewDispatcher(hooks.Config{Hooks: map[types.HookType][]types.HookMatcher{
		types.HookPostToolUse: {{Hooks: []types.HookCommand{
			{Command: `echo '{"hookSpecificOutput":{"additionalContext":"lint ok","updatedToolOutput":"replaced output"}}'`},
		}}},
	}})
	block, _, _ := runOne(t, tool, &hookChecker{behavior: tools.PermissionAllow}, d, nil)
	txt := blockText(block)
	if strings.Contains(txt, "done") || !strings.Contains(txt, "replaced output") {
		t.Errorf("updatedToolOutput not applied: %q", txt)
	}
	if !strings.Contains(txt, "<system-reminder>\nlint ok\n</system-reminder>") {
		t.Errorf("additionalContext not delivered as a reminder: %q", txt)
	}
}

// recTool with a HookStopReason result (like CodeMode after a sandboxed
// hook returned "continue": false).
type stopTool struct{ recTool }

func (s *stopTool) Call(in tools.Input, u *tools.UseContext, p tools.OnProgressFn) (*tools.Result, error) {
	r, err := s.recTool.Call(in, u, p)
	if r != nil {
		r.HookStopReason = "inner hook stop"
	}
	return r, err
}

func TestToolHooks_ToolReportedStop(t *testing.T) {
	tool := &stopTool{recTool{stubTool: stubTool{name: "Bash"}}}
	msgCh := make(chan Msg, 64)
	th := newToolHooks(nil)
	if _, err := executeOneTool(context.Background(), toolCall{id: "a", name: "Bash", input: json.RawMessage(`{}`)},
		newTestRegistry(tool), &tools.UseContext{Ctx: context.Background()}, msgCh, nil, th); err != nil {
		t.Fatal(err)
	}
	if stop, reason := th.stopped(); !stop || reason != "inner hook stop" {
		t.Errorf("stopped = %v %q", stop, reason)
	}
}

func TestToolHooks_SingleCallBatchNotConcurrent(t *testing.T) {
	rewrite := `echo '{"hookSpecificOutput":{"updatedInput":{"command":"x"}}}'`
	// After the rewrite the tool reports unsafe; alone in its batch it still runs.
	tool := &recTool{stubTool: stubTool{name: "Bash"}}
	reg := newTestRegistry(&rewriteUnsafeTool{recTool: tool})
	calls := []toolCall{{id: "a", name: "Bash", input: json.RawMessage(`{"command":"ls"}`)}}
	msgCh := make(chan Msg, 64)
	if _, err := runTools(context.Background(), partitionToolCalls(calls, reg), reg, &tools.UseContext{Ctx: context.Background()}, msgCh,
		&hookChecker{behavior: tools.PermissionAllow}, newToolHooks(hookDispatcher(types.HookPreToolUse, rewrite))); err != nil {
		t.Fatal(err)
	}
	if len(tool.calls()) != 1 {
		t.Errorf("single call refused as parallel: calls=%v", tool.calls())
	}
}

// rewriteUnsafeTool is concurrency-safe only for its original input.
type rewriteUnsafeTool struct{ *recTool }

func (r *rewriteUnsafeTool) IsConcurrencySafe(in tools.Input) bool {
	return strings.Contains(string(in), "ls")
}

func TestToolHooks_ConfigWarningsShown(t *testing.T) {
	d := hooks.NewDispatcher(hooks.Config{Hooks: map[types.HookType][]types.HookMatcher{
		types.HookPreToolUse: {{Hooks: []types.HookCommand{{Type: "http", Command: "https://x"}}}},
	}})
	tool := &recTool{stubTool: stubTool{name: "Bash"}}
	_, _, msgs := runOne(t, tool, &hookChecker{behavior: tools.PermissionAllow}, d, nil)
	var warned bool
	for _, m := range msgs {
		if m.Type == MsgTypeSystemMessage && strings.Contains(m.SystemText, "http") {
			warned = true
		}
	}
	if !warned {
		t.Error("unsupported hook type was not reported to the user")
	}
}
