---
package: tools
import_path: internal/tools
layer: tools
generated_at: 2026-11-25T16:47:52Z
source_files: [base.go, registry.go, tool.go]
---

# internal/tools

> Layer: **Tools** · Files: 3 · Interfaces: 7 · Structs: 12 · Functions: 3

## Interfaces

### AgentCoordinator (9 methods)
> AgentCoordinator is the subset of coordinator.Coordinator that tools need.

```go
type AgentCoordinator interface {
    SpawnAgent(ctx context.Context, req AgentSpawnRequest) (string, error)
    SendMessage(ctx context.Context, agentID string, message string) error
    StopAgent(ctx context.Context, agentID string) error
    GetAgentStatus(ctx context.Context, agentID string) (string, error)
    GetAgentResult(ctx context.Context, agentID string) (result string, status string, err error)
    ListAgents() map[string]string
    WaitForAgent(ctx context.Context, agentID string) (string, error)
    ResolveAgent(ctx context.Context, target string) (string, error)
    BroadcastMessage(ctx context.Context, message string) (int, error)
}
```

### MCPToolInfo (1 methods)
> MCPToolInfo is the optional sub-interface for MCP-backed tools.

```go
type MCPToolInfo interface {
    MCPInfo() MCPInfo
}
```

### MemoryEventSink (1 methods)
> MemoryEventSink receives MemoryEvents. Implementations must not block:

```go
type MemoryEventSink interface {
    MemoryEvent(MemoryEvent)
}
```

### PathTool (1 methods)
> PathTool is the optional sub-interface implemented by file-operation tools.

```go
type PathTool interface {
    GetPath(input Input) string
}
```

### PermissionContext (1 methods)
> PermissionContext provides current permission-mode information to tools for

```go
type PermissionContext interface {
    Mode() string
}
```

### SearchOrReadTool (1 methods)
> SearchOrReadTool is the optional sub-interface for search/read tools.

```go
type SearchOrReadTool interface {
    IsSearchOrRead(input Input) SearchOrReadResult
}
```

### Tool (19 methods)
> Tool is the interface every built-in tool must implement.

```go
type Tool interface {
    Name() string
    Aliases() []string
    Description(input Input, permCtx PermissionContext) string
    InputSchema() InputSchema
    Prompt(ctx context.Context, permCtx PermissionContext) (string, error)
    MaxResultSizeChars() int
    SearchHint() string
    IsConcurrencySafe(input Input) bool
    IsReadOnly(input Input) bool
    IsDestructive(input Input) bool
    IsEnabled() bool
    InterruptBehavior() InterruptBehavior
    ValidateInput(input Input, ctx *UseContext) (ValidationResult, error)
    CheckPermissions(input Input, ctx *UseContext) (PermissionResult, error)
    PreparePermissionMatcher(input Input) (func(pattern string) bool, error)
    Call(input Input, ctx *UseContext, onProgress OnProgressFn) (*Result, error)
    MapResultToToolResultBlock(output any, toolUseID string) (json.RawMessage, error)
    ToAutoClassifierInput(input Input) string
    UserFacingName(input Input) string
}
```

## Structs

- **AgentSpawnRequest** — 13 fields: Description, Prompt, SystemPrompt, AllowedTools, DenyTools, MaxTurns, AgentType, Model, ...
- **BaseTool**
- **InputSchema** — 3 fields: Type, Properties, Required
- **MCPInfo** — 2 fields: ServerName, ToolName
- **MemoryEvent** — 5 fields: Op, Name, QuerySource, Err, Warning
- **PermissionResult** — 3 fields: Behavior, Reason, AlwaysInCodeMode
- **Registry** — 4 fields
- **Result** — 4 fields: Content, HookStopReason, IsError, ContextModifier
- **SearchOrReadResult** — 3 fields: IsSearch, IsRead, Path
- **SnapshotSlot** — 2 fields
- **UseContext** — 16 fields: Ctx, AbortCh, PermCtx, Coordinator, AgentID, QuerySource, ExecFilter, NonInteractivePermissions, ...
- **ValidationResult** — 2 fields: OK, Reason

## Function Types

- `OnProgressFn` — `func(data any)`

## Functions

- `NewInputSchema(props map[string]json.RawMessage, required []string) InputSchema`
- `NewRegistry() *Registry`
- `PropSchema(def map[string]any) json.RawMessage`

## Constants

- `InterruptBehaviorBlock`
- `InterruptBehaviorCancel`
- `PermissionAllow`
- `PermissionAsk`
- `PermissionDeny`
- `PermissionPassthrough`

## Change Impact

**Test Mocks (must add new methods when interfaces change):**
- `stubTool` in `internal/engine/orchestration_test.go`
- `stubRegistry` in `internal/permissions/checker_test.go`
- `stubPathTool` in `internal/permissions/persist_test.go`
- `mockTool` in `internal/tools/registry_test.go`
- `stubTool` in `test/integration/engine_e2e_test.go`

**Exported type references (files that use types from this package):**
- `AgentCoordinator` → `internal/bootstrap/wire.go`, `internal/coordinator/adapter.go`, `internal/tools/agent/agent_test.go` (test), `internal/tools/tasks/tasks_test.go` (test), `internal/tui/init.go` + 1 more
- `AgentSpawnRequest` → `internal/bootstrap/acp.go`, `internal/coordinator/adapter.go`, `internal/coordinator/adapter_test.go` (test), `internal/tools/agent/agent.go`, `internal/tools/agent/agent_test.go` (test) + 3 more
- `BaseTool` → `internal/bootstrap/wire_fork_test.go` (test), `internal/codemode/catalog_test.go` (test), `internal/engine/engine_test.go` (test), `internal/mcp/adapter.go`, `internal/tools/agent/agent.go` + 20 more
- `InputSchema` → `internal/bootstrap/wire_fork_test.go` (test), `internal/codemode/catalog_test.go` (test), `internal/engine/orchestration_test.go` (test), `internal/mcp/adapter.go`, `internal/permissions/checker_test.go` (test) + 21 more
- `MCPInfo` → `internal/mcp/adapter.go`
- `MCPToolInfo` → `internal/mcp/adapter.go`, `internal/tools/mcp/mcp_test.go` (test)
- `MemoryEvent` → `internal/tools/memory/events_test.go` (test), `internal/tools/memory/memory.go`, `internal/tui/memory_events.go`, `internal/tui/memory_events_test.go` (test), `internal/tui/model.go`
- `MemoryEventSink` → `internal/tui/memory_events.go`, `internal/tui/model.go`
- `OnProgressFn` → `internal/bootstrap/wire_fork_test.go` (test), `internal/codemode/catalog_test.go` (test), `internal/engine/btw_test.go` (test), `internal/engine/forked_agent_test.go` (test), `internal/engine/orchestration_test.go` (test) + 24 more
- `PathTool` → `internal/permissions/checker.go`, `internal/permissions/persist.go`, `internal/tools/fileops/fileedit.go`, `internal/tools/fileops/fileread.go`, `internal/tools/fileops/filewrite.go` + 1 more
- `PermissionContext` → `internal/bootstrap/wire_fork_test.go` (test), `internal/codemode/catalog_test.go` (test), `internal/engine/compaction.go`, `internal/engine/orchestration_test.go` (test), `internal/mcp/adapter.go` + 22 more
- `PermissionResult` → `internal/engine/btw_test.go` (test), `internal/engine/orchestration.go`, `internal/engine/orchestration_test.go` (test), `internal/engine/tool_hooks_test.go` (test), `internal/mcp/adapter.go` + 16 more
- `Registry` → `internal/bootstrap/mcp.go`, `internal/bootstrap/mcp_connect.go`, `internal/bootstrap/tools.go`, `internal/bootstrap/wire.go`, `internal/codemode/catalog.go` + 8 more
- `Result` → `internal/bootstrap/wire_fork_test.go` (test), `internal/codemode/catalog_test.go` (test), `internal/engine/btw_test.go` (test), `internal/engine/forked_agent_test.go` (test), `internal/engine/orchestration_test.go` (test) + 26 more
- `SnapshotSlot` → `internal/engine/btw_test.go` (test), `internal/tui/btw_test.go` (test), `internal/tui/cmds.go`, `internal/tui/model.go`
- `Tool` → `internal/bootstrap/wire_fork_test.go` (test), `internal/engine/btw_test.go` (test), `internal/engine/engine.go`, `internal/engine/forked_agent.go`, `internal/engine/orchestration_test.go` (test) + 45 more
- `UseContext` → `internal/bootstrap/acp.go`, `internal/bootstrap/acp_integration_test.go` (test), `internal/bootstrap/mcp.go`, `internal/bootstrap/mcp_connect_test.go` (test), `internal/bootstrap/run.go` + 71 more
- `ValidationResult` → `internal/engine/orchestration_test.go` (test), `internal/mcp/adapter.go`, `internal/permissions/checker_test.go` (test), `internal/tools/fileops/fileread.go`, `internal/tools/fileops/glob.go` + 5 more

## Dependencies

**Imports:** *(none — zero-dependency)*

**Imported by:** `internal/bootstrap`, `internal/codemode`, `internal/coordinator`, `internal/engine`, `internal/mcp`, `internal/permissions`, `internal/tools/agent`, `internal/tools/codemode`, `internal/tools/fileops`, `internal/tools/interact`, `internal/tools/mcp`, `internal/tools/memory`, `internal/tools/misc`, `internal/tools/shell`, `internal/tools/tasks`, `internal/tools/web`, `internal/tui`

<!-- AUTO-GENERATED ABOVE — DO NOT EDIT -->
<!-- MANUAL NOTES BELOW — preserved across regeneration -->

## Design Notes

- **2026-11-18 内核搬回（反转 PR #31）**：本包原于 2026-09-23 逐字提取至外部仓 github.com/tunsuy/agtkeel；因该仓停止维护，经人类决策整併回本仓原始路径。来源：agtkeel main 3a77085＋后续提交，零逻辑改动（仅 import 路径恢复＋gofmt）。内核改动自此直接在本仓进行。
- **2026-11-18 CM-03**：PermissionResult +AlwaysInCodeMode bool（CodeMode-scoped 持久批准标记；非项目规则）。 additive 字段，既有构造点零影响。
- **2026-11-18 UseContext.CacheParams（feat/coordinator-fork-cache）**：+CacheParams any（具體型別 *engine.CacheSafeParams；tools 層零依賴故用 any，wire 層 cast）。engine 於 turn 邊界寫入、工具 call 期間讀取（寫入先於 runTools 的 goroutine 啟動，happens-before 成立）。Agent tool 轉發至 AgentSpawnRequest.CacheParams，fork 據此繼承父前綴。
- **2026-11-18 UseContext.WorkingDir（feat/agent-cwd）**：+WorkingDir string——per-call 會話工作目錄。工具相對路徑解析與 Bash cmd.Dir 改用它（空值 fallback process CWD，向後兼容）。此前 Bash 不設 cmd.Dir、fileops/glob/grep 用 os.Getwd()，EnterWorktree 只改 AppState.WorkingDir 不 chdir——主會話進 worktree 後工具仍在舊目錄、委派 agent 也無目錄繼承。現 TUI/headless/sub-agent 三條接線皆從 AppStateStore live 讀 WorkingDir 注入 uctx；buildRunAgentFn 改收 *state.AppStateStore（model 也改 live 讀，/model 切換後 spawn 生效）。per-agent 獨立 worktree isolation 仍為 Phase 2。
- **2026-11-20 /btw**：`UseContext` +`ExecFilter`、`NonInteractivePermissions`、`TodoAppendOnly`、`Snapshot *SnapshotSlot`。`SnapshotSlot` 以指標持有，複製 UseContext 時共用同一份。
