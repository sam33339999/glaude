// Package codemode provides the CodeMode tool (CM-01/CM-02/CM-03): one tool
// that executes model-written JavaScript/TypeScript composing other tools
// inside a permission-gated sandbox. See internal/codemode for the engine and
// catalog.
package codemode

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/tunsuy/glaude/internal/codemode"
	"github.com/tunsuy/glaude/internal/hooks"
	"github.com/tunsuy/glaude/internal/permissions"
	"github.com/tunsuy/glaude/internal/tools"

	"github.com/tunsuy/glaude/internal/textutil"
)

// maxResultBytes caps the inline tool result (window-small, cache-first gate
// Q2). Larger outputs spill to a file with a pointer + preview (CM-02 v2).
const maxResultBytes = 32 << 10 // 32 KiB

// spillPreviewBytes is the inline preview kept when a result spills to disk.
const spillPreviewBytes = 4 << 10 // 4 KiB

// maxModuleBytes caps one imported module's or snippet's source (CM-02/CM-03).
const maxModuleBytes = 256 << 10 // 256 KiB

// historyCodeCap bounds the code stored/returned per history entry. Replay
// uses the stored code; long workflows should be saved as snippets (full code).
const historyCodeCap = 2000

// codeModeTool is the CodeMode tool instance.
type codeModeTool struct {
	tools.BaseTool
	reg     *tools.Registry
	checker permissions.Checker
	engine  codemode.Runtime
	catalog []codemode.CatalogEntry
	dataDir string // history/spill/modules/snippets/approvals root ("" disables persistence)
	callSeq atomic.Int64
	// approvalMu serializes approvals.json read-modify-write cycles.
	approvalMu sync.Mutex
	// hooks runs the settings.json tool hooks around every inner call (nil = none).
	hooks *hooks.Dispatcher
}

// Option configures the CodeMode tool.
type Option func(*codeModeTool)

// WithToolHooks makes every inner tool call run the settings.json
// PreToolUse/PostToolUse hooks, like a direct tool call does.
func WithToolHooks(d *hooks.Dispatcher) Option {
	return func(t *codeModeTool) { t.hooks = d }
}

// NewCodeModeTool constructs the CodeMode tool. reg supplies the callable
// tool surface and the discovery catalog; checker enforces the full
// permission pipeline on EVERY inner call (sandboxing is not authorization);
// engine is the sandbox runtime (moejs primary); dataDir roots execution
// history, result spill files, controlled imports, snippets and persisted
// CodeMode approvals ("" disables those features). The catalog is snapshotted
// at construction — the tool surface is session-pinned (cache-stable).
func NewCodeModeTool(reg *tools.Registry, checker permissions.Checker, engine codemode.Runtime, dataDir string, opts ...Option) tools.Tool {
	t := &codeModeTool{
		reg:     reg,
		checker: checker,
		engine:  engine,
		catalog: codemode.BuildCatalog(reg),
		dataDir: dataDir,
	}
	for _, o := range opts {
		o(t)
	}
	return t
}

func (t *codeModeTool) Name() string { return "CodeMode" }

func (t *codeModeTool) Description(_ tools.Input, _ tools.PermissionContext) string {
	return `Execute JavaScript (or TypeScript with typescript=true) code that composes
tool calls in one sandboxed run. Write the BODY of an async function: use await,
loops, branches, and return the final value. Only the returned value and
console.log output come back to you — intermediate data stays in the sandbox
(fewer round trips, less context).

Available inside the sandbox:
- tools.<Name>(input) — call any registered tool (async; each call goes through
  the normal permission pipeline). Promise.all runs calls in parallel.
  Batch approval: when a tool prompts and you approve it, later calls to the
  SAME tool within this execution are pre-approved (execution scope only).
- codemode.search(query) — rank tool names by keyword (progressive discovery).
- codemode.describe(name) — full description + input schema of one tool.
- codemode.history(limit) — recent CodeMode executions (id, code, outcome).
- codemode.replay(id) — re-run a recorded execution (permissions still apply).
- codemode.run(name) / codemode.snippets() — run/list named reusable snippets
  (save one with the save_as input field after a successful run).
- codemode.audit(id) — write side effects recorded for one execution.
- codemode.approvals() / codemode.revoke(tool) — list/revoke persistent
  CodeMode-scoped approvals (granted by the user in the permission dialog).
- import { x } from "name" — controlled shared modules from the host module
  directory (if configured by the host; otherwise imports are disabled).
- console.log(...) — captured output.
No filesystem, network, env, or eval access. Execution is bounded by a timeout
and a memory limit. Results over 32 KiB spill to a file you can Read.

Tool catalog (call codemode.describe(name) for input schemas):
` + codemode.CatalogText(t.catalog)
}

func (t *codeModeTool) InputSchema() tools.InputSchema {
	return tools.NewInputSchema(
		map[string]json.RawMessage{
			"code": tools.PropSchema(map[string]any{
				"type":        "string",
				"description": "JavaScript/TypeScript async function body composing tools.* calls; return the final result. Required unless replay is set.",
			}),
			"typescript": tools.PropSchema(map[string]any{
				"type":        "boolean",
				"description": "Compile the code as TypeScript (type stripping); default false",
			}),
			"replay": tools.PropSchema(map[string]any{
				"type":        "string",
				"description": "Re-run a recorded execution by history id (mutually exclusive with code); permissions still apply",
			}),
			"save_as": tools.PropSchema(map[string]any{
				"type":        "string",
				"description": "After a successful run, save the code as a named snippet for codemode.run(name) (flat name)",
			}),
			"batch_writes": tools.PropSchema(map[string]any{
				"type":        "boolean",
				"description": "Gate ALL write calls behind ONE approval (first write prompts with the execution-wide scope); reads keep the normal flow",
			}),
		},
		nil, // code OR replay — validated at call time
	)
}

func (t *codeModeTool) IsConcurrencySafe(_ tools.Input) bool { return false }
func (t *codeModeTool) IsReadOnly(_ tools.Input) bool        { return false }
func (t *codeModeTool) UserFacingName(_ tools.Input) string  { return "CodeMode" }

// codeModeInput is the tool input.
type codeModeInput struct {
	Code        string `json:"code"`
	Typescript  bool   `json:"typescript,omitempty"`
	Replay      string `json:"replay,omitempty"`
	SaveAs      string `json:"save_as,omitempty"`
	BatchWrites bool   `json:"batch_writes,omitempty"`
}

// historyEntry is one recorded execution (CM-02 v2; CM-03 v3 provenance and
// write audit fields).
type historyEntry struct {
	ID          string       `json:"id"`
	Timestamp   string       `json:"ts"`
	OK          bool         `json:"ok"`
	DurationMS  int64        `json:"duration_ms"`
	Typescript  bool         `json:"typescript,omitempty"`
	Code        string       `json:"code"`
	Tools       []string     `json:"tools,omitempty"`
	Preview     string       `json:"preview,omitempty"`
	Error       string       `json:"error,omitempty"`
	ReplayOf    string       `json:"replay_of,omitempty"`
	Snippet     string       `json:"snippet,omitempty"`
	SideEffects []sideEffect `json:"side_effects,omitempty"`
}

func (t *codeModeTool) Call(input tools.Input, uctx *tools.UseContext, _ tools.OnProgressFn) (*tools.Result, error) {
	var in codeModeInput
	if err := json.Unmarshal(input, &in); err != nil {
		return &tools.Result{IsError: true, Content: fmt.Sprintf("CodeMode: invalid input: %v", err)}, nil
	}
	replayOf := ""
	if in.Replay != "" {
		if strings.TrimSpace(in.Code) != "" {
			return &tools.Result{IsError: true, Content: "CodeMode: code and replay are mutually exclusive"}, nil
		}
		e := t.findHistory(in.Replay)
		if e == nil {
			return &tools.Result{IsError: true, Content: fmt.Sprintf("CodeMode: no execution %q in history (replay requires a data directory)", in.Replay)}, nil
		}
		in.Code, in.Typescript, replayOf = e.Code, e.Typescript, e.ID
	}
	if strings.TrimSpace(in.Code) == "" {
		return &tools.Result{IsError: true, Content: "CodeMode: code is required (or replay with a history id)"}, nil
	}
	if t.engine == nil {
		return &tools.Result{IsError: true, Content: "CodeMode: no sandbox engine configured"}, nil
	}

	ctx := context.Background()
	var abortCh <-chan struct{}
	if uctx != nil {
		if uctx.Ctx != nil {
			ctx = uctx.Ctx
		}
		abortCh = uctx.AbortCh
	}
	if abortCh != nil {
		var cancel context.CancelFunc
		ctx, cancel = context.WithCancel(ctx)
		defer cancel()
		go func() {
			select {
			case <-abortCh:
				cancel()
			case <-ctx.Done():
			}
		}()
	}

	execID := fmt.Sprintf("%d-%d", time.Now().UnixMilli(), t.callSeq.Add(1))
	st := newExecState(in.BatchWrites)

	opts := t.sandboxOptions(ctx, uctx, st)
	opts.Typescript = in.Typescript
	start := time.Now()
	result, execErr := t.engine.Execute(ctx, in.Code, t.bridge(ctx, uctx, st), opts)
	duration := time.Since(start).Milliseconds()

	var content string
	if execErr != nil {
		content = execErr.Error()
		// Hook messages explain why a call failed; keep them on errors too.
		if hookLines := st.hookLines(); len(hookLines) > 0 {
			content += "\n" + strings.Join(hookLines, "\n")
		}
	} else {
		if result != nil && len(st.extraLogs) > 0 {
			result.Logs = append(result.Logs, st.extraLogs...)
		}
		content = t.formatResult(execID, result)
		if in.SaveAs != "" {
			if serr := t.saveSnippet(in.SaveAs, in.Code); serr != nil {
				content += fmt.Sprintf("\n(snippet save failed: %v)", serr)
			} else {
				content += fmt.Sprintf("\n(saved as snippet %q — reuse with codemode.run)", in.SaveAs)
			}
		}
	}
	t.recordHistory(historyEntry{
		ID:          execID,
		Timestamp:   start.UTC().Format(time.RFC3339),
		OK:          execErr == nil,
		DurationMS:  duration,
		Typescript:  in.Typescript,
		Code:        capString(in.Code, historyCodeCap),
		Tools:       st.snapshotTools(),
		Preview:     capString(content, 200),
		Error:       errorString(execErr),
		ReplayOf:    replayOf,
		SideEffects: st.snapshotSideEffects(),
	})

	// A hook's "continue": false inside the sandbox stops the outer loop too.
	stop := st.hookStopReason()
	if execErr != nil {
		return &tools.Result{IsError: true, Content: content, HookStopReason: stop}, nil
	}
	return &tools.Result{Content: content, HookStopReason: stop}, nil
}

// formatResult renders logs + return value; results over maxResultBytes spill
// to dataDir/spill/<execID>.txt with a pointer + preview (CM-02 v2).
func (t *codeModeTool) formatResult(execID string, r *codemode.Result) string {
	var sb strings.Builder
	if len(r.Logs) > 0 {
		sb.WriteString(strings.Join(r.Logs, "\n"))
		sb.WriteString("\n")
	}
	if r.Value != nil {
		if s, ok := r.Value.(string); ok {
			if sb.Len() > 0 {
				sb.WriteString("Return value:\n")
			}
			sb.WriteString(s)
		} else {
			raw, err := json.MarshalIndent(r.Value, "", "  ")
			if err != nil {
				raw = []byte(fmt.Sprintf("%v", r.Value))
			}
			if sb.Len() > 0 {
				sb.WriteString("Return value:\n")
			}
			sb.Write(raw)
		}
	}
	out := sb.String()
	if out == "" {
		return "(code completed with no return value or output)"
	}
	if len(out) <= maxResultBytes {
		return out
	}
	if t.dataDir != "" {
		spillDir := filepath.Join(t.dataDir, "spill")
		if err := os.MkdirAll(spillDir, 0o755); err == nil {
			path := filepath.Join(spillDir, execID+".txt")
			if werr := os.WriteFile(path, []byte(out), 0o644); werr == nil {
				return textutil.TruncateBytes(out, spillPreviewBytes) + fmt.Sprintf(
					"\n…(result is %d bytes; full output saved to %s — use tools.Read or tools.Grep on that file)", len(out), path)
			}
		}
	}
	return textutil.TruncateBytes(out, maxResultBytes) + "\n…(truncated: result exceeds 32 KiB; return less data or write it to a file via tools)"
}

// resolveModule serves controlled imports from dataDir/modules (CM-02 v2).
// Specifiers are flat names (no paths); the host controls the directory.
func (t *codeModeTool) resolveModule(specifier string) (string, error) {
	name := strings.TrimSuffix(specifier, ".js")
	if name == "" || strings.ContainsAny(name, "/\\") || strings.Contains(name, "..") {
		return "", fmt.Errorf("invalid module specifier %q (flat names only)", specifier)
	}
	path := filepath.Join(t.dataDir, "modules", name+".js")
	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("module %q not available (host module directory)", name)
	}
	if info.Size() > maxModuleBytes {
		return "", fmt.Errorf("module %q exceeds %d bytes", name, maxModuleBytes)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read module %q: %w", name, err)
	}
	return string(raw), nil
}

// recordHistory appends one execution entry (best-effort, never fatal).
func (t *codeModeTool) recordHistory(e historyEntry) {
	if t.dataDir == "" {
		return
	}
	if err := os.MkdirAll(t.dataDir, 0o755); err != nil {
		return
	}
	raw, err := json.Marshal(e)
	if err != nil {
		return
	}
	f, err := os.OpenFile(filepath.Join(t.dataDir, "history.jsonl"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.Write(append(raw, '\n'))
}

// readHistory returns the most recent entries (newest first) for
// codemode.history(limit) inside the sandbox.
func (t *codeModeTool) readHistory(limit int) []historyEntry {
	if t.dataDir == "" {
		return nil
	}
	if limit <= 0 {
		limit = 10
	}
	if limit > 50 {
		limit = 50
	}
	raw, err := os.ReadFile(filepath.Join(t.dataDir, "history.jsonl"))
	if err != nil {
		return nil
	}
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	var entries []historyEntry
	for i := len(lines) - 1; i >= 0 && len(entries) < limit; i-- {
		var e historyEntry
		if json.Unmarshal([]byte(lines[i]), &e) == nil {
			entries = append(entries, e)
		}
	}
	return entries
}

func capString(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return textutil.TruncateBytes(s, n) + "…"
}

func errorString(err error) string {
	if err == nil {
		return ""
	}
	return capString(err.Error(), 200)
}
