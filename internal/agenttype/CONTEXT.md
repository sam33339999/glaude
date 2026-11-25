---
package: agenttype
import_path: internal/agenttype
layer: infra
generated_at: 2026-11-25T16:45:14Z
source_files: [agenttype.go, builtins.go, loader.go, prompts.go, registry.go, tasktype.go]
---

# internal/agenttype

> Layer: **Infra** · Files: 6 · Interfaces: 0 · Structs: 3 · Functions: 7

## Structs

- **AgentProfile** — 9 fields: Type, DisplayName, Description, WhenToUse, SystemPrompt, ToolFilter, Model, MaxTurns, ...
- **Registry** — 3 fields
- **ToolFilter** — 2 fields: Mode, Tools

## Functions

- `FormatForkDirective(directive string) string`
- `LoadCustomAgents(dir string) ([]*AgentProfile, error)`
- `NewRegistry() *Registry`
- `RegisterBuiltins(r *Registry)`
- `ResolveAliasForProvider(provider string, parentModel string, model string) string`
- `ResolveModel(parentModel string, toolModel string, profileModel string) string`
- `ResolveModelForProvider(provider string, parentModel string, toolModel string, profileModel string) string`

## Constants

- `AgentTypeCustom`
- `AgentTypeExplore`
- `AgentTypeFork`
- `AgentTypeGuide`
- `AgentTypePlan`
- `AgentTypeVerify`
- `AgentTypeWorker`
- `ForkBoilerplate`
- `TaskTypeDream`
- `TaskTypeInProcessTeammate`
- `TaskTypeLocalAgent`
- `TaskTypeLocalBash`
- `TaskTypeLocalWorkflow`
- `TaskTypeMonitorMCP`
- `TaskTypeRemoteAgent`
- `ToolFilterAllowlist`
- `ToolFilterDenylist`

## Change Impact

**Test Mocks (must add new methods when interfaces change):**
- `stubRegistry` in `internal/permissions/checker_test.go`

**Exported type references (files that use types from this package):**
- `AgentProfile` → `internal/bootstrap/agents_cmd.go`, `internal/bootstrap/wire.go`
- `Registry` → `internal/bootstrap/acp.go`, `internal/bootstrap/wire.go`, `internal/bootstrap/wire_fork_test.go` (test)
- `ToolFilter` → `internal/bootstrap/wire.go`

## Dependencies

**Imports:** *(none — zero-dependency)*

**Imported by:** `internal/bootstrap`, `internal/tools/tasks`, `internal/tui`

<!-- AUTO-GENERATED ABOVE — DO NOT EDIT -->
<!-- MANUAL NOTES BELOW — preserved across regeneration -->

## Design Notes

- **2026-11-18 fork agent type（feat/coordinator-fork-cache）**：AgentTypeFork 的 SystemPrompt 與 ToolFilter.Tools **刻意為空**——fork 繼承父的 system prompt 與完整工具列表，使請求前綴（tools schema bytes＋system＋歷史）與父 byte-identical，命中 prompt cache（KV-cache 复用，便宜快取輸入）。禁止再委派由 ForkBoilerplate 在 prompt 層 enforcement（fork 看得見 Agent 工具但被禁止使用；bootstrap 側 UseContext.Coordinator=nil 兜底 fail-closed），而非從 schema 移除工具——移除會改 schema bytes、破壞 cache 前綴。來源：Anthropic 洩漏 claude-code/agents/fork.md（one-shot、背景執行、model 繼承、maxTurns 200）。FormatForkDirective 將 boilerplate＋directive 包成尾部 user message（繼承歷史之後），保證 tool_use/tool_result 序列合法。
- **2026-11-19 provider-aware model alias（fix/subagent-model-alias）**：實機重現「委派一直失敗」根因——explore profile Model:"haiku" 直送 OpenAI 相容 endpoint→404 "Model not exist."（debug log 證實）。新增 ResolveModelForProvider(provider, ...)：openai provider 上 Anthropic family alias（haiku/sonnet/opus）→繼承父 model（空字串＝engine 預設，唯一已知有效 model）；完整 model ID 全 provider 通行；CLAUDE_CODE_SUBAGENT_MODEL env override openai 直送（使用者自知 endpoint）、Anthropic provider 保留 legacy alias 解析。ResolveModel 保留為 provider="" wrapper（向後兼容）。wire.go buildSubAgentQuery +provider 參數（store.GetState().Settings.Provider live 讀）。守護測試：agenttype 表格×9＋env×2、bootstrap explore fallback。
- **2026-11-19 等級語意**：explore profile 的 `Model` 改為 `"lite"`（舊別名仍有效，只是文件語意一致）。新增 `ResolveAliasForProvider`：單一值套用別名規則但不讀 `CLAUDE_CODE_SUBAGENT_MODEL`，供 bootstrap 在等級未綁定時退回舊別名。等級解析本身在 `internal/modeltier`，本包不依賴它。
