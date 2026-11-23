---
package: acpclient
import_path: internal/acpclient
layer: services
generated_at: 2026-11-23T14:09:22Z
source_files: [client.go, manager.go, permission.go, proc.go, spec.go]
---

# internal/acpclient

> Layer: **Services** · Files: 5 · Interfaces: 0 · Structs: 9 · Functions: 8

## Structs

- **AgentSpec** — 7 fields: Name, Command, Args, Env, Model, Description, SessionMeta
- **Event** — 5 fields: Kind, Text, ToolCallID, Title, Status
- **Manager** — 7 fields
- **ModelCatalog** — 4 fields: Current, ThoughtLevel, ThoughtLevels, Models
- **ModelChoice** — 2 fields: ID, Name
- **Options** — 5 fields: Permission, Start, Logf, StartupTimeout, ClientVersion
- **PermissionQuery** — 6 fields: Agent, ToolCallID, Title, Kind, ToolName, Input
- **Proc** — 6 fields: Stdin, Stdout
- **Result** — 4 fields: Text, StopReason, NewSession, Restarted

## Function Types

- `PermissionFunc` — `func(ctx context.Context, q PermissionQuery) (bool, error)`
- `StartFunc` — `func(ctx context.Context, spec AgentSpec, cwd string) (*Proc, error)`

## Functions

- `Builtins() map[string]AgentSpec`
- `ExecStart(ctx context.Context, spec AgentSpec, cwd string) (*Proc, error)`
- `MapToolCall(agent string, tc acp.ToolCallUpdate) PermissionQuery`
- `NewManager(specs map[string]AgentSpec, opts Options) *Manager`
- `NewProc(stdin io.WriteCloser, stdout io.Reader, done chan any, kill func()) *Proc`
- `PickOption(options []acp.PermissionOption, allow bool) (acp.PermissionOptionId, bool)`
- `Resolve(overrides map[string]config.ACPAgentConfig) map[string]AgentSpec`
- `SortedNames(specs map[string]AgentSpec) []string`

## Constants

- `EventNotice`
- `EventPlan`
- `EventText`
- `EventThought`
- `EventToolCall`
- `EventToolUpdate`
- `GenericToolName`

## Change Impact

**Exported type references (files that use types from this package):**
- `AgentSpec` → `internal/bootstrap/acp.go`, `internal/bootstrap/acp_test.go` (test)
- `Event` → `internal/bootstrap/acp.go`
- `Manager` → `internal/bootstrap/acp.go`, `internal/bootstrap/acp_test.go` (test), `internal/bootstrap/wire.go`
- `ModelCatalog` → `internal/bootstrap/acp.go`
- `Options` → `internal/bootstrap/acp.go`, `internal/bootstrap/acp_test.go` (test)
- `PermissionFunc` → `internal/bootstrap/acp.go`, `internal/bootstrap/acp_test.go` (test)
- `PermissionQuery` → `internal/bootstrap/acp.go`, `internal/bootstrap/acp_test.go` (test)
- `Proc` → `internal/bootstrap/acp_test.go` (test)

## Dependencies

**Imports:** `internal/config`

**Imported by:** `internal/bootstrap`

<!-- AUTO-GENERATED ABOVE — DO NOT EDIT -->
<!-- MANUAL NOTES BELOW — preserved across regeneration -->

## Design Notes

- **2026-11-21 ACP 外部 agent（client 端）**：glaude 以 ACP client 身分驅動外部 coding agent（內建 `grok` = `grok agent stdio`、`opencode` = `opencode acp`；settings `acpAgents` 可覆寫/新增/停用）。SDK：`github.com/coder/acp-go-sdk` v0.20.0。
- **Session 壽命**：每個 (agent, cwd) 一個行程＋一個 ACP session，活到 glaude 退出（`AppContainer.Close`）。同一 agent 的所有派工（Agent 工具 `agent_type=<name>`、SendMessage 續接、`/<name>` 指令）都進同一 session，prompt 以 slot 鎖串行。行程死掉 → 下次 Prompt 自動重啟並回報 `Restarted`（舊 context 已失）。
- **不開 sub agent**：grok 以 env `GROK_SUBAGENTS=0`、`GROK_WORKFLOWS=0`（實測 `spawn_subagent` 工具消失）；opencode 以 `OPENCODE_CONFIG_CONTENT` 注入 `permission.task=deny`（實測 `task` 工具消失）。
- **權限**：不宣告 fs/terminal 能力，agent 用自己的工具。使用者層設定可能是全放行（例：`~/.grok/config.toml permission_mode="always-approve"`），故 grok 送 `_meta.yoloMode=false`、opencode 注入 `edit/bash/webfetch=ask`，強制 `session/request_permission` 回到 glaude。`MapToolCall` 把 ACP tool call 轉成 glaude 工具形狀（execute→Bash、edit→Edit/Write、read→Read、search→Grep、fetch→WebFetch、其餘→`ExternalAgent`），交 bootstrap 的 checker 判定（規則、受保護路徑、Bash 安全檢查、權限模式皆生效）；headless 未命中規則一律拒絕。選項優先 `allow_once`/`reject_once`，「永遠允許」由 glaude 自己落規則。
- **輸出隔離**：子行程 stderr 只留最後 4 KiB 供錯誤訊息；SDK 的 slog 導到 `Options.Logf`，絕不寫到終端（會弄壞 TUI）。
- **測試**：`acptest.Agent` 是可腳本化的假 agent（只依賴 SDK，避免循環 import）；`TestExecStart_RealProcess` 以測試二進位自身當子行程跑真 stdio。SDK agent 端建構時有 `asc.conn` 賦值競態，假 agent 以 gatedReader 迴避（glaude 只用 client 端）。
- **2026-11-22 內建 `agy`（`agy acp`）與動態模型切換**：`Builtins()` 新增 `agy`（預設 `Model: ""` 沿用使用者 agy 設定，可由 `settings.json` `acpAgents.agy.model` 覆寫；實測 `agy acp` 的 `configId="model"` 同時接受基礎名如 `gemini-4-argon` 與含推理強度的複合名如 `gemini-4-argon-medium`）。`MapToolCall` 補齊 `agy` 工具的 PascalCase 參數映射（`CommandLine`→Bash、`Path`/`Target`/`Replacement`/`Content`→Read/Edit/Write、`Query`/`Pattern`/`SearchPath`→Grep、`Url`→WebFetch）。`Manager.PromptWithModel` 支援每次派工覆寫模型：冷啟動時直接取代 `spec.Model`（只發一次 `set_config_option`），既有 session 若與 `activeModel` 不同才發 `set_config_option`。
