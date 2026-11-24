package codemode_test

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/tunsuy/glaude/internal/codemode"
	"github.com/tunsuy/glaude/internal/hooks"
	"github.com/tunsuy/glaude/internal/permissions"
	"github.com/tunsuy/glaude/internal/tools"
	cmtool "github.com/tunsuy/glaude/internal/tools/codemode"
	"github.com/tunsuy/glaude/pkg/types"
)

// recordingChecker records permission requests and answers with behavior.
type recordingChecker struct {
	behavior tools.PermissionBehavior
	mu       sync.Mutex
	reqs     []permissions.PermissionRequest
}

func (c *recordingChecker) CanUseTool(context.Context, string, tools.Input, *tools.UseContext) (tools.PermissionResult, error) {
	return tools.PermissionResult{Behavior: c.behavior}, nil
}
func (c *recordingChecker) RequestPermission(_ context.Context, req permissions.PermissionRequest, _ *tools.UseContext) (tools.PermissionResult, error) {
	c.mu.Lock()
	c.reqs = append(c.reqs, req)
	c.mu.Unlock()
	return tools.PermissionResult{Behavior: tools.PermissionAllow}, nil
}
func (c *recordingChecker) GetDenialCount() int { return 0 }

func hookedTool(chk permissions.Checker, event types.HookType, command string) tools.Tool {
	reg := tools.NewRegistry()
	reg.Register(&echoTool{})
	d := hooks.NewDispatcher(hooks.Config{Hooks: map[types.HookType][]types.HookMatcher{
		event: {{Matcher: "Echo", Hooks: []types.HookCommand{{Type: "command", Command: command}}}},
	}})
	return cmtool.NewCodeModeTool(reg, chk, codemode.NewMoejsRuntime(), "", cmtool.WithToolHooks(d))
}

func TestCodeModeHooks_PreDeny(t *testing.T) {
	tool := hookedTool(&fakeChecker{behavior: tools.PermissionAllow}, types.HookPreToolUse, "echo 'not in sandbox' >&2; exit 2")
	res := callCode(t, tool, `return await tools.Echo({ msg: "x" });`)
	if !res.IsError || !strings.Contains(res.Content.(string), "not in sandbox") {
		t.Errorf("result = %v", res.Content)
	}
}

func TestCodeModeHooks_PreUpdatedInput(t *testing.T) {
	tool := hookedTool(&fakeChecker{behavior: tools.PermissionAllow}, types.HookPreToolUse,
		`echo '{"hookSpecificOutput":{"updatedInput":{"msg":"rewritten"}}}'`)
	res := callCode(t, tool, `const r = await tools.Echo({ msg: "orig" }); return r.msg;`)
	if res.IsError || !strings.Contains(res.Content.(string), "rewritten") {
		t.Errorf("result = %v", res.Content)
	}
}

// A real checker with no UI would ask (and so deny) an unknown tool; a hook
// allow skips that default prompt.
func TestCodeModeHooks_PreAllowSkipsDefaultPrompt(t *testing.T) {
	allow := `echo '{"hookSpecificOutput":{"permissionDecision":"allow"}}'`
	chk := permissions.NewChecker(permissions.CheckerConfig{PermCtx: types.ToolPermissionContext{Mode: types.PermissionModeDefault}})
	res := callCode(t, hookedTool(chk, types.HookPreToolUse, allow), `const r = await tools.Echo({ msg: "ok" }); return r.msg;`)
	if res.IsError {
		t.Errorf("hook allow must skip the default prompt: %v", res.Content)
	}
	// A deny rule still wins.
	deny := permissions.NewChecker(permissions.CheckerConfig{PermCtx: types.ToolPermissionContext{
		Mode:            types.PermissionModeDefault,
		AlwaysDenyRules: types.ToolPermissionRulesBySource{types.RuleSourceUser: {"Echo"}},
	}})
	if res := callCode(t, hookedTool(deny, types.HookPreToolUse, allow), `return await tools.Echo({ msg: "x" });`); !res.IsError {
		t.Error("deny rule must win over a hook allow")
	}
}

func TestCodeModeHooks_PreAskForcesPrompt(t *testing.T) {
	chk := &recordingChecker{behavior: tools.PermissionAllow}
	tool := hookedTool(chk, types.HookPreToolUse, `echo '{"hookSpecificOutput":{"permissionDecision":"ask","permissionDecisionReason":"check"}}'`)
	res := callCode(t, tool, `await tools.Echo({ msg: "a" }); await tools.Echo({ msg: "b" }); return "done";`)
	if res.IsError {
		t.Fatalf("unexpected error: %v", res.Content)
	}
	if len(chk.reqs) != 2 || !chk.reqs[0].ForceAsk || chk.reqs[0].HookReason != "check" {
		t.Errorf("requests = %+v, want one forced ask per call", chk.reqs)
	}
}

// The tool already ran: PostToolUse feedback is logged, not turned into an
// error the script might retry.
func TestCodeModeHooks_PostFeedbackLogged(t *testing.T) {
	tool := hookedTool(&fakeChecker{behavior: tools.PermissionAllow}, types.HookPostToolUse,
		`cat > /dev/null; echo '{"decision":"block","reason":"output rejected"}'`)
	res := callCode(t, tool, `const r = await tools.Echo({ msg: "x" }); return r.msg;`)
	if res.IsError || !strings.Contains(res.Content.(string), "[hook]") || !strings.Contains(res.Content.(string), "output rejected") {
		t.Errorf("result = %v", res.Content)
	}
}

// continue:false lets the call finish and stops the later calls.
func TestCodeModeHooks_ContinueFalseStopsLaterCalls(t *testing.T) {
	tool := hookedTool(&fakeChecker{behavior: tools.PermissionAllow}, types.HookPreToolUse,
		`echo '{"continue":false,"stopReason":"halt now"}'`)
	res := callCode(t, tool, `
		const a = await tools.Echo({ msg: "first" });
		try { await tools.Echo({ msg: "second" }); return "second ran"; }
		catch (e) { return a.msg + " / " + e.message; }`)
	if res.IsError || !strings.Contains(res.Content.(string), "first / ") || !strings.Contains(res.Content.(string), "halt now") {
		t.Errorf("result = %v", res.Content)
	}
	if res.HookStopReason != "halt now" {
		t.Errorf("HookStopReason = %q, want the outer loop to stop", res.HookStopReason)
	}
}
