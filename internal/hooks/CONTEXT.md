---
package: hooks
import_path: internal/hooks
layer: infra
generated_at: 2026-11-25T16:03:34Z
source_files: [hooks.go]
---

# internal/hooks

> Layer: **Infra** · Files: 1 · Interfaces: 0 · Structs: 6 · Functions: 1

## Structs

- **Common** — 4 fields: Notices, PreventContinuation, StopReason, AdditionalContext
- **Config** — 6 fields: Hooks, Disabled, ProjectDir, ModeFn, IfMatcher, DebugLogf
- **Dispatcher** — 7 fields
- **PostToolUseResult** — 4 fields: Feedback, UpdatedToolOutput, UpdatedMCPToolOutput (embeds Common)
- **PreToolUseResult** — 4 fields: Decision, Reason, UpdatedInput (embeds Common)
- **ToolEvent** — 7 fields: SessionID, TranscriptPath, Cwd, PermissionMode, ToolName, ToolInput, ToolUseID

## Functions

- `NewDispatcher(cfg Config) *Dispatcher`

## Constants

- `DecisionAllow`
- `DecisionAsk`
- `DecisionDefer`
- `DecisionDeny`
- `DecisionNone`
- `DefaultTimeout`

## Change Impact

**Exported type references (files that use types from this package):**
- `Common` → `internal/engine/tool_hooks.go`, `internal/tools/codemode/state.go`
- `Config` → `internal/bootstrap/wire.go`, `internal/engine/tool_hooks_test.go` (test), `internal/tools/codemode/hooks_test.go` (test)
- `Dispatcher` → `internal/bootstrap/wire.go`, `internal/engine/engine.go`, `internal/engine/tool_hooks.go`, `internal/engine/tool_hooks_test.go` (test), `internal/tools/codemode/tool.go`
- `PostToolUseResult` → `internal/engine/tool_hooks.go`
- `PreToolUseResult` → `internal/engine/tool_hooks.go`
- `ToolEvent` → `internal/engine/tool_hooks.go`, `internal/tools/codemode/bridge.go`

## Dependencies

**Imports:** `pkg/types`

**Imported by:** `internal/bootstrap`, `internal/engine`, `internal/tools/codemode`

<!-- AUTO-GENERATED ABOVE — DO NOT EDIT -->
<!-- MANUAL NOTES BELOW — preserved across regeneration -->

## Design Notes

- **2026-11-24 工具 hook 完整實作**：設定格式改為與 Claude Code 相同的 matcher 分組（`{"matcher","hooks":[{"type":"command","command","timeout"}]}`），舊扁平格式移除。PreToolUse／PostToolUse／PostToolUseFailure 在 engine `executeOneTool` 與 CodeMode bridge 呼叫，不再經過 permissions checker。設計見 `docs/superpowers/specs/2026-11-24-tool-hooks-design.md`。
- **2026-11-18 内核搬回（反转 PR #31）**：本包原于 2026-09-23 逐字提取至外部仓 github.com/tunsuy/agtkeel；因该仓停止维护，经人类决策整併回本仓原始路径。来源：agtkeel main 3a77085＋后续提交，零逻辑改动（仅 import 路径恢复＋gofmt）。内核改动自此直接在本仓进行。
