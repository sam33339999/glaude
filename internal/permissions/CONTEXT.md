---
package: permissions
import_path: internal/permissions
layer: services
generated_at: 2026-11-23T08:02:40Z
source_files: [ask.go, checker.go, denial.go, filesystem.go, persist.go]
---

# internal/permissions

> Layer: **Services** · Files: 5 · Interfaces: 1 · Structs: 9 · Functions: 3

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

## Structs

- **AskRequest** — 9 fields: ID, ToolName, ToolUseID, Message, Input, Suggestions, BlockedPath, ProjectPath, ...
- **AskResponse** — 5 fields: ID, Decision, AlwaysAllow, UserModified, AlwaysInCodeMode
- **CheckerConfig** — 7 fields: PermCtx, Dispatcher, Registry, AskCh, RespCh, Persist, ModeFn
- **DangerousPathResult** — 3 fields: Dangerous, Matched, Reason
- **DenialLimits** — 2 fields: MaxConsecutive, MaxTotal
- **DenialRecord** — 4 fields: ToolName, ToolUseID, Reason, DeniedAt
- **DenialTrackingState** — 4 fields: DenialCount, LastDeniedAt, RecentDenials
- **PermissionRequest** — 6 fields: ToolName, ToolUseID, Input, ToolResult, FromCodeMode, Note
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
- `Checker` → `internal/bootstrap/acp.go`, `internal/bootstrap/acp_test.go` (test), `internal/bootstrap/wire.go`, `internal/engine/engine.go`, `internal/engine/orchestration.go` + 3 more
- `CheckerConfig` → `internal/bootstrap/acp_test.go` (test), `internal/bootstrap/wire.go`
- `PermissionRequest` → `internal/bootstrap/acp.go`, `internal/engine/btw_test.go` (test), `internal/engine/orchestration.go`, `internal/tools/codemode/bridge.go`, `internal/tools/codemode/tool_test.go` (test) + 1 more
- `PersistConfig` → `internal/bootstrap/wire.go`

## Dependencies

**Imports:** `internal/config`, `internal/hooks`, `internal/tools`, `pkg/types`

**Imported by:** `internal/bootstrap`, `internal/engine`, `internal/tools/codemode`, `internal/tui`

<!-- AUTO-GENERATED ABOVE — DO NOT EDIT -->
<!-- MANUAL NOTES BELOW — preserved across regeneration -->

## Design Notes

- **2026-11-18 内核搬回（反转 PR #31）**：本包原于 2026-09-23 逐字提取至外部仓 github.com/tunsuy/agtkeel；因该仓停止维护，经人类决策整併回本仓原始路径。来源：agtkeel main 3a77085＋后续提交，零逻辑改动（仅 import 路径恢复＋gofmt）。内核改动自此直接在本仓进行。
- **2026-11-18 BF-09/BF-21 ModeFn**：CheckerConfig.ModeFn 可选 live mode 来源（覆盖 PermCtx.Mode 快照）；currentMode() 于 bypass 与 mode-switch 两处消费；nil 时行为不变。锁序 checker.mu→store.mu 无死锁。Checker 接口零变更。
- **2026-11-18 CM-03**：PermissionRequest/AskRequest +FromCodeMode/Note；AskResponse/PermissionResult +AlwaysInCodeMode。askUser：Note 附加讯息；AlwaysInCodeMode 批准不落项目规则（CodeMode bridge 持久化），与 AlwaysAllow 互斥。Checker 接口零变更。
