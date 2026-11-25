---
package: permissions
import_path: internal/permissions
layer: services
generated_at: 2026-11-25T16:47:52Z
source_files: [ask.go, checker.go, denial.go, filesystem.go, persist.go]
---

# internal/permissions

> Layer: **Services** · Files: 5 · Interfaces: 2 · Structs: 9 · Functions: 3

## Interfaces

### Checker (3 methods)
> Checker is the top-level permission pipeline interface.

```go
type Checker interface {
    CanUseTool(ctx context.Context, toolName string, input tools.Input, tctx *tools.UseContext) (tools.PermissionResult, error)
    RequestPermission(ctx context.Context, req PermissionRequest, tctx *tools.UseContext) (tools.PermissionResult, error)
    GetDenialCount() int
}
```

### HookAwareChecker (2 methods)
> HookAwareChecker is implemented by checkers that support the decisions of

```go
type HookAwareChecker interface {
    CanUseToolHookAllowed(ctx context.Context, toolName string, input tools.Input, tctx *tools.UseContext) (tools.PermissionResult, error)
    MatchRule(rule string, toolName string, input tools.Input) bool
}
```

## Structs

- **AskRequest** — 9 fields: ID, ToolName, ToolUseID, Message, Input, Suggestions, BlockedPath, ProjectPath, ...
- **AskResponse** — 5 fields: ID, Decision, AlwaysAllow, UserModified, AlwaysInCodeMode
- **CheckerConfig** — 6 fields: PermCtx, Registry, AskCh, RespCh, Persist, ModeFn
- **DangerousPathResult** — 3 fields: Dangerous, Matched, Reason
- **DenialLimits** — 2 fields: MaxConsecutive, MaxTotal
- **DenialRecord** — 4 fields: ToolName, ToolUseID, Reason, DeniedAt
- **DenialTrackingState** — 5 fields: DenialCount, LastDeniedAt, RecentDenials
- **PermissionRequest** — 8 fields: ToolName, ToolUseID, Input, ToolResult, FromCodeMode, Note, ForceAsk, HookReason
- **PersistConfig** — 1 fields: ProjectDir

## Functions

- `CheckDangerousPath(path string) DangerousPathResult`
- `IsDangerousPath(path string) bool`
- `NewChecker(cfg CheckerConfig) Checker`

## Change Impact

**Test Mocks (must add new methods when interfaces change):**
- `fakeChecker` in `internal/tools/codemode/tool_test.go`

**Exported type references (files that use types from this package):**
- `AskRequest` → `internal/bootstrap/acp_test.go` (test), `internal/bootstrap/wire.go`, `internal/tui/init.go`, `internal/tui/model.go`, `internal/tui/permission_codemode_test.go` (test)
- `AskResponse` → `internal/bootstrap/acp_test.go` (test), `internal/bootstrap/wire.go`, `internal/tui/init.go`, `internal/tui/model.go`, `internal/tui/permission_codemode_test.go` (test)
- `Checker` → `internal/bootstrap/acp.go`, `internal/bootstrap/acp_test.go` (test), `internal/bootstrap/wire.go`, `internal/engine/engine.go`, `internal/engine/orchestration.go` + 5 more
- `CheckerConfig` → `internal/bootstrap/acp_test.go` (test), `internal/bootstrap/wire.go`, `internal/tools/codemode/hooks_test.go` (test)
- `HookAwareChecker` → `internal/bootstrap/wire.go`, `internal/engine/orchestration.go`, `internal/tools/codemode/bridge.go`
- `PermissionRequest` → `internal/bootstrap/acp.go`, `internal/engine/btw_test.go` (test), `internal/engine/orchestration.go`, `internal/engine/tool_hooks_test.go` (test), `internal/tools/codemode/bridge.go` + 3 more
- `PersistConfig` → `internal/bootstrap/wire.go`

## Dependencies

**Imports:** `internal/config`, `internal/tools`, `pkg/types`

**Imported by:** `internal/bootstrap`, `internal/engine`, `internal/tools/codemode`, `internal/tui`

<!-- AUTO-GENERATED ABOVE — DO NOT EDIT -->
<!-- MANUAL NOTES BELOW — preserved across regeneration -->

## Design Notes

- **2026-11-18 内核搬回（反转 PR #31）**：本包原于 2026-09-23 逐字提取至外部仓 github.com/tunsuy/agtkeel；因该仓停止维护，经人类决策整併回本仓原始路径。来源：agtkeel main 3a77085＋后续提交，零逻辑改动（仅 import 路径恢复＋gofmt）。内核改动自此直接在本仓进行。
- **2026-11-18 BF-09/BF-21 ModeFn**：CheckerConfig.ModeFn 可选 live mode 来源（覆盖 PermCtx.Mode 快照）；currentMode() 于 bypass 与 mode-switch 两处消费；nil 时行为不变。锁序 checker.mu→store.mu 无死锁。Checker 接口零变更。
- **2026-11-18 CM-03**：PermissionRequest/AskRequest +FromCodeMode/Note；AskResponse/PermissionResult +AlwaysInCodeMode。askUser：Note 附加讯息；AlwaysInCodeMode 批准不落项目规则（CodeMode bridge 持久化），与 AlwaysAllow 互斥。Checker 接口零变更。
- **2026-11-23 並行權限詢問序列化（fix/repetitive-replies）**：主腦的並行工具呼叫與所有 sub-agent 共用一個 checker 與一組 askCh/respCh，TUI 一次只顯示一個對話框。舊碼不序列化：每個新詢問**覆蓋**正在顯示的對話框，使用者的回答被最先讀 respCh 的呼叫者拿走（`AskResponse.ID` 註解說必須相符，卻從未比對），其餘呼叫者一直等到查詢被取消，回 `"context cancelled"`。實機 transcript：4 個並行 Agent 呼叫 3 個 context cancelled、1 個放行；守護測試在舊碼上重現同一結果，且放行的那個拿到的是別的對話框的答案。現在 `askSem`（容量 1、可被 ctx 取消）讓詢問逐一進行；輪到時先以 `allowedByRule` 重比 allow 規則（只比規則、不重跑 PreToolUse hook），前一個對話框選了「always allow」的同類呼叫不再詢問；只接受 ID 相符的回應，丟棄已放棄詢問的過期回答。`DenialTrackingState` 加 mutex（並行 RequestPermission 本來就會同時寫它，-race 測出）。守護測試 ask_concurrency_test.go（`fakeTUI` 模擬覆蓋式單一對話框）。
- **2026-11-23 檔案工具的路徑規則生效（fix/repetitive-replies）**：Read/Write/Edit/NotebookEdit 沿用 `BaseTool.PreparePermissionMatcher`（nil），`matchPattern` 對帶內容的規則在 matcher 為 nil 時一律不符——**所有** `Write(...)` 類 allow／deny 規則都從未生效；`ruleForTool` 還把檔案存成 `Write(<file>/**)`，連同一個檔案都比對不到，「always allow」形同無效、每次寫檔都再問一次。現在 `resolveMatcherFn` 對沒有 matcher 的 `PathTool` 用 `pathRuleMatcher`（`dir/**` 前綴、含 `*?[` 走 `filepath.Match`、其他為完整路徑）；`ruleForTool` 依對話框文案存 `Tool(<專案>/**)`，專案外只存該檔案。注意：既有的路徑 deny 規則（如 `Write(/etc/**)`）從此開始生效。
