---
package: mcp
import_path: internal/tools/mcp
layer: tools
generated_at: 2026-11-25T17:04:06Z
source_files: [doc.go, mcp.go]
---

# internal/tools/mcp

> Layer: **Tools** · Files: 2 · Interfaces: 0 · Structs: 2 · Functions: 3

## Structs

- **ListMcpResourcesInput** — 1 fields: ServerName
- **ReadMcpResourceInput** — 2 fields: ServerName, URI

## Functions

- `NewListMcpResourcesTool(pool *agtmcp.Pool) tools.Tool`
- `NewReadMcpResourceTool(pool *agtmcp.Pool) tools.Tool`
- `RegisterServerTools(ctx context.Context, reg *tools.Registry, pool *agtmcp.Pool) (int, error)`

## Dependencies

**Imports:** `internal/mcp`, `internal/tools`

**Imported by:** `internal/bootstrap`

<!-- AUTO-GENERATED ABOVE — DO NOT EDIT -->
<!-- MANUAL NOTES BELOW — preserved across regeneration -->

## Design Notes

- **2026-09-23 基座换库（agtkeel import-swap）**：可复用 agent 内核已逐字提取到 `github.com/tunsuy/agtkeel`（MIT），本包 import 随之改写：`internal/{api,compact,msgqueue,engine,session,permissions,hooks,mcp,config}` → `agtkeel/*`、`internal/tools`（接口契约+注册表）→ `agtkeel/tools`、`pkg/types` → `agtkeel/types`、`pkg/utils/{fs,ids}` → `agtkeel/utils/*`。本包代码**仅 import 路径重写，零逻辑改动**，`go build`/`go vet`/`go test -race ./...` 全绿。已知表现局限：docgen 依赖图按本模块前缀分类，agtkeel 视作第三方库，故上方 Imports 栏不再展示内核依赖（与 cobra 等一致）。

- **2026-11-18 BF-01 MCP 三工具接线（桩回填）**：删除本地 `MCPProxyTool`/`NewMCPProxyTool`/`MCPToolInput` 桩，每服务器代理工具改为复用 agtkeel 内核 `mcp.AdaptToTool`（避免重复实现已工作的内核代码）；新增 `RegisterServerTools(ctx, reg, pool)` 在启动时从 `Pool.GetConnected()` 发现并注册各服务器工具。`ListMcpResources`/`ReadMcpResource` 改为构造函数 `NewListMcpResourcesTool(pool)`/`NewReadMcpResourceTool(pool)`——`tools.UseContext` 无 MCP 字段，pool 依赖只能在注册期注入（bootstrap 负责连线，工具层不 import bootstrap）。nil pool → 诚实错误；空 pool → 空列表/未连接提示，不 panic。测试用 httptest 轻量 MCP stub server（Streamable HTTP + 按 Mcp-Session-Id 路由响应队列）走真实 Pool→client 全链路。
- **会话钉住（cache-stability，PM 补充验收约束）**：`RegisterServerTools` 对每个 registry 仅首次调用生效（boot 阶段、首个 LLM 请求前一次性完成），后续调用 no-op——重连后 live tool list 变化也不改写本会话工具名集合与 schema 字节，差异留到下个会话启动。原因：增删工具/改 schema 会移动 provider 前缀缓存（docs/PROMPT_CACHE_AND_COST.md "Pinned MCP placeholders"、Boundaries §4）。守护测试：`TestRegisterServerTools_PinnedAcrossReconnect`（注册→改 tool list→真实 Reconnect→断言名集合+schema 字节+description 不变）。钉住标记存于包级 `pinnedRegistries` sync.Map（键为进程生命周期的 boot registry）。`Description()`/`InputSchema()` 输出静态字节，动态连接状态只进 /mcp 输出。
- **2026-11-18 BF-01-F1/F2 评审修复（CR-BF01-2/3/4）**：boot ListTools 加每服务器 deadline（`listToolsTimeout` var，生产 10s，`SetListToolsTimeoutForTest` 注入）；钉住生命周期文档化＋test-only reset hook（`export_test.go`，不进生产构建）：registry 生命周期＝会话，标记进程生命周期保留，注册失败亦钉住（本会话不可经重试恢复）。`ListMcpResources` 聚合输出加 `maxResourceEntries=200` 上限（超限截断＋诚实提示，cache gate Q2 窗口收紧；schema 字节不变）。守护测试补强：fresh-registry 对照证明 reconnect 差异真实存在且钉住是 per-registry 语义；partial-failure Note、nil uctx、empty-content、truncation 用例补齐。
