package codemode

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"strings"

	"github.com/tunsuy/glaude/internal/codemode"
	"github.com/tunsuy/glaude/internal/hooks"
	"github.com/tunsuy/glaude/internal/permissions"
	"github.com/tunsuy/glaude/internal/tools"
	"github.com/tunsuy/glaude/pkg/types"
)

// bridge builds the CallFunc dispatching one inner tool call through the SAME
// flow the engine uses (orchestration.go). Permission layers (CM-02/CM-03):
//  1. tool CheckPermissions + checker CanUseTool — Deny always wins, live
//     rule-Allows are re-checked every call (Shift+Tab mode changes apply).
//  2. execution-scope batch approval (execAllow): an Ask the user approved
//     pre-approves the SAME tool for the rest of this execution.
//  3. persisted CodeMode-scoped approvals (CM-03): consulted only on Ask.
//  4. batch-writes gate (CM-03): one approval covers ALL writes of the
//     execution when input batch_writes=true.
func (t *codeModeTool) bridge(ctx context.Context, outer *tools.UseContext, st *execState) codemode.CallFunc {
	return func(_ context.Context, name string, input json.RawMessage) (json.RawMessage, error) {
		tool, ok := t.reg.Get(name)
		if !ok {
			return nil, fmt.Errorf("unknown tool %q (use codemode.search to discover tools)", name)
		}
		if reason := st.hookStopReason(); reason != "" {
			return nil, fmt.Errorf("%s: skipped: %s", name, reason)
		}
		st.recordTool(name)
		inner := &tools.UseContext{Ctx: ctx}
		if outer != nil {
			inner.AbortCh = outer.AbortCh
			inner.PermCtx = outer.PermCtx
			inner.Coordinator = outer.Coordinator
			inner.AgentID = outer.AgentID
			inner.MemoryStore = outer.MemoryStore
			inner.SessionID = outer.SessionID
			inner.TranscriptPath = outer.TranscriptPath
		}

		vr, err := tool.ValidateInput(tools.Input(input), inner)
		if err != nil {
			return nil, fmt.Errorf("%s: input validation: %w", name, err)
		}
		if !vr.OK {
			return nil, fmt.Errorf("%s: invalid input: %s", name, vr.Reason)
		}

		// PreToolUse hooks: same decisions as a direct call (deny / allow /
		// ask / updatedInput). "continue": false lets this call finish and
		// skips the rest of the execution. Notices and additionalContext go
		// to the execution log, which is returned to the model.
		ev := hooks.ToolEvent{
			SessionID:      inner.SessionID,
			TranscriptPath: inner.TranscriptPath,
			Cwd:            outerWorkingDir(outer),
			ToolName:       name,
			ToolUseID:      fmt.Sprintf("codemode-%d", t.callSeq.Add(1)),
		}
		hookAllowed, hookAsk, hookReason := false, false, ""
		if t.hooks.Has(types.HookPreToolUse, name) {
			ev.ToolInput = input
			pre := t.hooks.RunPreToolUse(ctx, ev)
			st.hookCommon(pre.Common)
			if ctx.Err() != nil {
				return nil, fmt.Errorf("%s: interrupted while PreToolUse hooks were running", name)
			}
			if pre.Decision == hooks.DecisionDeny {
				return nil, fmt.Errorf("%s: denied: %s", name, pre.Reason)
			}
			if len(pre.UpdatedInput) > 0 {
				input = pre.UpdatedInput
				vr, err := tool.ValidateInput(tools.Input(input), inner)
				if err != nil {
					return nil, fmt.Errorf("%s: input rewritten by a PreToolUse hook: %w", name, err)
				}
				if !vr.OK {
					return nil, fmt.Errorf("%s: input rewritten by a PreToolUse hook is invalid: %s", name, vr.Reason)
				}
			}
			switch pre.Decision {
			case hooks.DecisionAllow:
				hookAllowed = true
			case hooks.DecisionAsk:
				hookAsk, hookReason = true, pre.Reason
			}
		}

		prelim, err := tool.CheckPermissions(tools.Input(input), inner)
		if err != nil {
			return nil, fmt.Errorf("%s: permission check: %w", name, err)
		}
		if prelim.Behavior == tools.PermissionDeny {
			return nil, fmt.Errorf("%s: denied: %s", name, prelim.Reason)
		}

		isWrite := !tool.IsReadOnly(tools.Input(input))
		switch {
		case t.checker == nil:
		case hookAllowed:
			// The hook skips the default prompt; rules and checks still apply.
			if err := t.hookAllowed(ctx, name, input, inner); err != nil {
				if !errors.Is(err, errHookAsk) {
					return nil, err
				}
				if err := t.authorize(ctx, name, input, inner, prelim, st, isWrite); err != nil {
					return nil, err
				}
			}
		case hookAsk:
			if err := t.requestHookAsk(ctx, name, input, inner, prelim, hookReason); err != nil {
				return nil, err
			}
		default:
			if err := t.authorize(ctx, name, input, inner, prelim, st, isWrite); err != nil {
				return nil, err
			}
		}

		res, err := tool.Call(tools.Input(input), inner, nil)
		ev.ToolInput = input
		if err != nil || res == nil || res.IsError {
			msg := ""
			switch {
			case err != nil:
				msg = err.Error()
			case res == nil:
				msg = "no result"
			default:
				msg = fmt.Sprint(res.Content)
			}
			if t.hooks.Has(types.HookPostToolUseFailure, name) {
				post := t.hooks.RunPostToolUseFailure(ctx, ev, msg, ctx.Err() != nil)
				st.hookCommon(post.Common)
				if len(post.Feedback) > 0 {
					msg += "\n" + strings.Join(post.Feedback, "\n")
				}
			}
			return nil, fmt.Errorf("%s: %s", name, msg)
		}
		if isWrite {
			st.recordSideEffect(name, input) // audit: only executed writes
		}
		if t.hooks.Has(types.HookPostToolUse, name) {
			resp, merr := json.Marshal(res.Content)
			if merr != nil {
				resp = nil
			}
			post := t.hooks.RunPostToolUse(ctx, ev, resp)
			st.hookCommon(post.Common)
			// The tool already ran: feedback is logged, not turned into an error.
			st.hookLog(post.Feedback)
			replacement := post.UpdatedToolOutput
			if len(post.UpdatedMCPToolOutput) > 0 && strings.HasPrefix(name, "mcp__") {
				replacement = post.UpdatedMCPToolOutput
			}
			if len(replacement) > 0 {
				return replacement, nil
			}
		}
		switch c := res.Content.(type) {
		case string:
			return json.Marshal(c)
		case nil:
			return json.Marshal(nil)
		default:
			return json.Marshal(c)
		}
	}
}

// authorize runs the checker layers for one inner call (CM-03 v3).
func (t *codeModeTool) authorize(
	ctx context.Context, name string, input json.RawMessage,
	inner *tools.UseContext, prelim tools.PermissionResult, st *execState, isWrite bool,
) error {
	st.mu.Lock()
	preApproved := st.execAllow[name]
	st.mu.Unlock()
	if preApproved {
		return nil
	}

	// CanUseTool first: Deny wins over everything (including persisted
	// approvals); direct rule-Allows are never cached so live permission-mode
	// changes keep applying.
	can, err := t.checker.CanUseTool(ctx, name, tools.Input(input), inner)
	if err != nil {
		return fmt.Errorf("%s: permission check: %w", name, err)
	}
	if can.Behavior == tools.PermissionDeny {
		return fmt.Errorf("%s: denied: %s", name, can.Reason)
	}
	if can.Behavior == tools.PermissionAllow {
		return nil
	}

	// Ask path.
	if st.batchWrites && isWrite {
		return t.writeGate(ctx, name, input, inner, prelim, st)
	}
	if t.hasApproval(name) {
		return nil // persisted CodeMode-scoped approval (user-granted, revocable)
	}
	return t.requestUser(ctx, name, input, inner, prelim, st, "")
}

// errHookAsk reports that a call a hook allowed still needs the normal flow
// (an ask rule or a tool check applies).
var errHookAsk = errors.New("hook allow needs confirmation")

// hookAllowed checks a call a PreToolUse hook allowed: nil = run it,
// errHookAsk = go through the normal authorization, other = denied.
func (t *codeModeTool) hookAllowed(ctx context.Context, name string, input json.RawMessage, inner *tools.UseContext) error {
	hac, ok := t.checker.(permissions.HookAwareChecker)
	if !ok {
		return errHookAsk
	}
	res, err := hac.CanUseToolHookAllowed(ctx, name, tools.Input(input), inner)
	switch {
	case err != nil:
		return fmt.Errorf("%s: permission check: %w", name, err)
	case res.Behavior == tools.PermissionDeny:
		return fmt.Errorf("%s: denied: %s", name, firstNonEmpty(res.Reason, "permission denied"))
	case res.Behavior == tools.PermissionAllow:
		return nil
	}
	return errHookAsk
}

// requestHookAsk asks the user because a PreToolUse hook answered "ask". The
// approval is not cached for the execution: the hook decides per call.
func (t *codeModeTool) requestHookAsk(
	ctx context.Context, name string, input json.RawMessage,
	inner *tools.UseContext, prelim tools.PermissionResult, reason string,
) error {
	perm, err := t.checker.RequestPermission(ctx, permissions.PermissionRequest{
		ToolName:     name,
		ToolUseID:    fmt.Sprintf("codemode-%d", t.callSeq.Add(1)),
		Input:        tools.Input(input),
		ToolResult:   prelim,
		FromCodeMode: true,
		ForceAsk:     true,
		HookReason:   reason,
	}, inner)
	if err != nil {
		return fmt.Errorf("%s: permission check: %w", name, err)
	}
	if perm.Behavior == tools.PermissionDeny {
		return fmt.Errorf("%s: denied: %s", name, firstNonEmpty(perm.Reason, "permission denied by user or policy"))
	}
	return nil
}

func outerWorkingDir(outer *tools.UseContext) string {
	if outer == nil {
		return ""
	}
	return outer.WorkingDir
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// requestUser runs the interactive Ask flow and caches an approval for this
// execution; a CodeMode-scoped "always allow" choice is also persisted.
func (t *codeModeTool) requestUser(
	ctx context.Context, name string, input json.RawMessage,
	inner *tools.UseContext, prelim tools.PermissionResult, st *execState, note string,
) error {
	req := permissions.PermissionRequest{
		ToolName:     name,
		ToolUseID:    fmt.Sprintf("codemode-%d", t.callSeq.Add(1)),
		Input:        tools.Input(input),
		ToolResult:   prelim,
		FromCodeMode: true,
		Note:         note,
	}
	perm, err := t.checker.RequestPermission(ctx, req, inner)
	if err != nil {
		return fmt.Errorf("%s: permission check: %w", name, err)
	}
	if perm.Behavior == tools.PermissionDeny {
		reason := perm.Reason
		if reason == "" {
			reason = "permission denied by user or policy"
		}
		return fmt.Errorf("%s: denied: %s", name, reason)
	}
	st.mu.Lock()
	st.execAllow[name] = true
	st.mu.Unlock()
	if perm.AlwaysInCodeMode {
		t.saveApproval(name)
	}
	return nil
}

// writeGate implements the CM-03 batch-writes gate: the FIRST write call of
// the execution asks once (with a scope note); the decision covers ALL later
// write calls. Concurrent writes wait for the decision. Reads never enter the
// gate. Full write-plan preview (two-phase execution) is deferred — the dialog
// shows the first concrete write, and the note states the execution-wide scope.
func (t *codeModeTool) writeGate(
	ctx context.Context, name string, input json.RawMessage,
	inner *tools.UseContext, prelim tools.PermissionResult, st *execState,
) error {
	st.mu.Lock()
	if st.gateCh == nil {
		st.gateCh = make(chan struct{})
		st.mu.Unlock()
		note := "CodeMode batch-writes gate: approving allows ALL write tool calls " +
			"in this CodeMode execution (reads keep their normal permission flow). " +
			"First write shown below."
		err := t.requestUser(ctx, name, input, inner, prelim, st, note)
		st.mu.Lock()
		st.gateApproved = err == nil
		close(st.gateCh)
		st.mu.Unlock()
		return err
	}
	ch := st.gateCh
	st.mu.Unlock()

	select {
	case <-ch:
	case <-ctx.Done():
		return fmt.Errorf("%s: write gate: %w", name, ctx.Err())
	}
	st.mu.Lock()
	approved := st.gateApproved
	st.mu.Unlock()
	if !approved {
		return fmt.Errorf("%s: denied: CodeMode write gate was rejected for this execution", name)
	}
	return nil
}
