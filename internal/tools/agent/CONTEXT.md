---
package: agent
import_path: internal/tools/agent
layer: tools
generated_at: 2026-11-24T06:00:34Z
source_files: [agent.go, doc.go, external.go, getagentstatus.go, sendmessage.go]
---

# internal/tools/agent

> Layer: **Tools** · Files: 5 · Interfaces: 0 · Structs: 7 · Functions: 1

## Structs

- **AgentInput** — 8 fields: Prompt, SystemPrompt, AllowedTools, MaxTurns, AgentType, Background, AgentName, Model
- **AgentOutput** — 2 fields: Response, AgentID
- **ExternalAgentInfo** — 2 fields: Name, Description
- **GetAgentStatusInput** — 1 fields: AgentID
- **GetAgentStatusOutput** — 3 fields: AgentID, Status, Result
- **SendMessageInput** — 3 fields: To, AgentID, Content
- **SendMessageOutput** — 1 fields: Response

## Functions

- `SetExternalAgents(infos []ExternalAgentInfo)`

## Change Impact

**Exported type references (files that use types from this package):**
- `ExternalAgentInfo` → `internal/bootstrap/acp.go`

## Dependencies

**Imports:** `internal/textutil`, `internal/tools`

**Imported by:** `internal/bootstrap`

<!-- AUTO-GENERATED ABOVE — DO NOT EDIT -->
<!-- MANUAL NOTES BELOW — preserved across regeneration -->

## Design Notes

- **2026-09-23 基座换库（agtkeel import-swap）**：可复用 agent 内核已逐字提取到 `github.com/tunsuy/agtkeel`（MIT），本包 import 随之改写：`internal/{api,compact,msgqueue,engine,session,permissions,hooks,mcp,config}` → `agtkeel/*`、`internal/tools`（接口契约+注册表）→ `agtkeel/tools`、`pkg/types` → `agtkeel/types`、`pkg/utils/{fs,ids}` → `agtkeel/utils/*`。本包代码**仅 import 路径重写，零逻辑改动**，`go build`/`go vet`/`go test -race ./...` 全绿。已知表现局限：docgen 依赖图按本模块前缀分类，agtkeel 视作第三方库，故上方 Imports 栏不再展示内核依赖（与 cobra 等一致）。
- **2026-11-18 fork 語義＋leak 對齊（feat/coordinator-fork-cache）**：agent_type "fork" 強制 background=true、model override 忽略（繼承父 model）；Call 轉發 ctx.CacheParams 至 spawn request。Agent/SendMessage description 依 Anthropic 洩漏（claude-code-opus-5.5.md Agent 段、claude-projects-coordinator.md）重寫：成本感知委派（when in doubt, don't spawn）、禁止虛構 pending 結果、SendMessage 續話保持 context intact（已結束 agent 重開）、brief like a peer。**schema bytes 一次性變更**（description 屬 tool schema）——新會話基線，會話內釘住（cache-first 閘門已於設計文件回答三問）。
- **2026-11-19 GetAgentStatus name resolution（fix/getagentstatus-name-resolution）**：實機「GetAgentStatus("code-reader") 找不到」根因——Call 直送 in.AgentID 至 ID-keyed result map，**無 name resolution**（SendMessage 有 ResolveAgent、GetAgentStatus 沒有，不對稱）。LLM 用指派的 name 查→unknown agent。修：Call 先 ResolveAgent（name→ID）再 GetAgentResult；output AgentID=resolved ID；description/schema 明示接受 ID 或 name。註冊本身無 race（SpawnAgent 於 c.mu 下同步註冊 agents map＋nameIndex 後才 return）。守護測試×3（name 解析／unknown name 錯誤／ID 直查不變）。schema bytes 一次性變更（description）——會話內釘住。
