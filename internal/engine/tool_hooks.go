package engine

import (
	"context"
	"encoding/json"
	"strings"
	"sync"

	"github.com/tunsuy/glaude/internal/hooks"
	"github.com/tunsuy/glaude/internal/tools"
	"github.com/tunsuy/glaude/pkg/types"
)

// toolHooks runs the settings.json tool hooks for one batch of tool calls and
// records a "continue": false request from any of them. A nil *toolHooks (or
// one with a nil dispatcher) runs nothing.
type toolHooks struct {
	d *hooks.Dispatcher

	mu         sync.Mutex
	stop       bool
	stopReason string
}

func newToolHooks(d *hooks.Dispatcher) *toolHooks {
	return &toolHooks{d: d}
}

func (h *toolHooks) has(event types.HookType, tool string) bool {
	return h != nil && h.d.Has(event, tool)
}

// warn shows the dispatcher's configuration warnings once per session
// (TakeWarnings returns them only the first time).
func (h *toolHooks) warn(ctx context.Context, msgCh chan<- Msg) {
	if h == nil {
		return
	}
	for _, w := range h.d.TakeWarnings() {
		sendSystem(ctx, msgCh, w)
	}
}

func (h *toolHooks) requestStop(reason string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.stop = true
	if reason != "" && h.stopReason == "" {
		h.stopReason = reason
	}
}

// stopped reports whether a hook asked the agent loop to stop.
func (h *toolHooks) stopped() (bool, string) {
	if h == nil {
		return false, ""
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.stop, h.stopReason
}

func hookEvent(tc toolCall, uctx *tools.UseContext) hooks.ToolEvent {
	ev := hooks.ToolEvent{ToolName: tc.name, ToolInput: tc.input, ToolUseID: tc.id}
	if uctx != nil {
		ev.SessionID = uctx.SessionID
		ev.TranscriptPath = uctx.TranscriptPath
		ev.Cwd = uctx.WorkingDir
	}
	return ev
}

// common applies the outcome shared by every event: user notices and the
// stop request.
func (h *toolHooks) common(ctx context.Context, c hooks.Common, msgCh chan<- Msg) {
	for _, n := range c.Notices {
		sendSystem(ctx, msgCh, n)
	}
	if c.PreventContinuation {
		h.requestStop(c.StopReason)
	}
}

func (h *toolHooks) pre(ctx context.Context, tc toolCall, uctx *tools.UseContext, msgCh chan<- Msg) hooks.PreToolUseResult {
	res := h.d.RunPreToolUse(ctx, hookEvent(tc, uctx))
	h.common(ctx, res.Common, msgCh)
	return res
}

// post runs PostToolUse (success) or PostToolUseFailure (failed call) and
// returns the outcome; ok is false when no hook is configured for the event.
func (h *toolHooks) post(ctx context.Context, tc toolCall, uctx *tools.UseContext, msgCh chan<- Msg, failed bool, response json.RawMessage, errText string) (hooks.PostToolUseResult, bool) {
	event := types.HookPostToolUse
	if failed {
		event = types.HookPostToolUseFailure
	}
	if !h.has(event, tc.name) {
		return hooks.PostToolUseResult{}, false
	}
	ev := hookEvent(tc, uctx)
	var res hooks.PostToolUseResult
	if failed {
		res = h.d.RunPostToolUseFailure(ctx, ev, errText, ctx.Err() != nil)
	} else {
		res = h.d.RunPostToolUse(ctx, ev, response)
	}
	h.common(ctx, res.Common, msgCh)
	return res, true
}

// toolResponseJSON renders a tool's structured result for tool_response.
func toolResponseJSON(content any, fallback string) json.RawMessage {
	if content == nil {
		b, _ := json.Marshal(fallback)
		return b
	}
	if raw, ok := content.(json.RawMessage); ok && json.Valid(raw) {
		return raw
	}
	b, err := json.Marshal(content)
	if err != nil {
		b, _ = json.Marshal(fallback)
	}
	return b
}

// mcpOutputText renders an updatedMCPToolOutput value as tool result text.
func mcpOutputText(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	return string(raw)
}

// recheckRewrittenInput re-applies the checks done before the hooks ran to an
// input a PreToolUse hook rewrote. It returns a refusal reason, or "".
func recheckRewrittenInput(t tools.Tool, tc toolCall, uctx *tools.UseContext) string {
	vr, verr := t.ValidateInput(tc.input, uctx)
	if verr != nil {
		return "input rewritten by a PreToolUse hook is invalid: " + verr.Error()
	}
	if !vr.OK {
		return "input rewritten by a PreToolUse hook is invalid: " + firstNonEmptyStr(vr.Reason, "invalid input")
	}
	if uctx != nil && uctx.ExecFilter != nil {
		if ok, reason := uctx.ExecFilter(t, tc.input); !ok {
			return firstNonEmptyStr(reason, tc.name+" is not available here")
		}
	}
	if tc.concurrent && !t.IsConcurrencySafe(tc.input) {
		return "input rewritten by a PreToolUse hook is not safe to run in parallel with other tools"
	}
	return ""
}

// hookReminders wraps hook additionalContext the way Claude Code delivers it.
func hookReminders(ctx []string) []string {
	out := make([]string, 0, len(ctx))
	for _, c := range ctx {
		out = append(out, "<system-reminder>\n"+c+"\n</system-reminder>")
	}
	return out
}

func firstNonEmptyStr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// isMCPTool reports whether name is an MCP tool (mcp__server__tool).
func isMCPTool(name string) bool { return strings.HasPrefix(name, "mcp__") }
