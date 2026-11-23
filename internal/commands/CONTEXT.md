---
package: commands
import_path: internal/commands
layer: cli
generated_at: 2026-11-23T07:58:32Z
source_files: [builtins.go, external.go, goal.go, loop.go, mcp.go, modelswitch.go, provider.go, registry.go, resume.go, setup.go, soul.go, terminal_setup.go, tier.go]
---

# internal/commands

> Layer: **CLI** · Files: 13 · Interfaces: 6 · Structs: 19 · Functions: 13

## Interfaces

### MCPServerLister (1 methods)
> MCPServerLister is the read-only view of the MCP connection pool required

```go
type MCPServerLister interface {
    GetAll() []*mcp.ServerConnection
}
```

### ModelSwitcher (3 methods)
> ModelSwitcher switches the main loop between the startup model and tier

```go
type ModelSwitcher interface {
    Options() []ModelOption
    Resolve(ref string) (ModelOption, error)
    Apply(opt ModelOption) error
}
```

### ProviderBackend (7 methods)
> ProviderBackend is what the /provider wizard needs from the outside

```go
type ProviderBackend interface {
    List() []ProviderInfo
    Warnings() []string
    MainProvider() string
    Writable() error
    ListModels(ctx context.Context, in ProviderInput) ([]SetupModel, error)
    Apply(ctx context.Context, plan ProviderPlan) (ProviderReport, error)
    Delete(id string) (ProviderReport, error)
}
```

### SetupBackend (5 methods)
> SetupBackend is what the /setup wizard needs from the outside world.

```go
type SetupBackend interface {
    Current() SetupDefaults
    ListModels(ctx context.Context, in SetupInput) ([]SetupModel, error)
    Apply(ctx context.Context, plan SetupPlan) (SetupReport, error)
    Overrides() []string
    NeedsSetup() bool
}
```

### TierManager (7 methods)
> TierManager is the tier table view /tier needs. *modeltier.Store

```go
type TierManager interface {
    List() []modeltier.Binding
    Providers() []modeltier.ProviderStatus
    Bind(tier string, providerID string, modelID string) error
    BindWithLimits(tier string, providerID string, modelID string, contextWindow int) error
    Unbind(tier string) error
    MainContextWindow() int
    Warnings() []string
}
```

### TierModelLister (1 methods)
> TierModelLister is optionally implemented by the TierManager handed to the

```go
type TierModelLister interface {
    ListProviderModels(ctx context.Context, providerID string) ([]SetupModel, error)
}
```

## Structs

- **Command** — 3 fields: Name, Description, Execute
- **CommandContext** — 10 fields: WorkingDir, Model, SessionID, DarkMode, VimEnabled, Effort, MessageCount, TokensInput, ...
- **ExternalAgentCommand** — 3 fields: Name, Description, ListModels
- **GoalVerdict** — 2 fields: Met, Reason
- **LoopSpec** — 4 fields: Action, Interval, DefaultInterval, Prompt
- **ModelOption** — 9 fields: Name, Group, Provider, Model, ContextWindow, MaxTokens, Available, Reason, ...
- **ProviderInfo** — 8 fields: ID, Format, BaseURL, KeySource, KeyEnv, KeyPresent, Models, Tiers
- **ProviderInput** — 5 fields: ID, Format, BaseURL, APIKey, NoKey
- **ProviderPlan** — 4 fields: Input, IsNew, Models, Tiers
- **ProviderReport** — 3 fields: MainSwitched, Unbound, Warnings
- **Registry** — 2 fields
- **Result** — 15 fields: Text, Display, ShouldExit, NewMessages, NewSessionID, NewTheme, NewModel, ModelRef, ...
- **SessionSummary** — 5 fields: ID, Title, ModTime, MessageCount, Current
- **SetupDefaults** — 5 fields: Format, BaseURL, Model, KeySources, Tiers
- **SetupInput** — 3 fields: Format, BaseURL, APIKey
- **SetupModel** — 2 fields: ID, ContextWindow
- **SetupPlan** — 4 fields: Input, MainModel, Tiers, ContextWindows
- **SetupReport** — 5 fields: SettingsPath, ConfigPath, SettingsWritten, ConfigWritten, Warnings
- **TerminalInfo** — 3 fields: Terminal, Shell, Term

## Function Types

- `ExternalAgentSpawner` — `func(name string, model string, task string) (string, error)`

## Functions

- `DetectTerminal(lookup func(string) string) TerminalInfo`
- `FormatInterval(d time.Duration) string`
- `FormatMCPStatus(ctx context.Context, pool MCPServerLister) string`
- `FormatTerminalSetup(info TerminalInfo) string`
- `GoalContinuePrompt(condition string, reason string, n int, max int) string`
- `GoalJudgeUser(condition string, transcript string) string`
- `GoalKickoffPrompt(condition string) string`
- `NewRegistry() *Registry`
- `ParseGoalArgs(args string) (GoalAction, string)`
- `ParseGoalVerdict(text string) (GoalVerdict, error)`
- `ParseLoopArgs(args string) (LoopSpec, error)`
- `RegisterBuiltins(r *Registry)`
- `RegisterExternalAgents(r *Registry, agents []ExternalAgentCommand, spawn ExternalAgentSpawner)`

## Constants

- `DisplayDialog`
- `DisplayError`
- `DisplayMessage`
- `DisplayNone`
- `GoalClear`
- `GoalJudgeSystem`
- `GoalMaxContinuations`
- `GoalSet`
- `GoalStatus`
- `GoalUsage`
- `LoopDefaultInterval`
- `LoopMinInterval`
- `LoopStart`
- `LoopStatus`
- `LoopStop`
- `LoopUsage`
- `SetupFormatAnthropic`
- `SetupFormatOpenAI`

## Change Impact

**Test Mocks (must add new methods when interfaces change):**
- `stubRegistry` in `internal/permissions/checker_test.go`
- `fakeProviderBackend` in `internal/tui/provider_dialog_test.go`
- `fakeSetupBackend` in `internal/tui/setup_dialog_test.go`
- `fakeTierManager` in `internal/tui/tierpicker_test.go`

**Exported type references (files that use types from this package):**
- `Command` → `internal/tui/tui_test.go` (test), `internal/tui/update.go`
- `CommandContext` → `internal/tui/tui_test.go` (test), `internal/tui/update.go`
- `ExternalAgentCommand` → `internal/bootstrap/acp.go`
- `GoalVerdict` → `internal/tui/autorun.go`, `internal/tui/autorun_test.go` (test)
- `MCPServerLister` → `internal/tui/cmds.go`, `internal/tui/init.go`, `internal/tui/mcp_resume_test.go` (test), `internal/tui/model.go`
- `ModelOption` → `internal/bootstrap/modelswitch.go`, `internal/bootstrap/modelswitch_test.go` (test), `internal/tui/model.go`, `internal/tui/modelpicker.go`, `internal/tui/modelpicker_provider_test.go` (test) + 1 more
- `ModelSwitcher` → `internal/bootstrap/modelswitch.go`, `internal/bootstrap/wire.go`, `internal/tui/init.go`, `internal/tui/model.go`, `internal/tui/modelpicker_test.go` (test)
- `ProviderBackend` → `internal/bootstrap/provider.go`, `internal/bootstrap/wire.go`, `internal/tui/model.go`, `internal/tui/provider_dialog.go`, `internal/tui/provider_dialog_test.go` (test)
- `ProviderInfo` → `internal/bootstrap/provider.go`, `internal/tui/provider_dialog_test.go` (test), `internal/tui/providerwizard.go`, `internal/tui/providerwizard_test.go` (test), `internal/tui/providerwizard_view.go`
- `ProviderInput` → `internal/bootstrap/provider.go`, `internal/bootstrap/provider_test.go` (test), `internal/tui/provider_dialog_test.go` (test), `internal/tui/providerwizard.go`
- `ProviderPlan` → `internal/bootstrap/provider.go`, `internal/bootstrap/provider_test.go` (test), `internal/tui/provider_dialog_test.go` (test), `internal/tui/providerwizard.go`
- `ProviderReport` → `internal/bootstrap/provider.go`, `internal/tui/provider_dialog.go`, `internal/tui/provider_dialog_test.go` (test), `internal/tui/providerwizard.go`, `internal/tui/providerwizard_test.go` (test)
- `Registry` → `internal/bootstrap/acp.go`, `internal/tui/completion.go`, `internal/tui/init.go`, `internal/tui/model.go`
- `Result` → `internal/tui/autorun_test.go` (test), `internal/tui/bashmode_test.go` (test), `internal/tui/btw_send_test.go` (test), `internal/tui/btw_test.go` (test), `internal/tui/mcp_resume_test.go` (test) + 8 more
- `SessionSummary` → `internal/tui/mcp_resume_test.go` (test), `internal/tui/resumepicker.go`
- `SetupBackend` → `internal/bootstrap/setup.go`, `internal/bootstrap/wire.go`, `internal/tui/init.go`, `internal/tui/model.go`, `internal/tui/setup_dialog.go` + 1 more
- `SetupDefaults` → `internal/bootstrap/setup.go`, `internal/tui/setup_dialog_test.go` (test), `internal/tui/setupwizard.go`, `internal/tui/setupwizard_test.go` (test)
- `SetupInput` → `internal/bootstrap/setup.go`, `internal/bootstrap/setup_test.go` (test), `internal/tui/setup_dialog.go`, `internal/tui/setup_dialog_test.go` (test), `internal/tui/setupwizard.go`
- `SetupModel` → `internal/bootstrap/provider.go`, `internal/bootstrap/provider_test.go` (test), `internal/bootstrap/setup.go`, `internal/bootstrap/tiers.go`, `internal/tui/modelchooser.go` + 12 more
- `SetupPlan` → `internal/bootstrap/setup.go`, `internal/bootstrap/setup_test.go` (test), `internal/tui/setup_dialog.go`, `internal/tui/setup_dialog_test.go` (test), `internal/tui/setupwizard.go`
- `SetupReport` → `internal/bootstrap/setup.go`, `internal/tui/setup_dialog.go`, `internal/tui/setup_dialog_test.go` (test), `internal/tui/setupwizard.go`, `internal/tui/setupwizard_test.go` (test) + 1 more
- `TierManager` → `internal/bootstrap/tiers.go`, `internal/tui/init.go`, `internal/tui/model.go`, `internal/tui/tier_dialog.go`, `internal/tui/tierpicker.go`
- `TierModelLister` → `internal/bootstrap/tiers.go`, `internal/tui/tier_dialog.go`

## Dependencies

**Imports:** `internal/mcp`, `internal/memdir`, `internal/modeltier`, `internal/session`, `internal/soul`, `pkg/types`, `pkg/utils/fs`

**Imported by:** `internal/bootstrap`, `internal/tui`

<!-- AUTO-GENERATED ABOVE — DO NOT EDIT -->
<!-- MANUAL NOTES BELOW — preserved across regeneration -->

## Design Notes

- **2026-11-18 BF-03（/mcp、/resume 真实实现）**：移除两处用户可见桩。新增 `mcp.go`（`MCPServerLister` 只读池视图接口 + `FormatMCPStatus` 渲染，connected server 经 `ListTools` 附工具数，每 server 2s 超时、失败降级）与 `resume.go`（经 agtkeel `session`/`utils/fs` 扫描 `<cwd>/.glaude/projects/<hash>/*.jsonl`，列表 + 按 ID 恢复，ID 路径穿越校验，损坏条目跳过）。`Result` 新增 `FetchMCPStatus`（/mcp 异步报告标志，仿 TriggerDream 先例）与 `NewSessionID`（/resume 采用恢复会话 ID）；`NewMessages` 契约明确为携带 `[]types.Message`、由 TUI 类型断言消费。/mcp 的 Execute 不做网络 I/O（报告由 TUI 的 tea.Cmd 异步生成）；/resume 的文件 I/O 与既有 /memory 同步模式一致。本包新增对 agtkeel `mcp`/`session`/`types`/`utils/fs` 的向下依赖，未依赖 bootstrap/internal/tools。设计详见 `docs/project/design/tui.md` §4.1。
- **2026-11-18 BF-03-F1 评审修复（CR-BF03-2/3/4）**：`MCPServerLister`/`FormatMCPStatus` GoDoc 钉住「GetAll 返回活指针、读取期间禁止并发池突变」约束（快照根治待跨仓 BF-19；BF-01 并发写者落地前必须关闭，否则升级 P0）；渲染侧每连接值拷贝一次作廉价防御（非同步手段，注释明示）。/resume 的 `session.Resume` 底层错误经 %w 保留于友好文案（用户可见文案不变），`mgr.Close` 失败显式返回错误（与 listSessions 模式一致）。
- **2026-11-18 BF-05 /config＋/terminal-setup 桩回填**：cmdConfig 改返回 Result{OpenDialog:"config"}（TUI 既有 wiring），面板只读（编辑经 /model /theme /vim /effort 与设置文件，交互编辑为后续项）。新增 terminal_setup.go：DetectTerminal(lookup) 纯函数环境检测（12 种终端识别，未知诚实报告），FormatTerminalSetup 非侵入指引（Ctrl+J 多行提示＋cobra completion 指引），不写文件不改终端设置（与原版键绑定安装的差距已在 tui.md v1.3 文档化）。
- **2026-11-19 `/tier`**：`TierManager` 介面（`*modeltier.Store` 直接實作），經 `CommandContext.Tiers` 注入（commands 不得 import bootstrap）。改綁定在 Execute 內同步寫檔（小檔案，比照其他同步指令），下一個派出的 sub-agent 生效。等級表是唯一狀態來源，TUI 不需另外更新狀態。
- 最終審查後補修：`TierManager` +`Warnings()`；`/tier` 列表最上方顯示 config.toml 的啟動警告（TUI 的 alt screen 會蓋掉 stderr）。
- **2026-11-19 `/setup` 與 `SetupBackend`**：`/setup` 只回傳 `OpenDialog: "setup"`，commands 層不做 I/O、不 import `internal/api`（資料型別自己定義）。`SetupDefaults.KeySources` 依格式記錄可沿用的金鑰來源（`settings` 只對目前格式有效、`env:<VAR>`），讓換格式後的提示與阻擋正確。`SetupInput.APIKey` 為空表示沿用，沿用的金鑰由 backend 取得，不經過 TUI。
- **2026-11-20 `/model`**：無參數回 `OpenDialog: "model"`，有參數回 `ModelRef`；解析與切換都由注入的 `ModelSwitcher` 做（commands 不 import bootstrap）。`ModelOption.Label()` 統一顯示 `provider:model`。
- **2026-11-20 /btw**：`cmdBTW` 只為了 /help 與補全而註冊；TUI 在指令分派前就攔下 `/btw`，所以查詢進行中也能立即執行。
- **2026-11-22 外部 ACP agent 斜線指令 `-m`/`--model`**：`ExternalAgentSpawner` 簽名擴為 `func(name, model, task string) (string, error)`；`/<agent> [-m <model>|--model=<model>] <task>` 解析選填模型旗標並轉交 `AgentSpawnRequest.Model`。
