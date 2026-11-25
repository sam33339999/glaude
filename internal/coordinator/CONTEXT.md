---
package: coordinator
import_path: internal/coordinator
layer: core
generated_at: 2026-11-25T16:45:14Z
source_files: [adapter.go, coordinator.go, prompt.go]
---

# internal/coordinator

> Layer: **Core** · Files: 3 · Interfaces: 1 · Structs: 7 · Functions: 6

## Interfaces

### Coordinator (8 methods)
> Coordinator manages the lifecycle and message routing of multiple sub-agents.

```go
type Coordinator interface {
    SpawnAgent(ctx context.Context, req SpawnRequest) (AgentID, error)
    SendMessage(ctx context.Context, to AgentID, message string) error
    StopAgent(ctx context.Context, agentID AgentID) error
    GetAgentStatus(ctx context.Context, agentID AgentID) (AgentStatus, error)
    Subscribe(agentID AgentID) (<-chan TaskNotification, error)
    IsCoordinatorMode() bool
    ResolveAgent(target string) (AgentID, error)
    BroadcastMessage(ctx context.Context, message string) (int, error)
}
```

## Structs

- **AgentUsage** — 3 fields: TotalTokens, ToolUses, DurationMs
- **Config** — 5 fields: CoordinatorMode, RunAgent, OnProgress, OnStatusChange, SummaryGenerator
- **Event** — 10 fields: Kind, AgentID, Description, AgentName, AgentType, Activity, Detail, Status, ...
- **MCPClientInfo** — 1 fields: Name
- **ProgressEvent** — 6 fields: AgentID, Description, AgentName, AgentType, Activity, Detail
- **SpawnRequest** — 14 fields: Description, SubagentType, Prompt, Model, AllowedTools, DenyTools, MaxTurns, ParentAgentID, ...
- **TaskNotification** — 5 fields: TaskID, Status, Summary, Result, Usage

## Function Types

- `OnProgressFn` — `func(evt ProgressEvent)`
- `OnStatusChangeFn` — `func(agentID AgentID, description string, name string, agentType string, status AgentStatus, result string, summary string)`
- `RunAgentFn` — `func(ctx context.Context, agentID AgentID, req SpawnRequest, inboxCh <-chan string) (string, error)`
- `SummaryGeneratorFn` — `func(agentID AgentID, result string) string`

## Functions

- `DefaultSummaryGenerator(_ AgentID, result string) string`
- `FormatTaskNotification(n TaskNotification) string`
- `GetCoordinatorSystemPrompt(isSimpleMode bool) string`
- `GetCoordinatorUserContext(mcpClients []MCPClientInfo, scratchpadDir string) map[string]string`
- `New(cfg Config) Coordinator`
- `NewAgentCoordinator(c Coordinator) tools.AgentCoordinator`

## Constants

- `AgentStatusCompleted`
- `AgentStatusFailed`
- `AgentStatusRunning`
- `AgentStatusStopped`
- `DefaultMaxTurns`
- `EventProgress`
- `EventStatusChange`

## Change Impact

**Adapters (update when interfaces change):**
- `adapter.go`

**Test Mocks (must add new methods when interfaces change):**
- `mockCoordinator` in `internal/tools/agent/agent_test.go`
- `mockCoordinator` in `internal/tools/tasks/tasks_test.go`

**Exported type references (files that use types from this package):**
- `Config` → `internal/bootstrap/acp_test.go` (test), `internal/bootstrap/wire.go`
- `Coordinator` → `internal/bootstrap/wire.go`
- `Event` → `internal/bootstrap/wire.go`, `internal/tui/init.go`, `internal/tui/model.go`
- `OnProgressFn` → `internal/bootstrap/acp.go`, `internal/bootstrap/wire.go`
- `OnStatusChangeFn` → `internal/bootstrap/wire.go`
- `ProgressEvent` → `internal/bootstrap/acp.go`, `internal/bootstrap/acp_test.go` (test), `internal/bootstrap/wire.go`
- `RunAgentFn` → `internal/bootstrap/acp.go`, `internal/bootstrap/wire.go`
- `SpawnRequest` → `internal/bootstrap/acp.go`, `internal/bootstrap/acp_test.go` (test), `internal/bootstrap/soul_subagent_test.go` (test), `internal/bootstrap/tiers_test.go` (test), `internal/bootstrap/wire.go` + 2 more
- `TaskNotification` → `internal/tui/update.go`

## Dependencies

**Imports:** `internal/textutil`, `internal/tools`, `pkg/utils/ids`

**Imported by:** `internal/bootstrap`, `internal/tui`

<!-- AUTO-GENERATED ABOVE — DO NOT EDIT -->
<!-- MANUAL NOTES BELOW — preserved across regeneration -->

## Design Notes

- Coordinator vs AgentCoordinator: 两个接口是为了打破 tools→coordinator 的循环依赖。tools 包定义 AgentCoordinator（string-based），coordinator 包实现 Coordinator（typed AgentID），adapter.go 做桥接。改任一个接口都要同步改另一个。(2026-04-28)
- UseContext.Coordinator 在子 agent 里故意设为 nil，防止无限递归 spawn。见 bootstrap/wire.go buildRunAgentFn()。(2026-04-28)
- nameIndex 用于 SendMessage 名称路由，SpawnAgent 时注册，ResolveAgent 时查找。(2026-04-28)
- **2026-11-18 thread resume＋訊息不擱淺（feat/coordinator-fork-cache）**：SendMessage 對已結束 agent（completed/failed/stopped）重開續話——resumeAgent 等舊 loopDone 關閉後重啟 loop（RunAgentFn 以 AgentID 保留歷史，見 bootstrap runStates）。防擱淺協議：SendMessage 的 enqueue＋status 檢查與 runAgentLoop 的 exit-check 同在 entry.mu 下原子化；exit 時 inbox 仍有訊息→有界 continuation re-run（maxAgentContinuations=100，防忽略 inbox 的 RunAgentFn 空轉；契約：RunAgentFn 必須 drain inbox，engine 經 QueryParams.InboxCh 為之）。inboxPending 用 len() 非消費快照（<-inbox 會吃掉訊息）。StopAgent 的 killed 狀態於 loop exit 保留（修 TestAdapterStopAgent flake 根因）。系統提示重寫自 claude-projects-coordinator.md＋opus-5.5 Agent 段：常駐 coordinator、不先問就開 thread、instructions 只給 context 不加戲、對 thread 的話永遠不是使用者批准、成本感知委派。
- **2026-11-18 實機驗證修復：agent 生命週期脫鉤（fix/agent-lifetime）**：SpawnAgent 原以 caller ctx（＝父 query 的 tool-call ctx）為 agent ctx 父源——父 turn 結束（Query defer cancel）或使用者送下一訊息（TUI abortFn）即取消所有 background agent/fork，實機「委派一直失敗」根因。現以 context.WithoutCancel(ctx) 為 parentCtx（保留 values、去除 cancellation）：thread 存活至完成或 StopAgent，對齊 Projects thread 語義。resumeAgent 的 parentCtx 同源，父 ctx 取消後仍可續話。守護測試：TestSpawnAgent_SurvivesParentContextCancel、TestSendMessage_ResumeAfterParentContextCancel。
- **2026-11-19 完成通知携带 result/summary（feat/coordinator-completion-wake）**：OnStatusChangeFn +result/summary 參數（running 時空字串）；Event +Result/Summary 欄位。runAgentLoop 完成呼叫帶 entry.result＋summary。此前 FormatTaskNotification 僅測試使用、生產從不注入—— coordinator prompt 承諾的 <task-notification> 協議從未兑现，背景 agent 完成後主腦無從得知。現由 TUI 消费（見 tui Design Notes）。
- **2026-11-23 系統提示加「Reporting to the user」段（fix/repetitive-replies）**：實機 transcript 中主腦在工具呼叫前預告一段、做完再以幾乎相同的開頭總結一次，同一件事（研究線程失敗）講三遍。新段落說明工具呼叫前的文字也會即時顯示：預告最多一行，每個事實只講一次，最終訊息只報告新內容，被截斷時從截斷處續寫。Cache-first：靜態常數變更，coordinator 模式的 system 前綴一次性移動（成本決策：換取不重複的輸出，輸出 token 比前綴快取更貴）；不增大窗口外的呼叫、不新增呼叫。
