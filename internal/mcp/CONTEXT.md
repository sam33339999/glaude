---
package: mcp
import_path: internal/mcp
layer: services
generated_at: 2026-11-25T16:47:52Z
source_files: [adapter.go, client.go, jsonrpc.go, pool.go, transport.go, transport_ws.go, wake.go]
---

# internal/mcp

> Layer: **Services** · Files: 7 · Interfaces: 2 · Structs: 17 · Functions: 5

## Interfaces

### MCPClient (7 methods)
> MCPClient represents a connection to a single MCP server.

```go
type MCPClient interface {
    ListTools(ctx context.Context) ([]MCPToolDef, error)
    CallTool(ctx context.Context, name string, args map[string]any) (*MCPToolResult, error)
    ListResources(ctx context.Context) ([]MCPResource, error)
    ReadResource(ctx context.Context, uri string) (*MCPResourceContent, error)
    Ping(ctx context.Context) error
    Close() error
    ServerInfo() ServerInfo
}
```

### Transport (3 methods)
> Transport is the abstraction for a bidirectional JSON-RPC communication channel.

```go
type Transport interface {
    Send(ctx context.Context, msg *JSONRPCMessage) error
    Recv(ctx context.Context) (*JSONRPCMessage, error)
    Close() error
}
```

## Structs

- **HTTPTransportConfig** — 3 fields: URL, Headers, OAuth
- **JSONRPCError** — 3 fields: Code, Message, Data
- **JSONRPCMessage** — 6 fields: JSONRPC, ID, Method, Params, Result, Error
- **MCPContent** — 4 fields: Type, Text, Data, MIMEType
- **MCPOAuthConfig** — 3 fields: ClientID, CallbackPort, AuthServerMetadataURL
- **MCPResource** — 4 fields: URI, Name, Description, MIMEType
- **MCPResourceContent** — 4 fields: URI, MIMEType, Text, Blob
- **MCPToolDef** — 3 fields: Name, Description, InputSchema
- **MCPToolResult** — 3 fields: Content, IsError, Meta
- **Pool** — 3 fields
- **SSETransportConfig** — 3 fields: URL, Headers, OAuth
- **ServerCapabilities** — 3 fields: Tools, Resources, Prompts
- **ServerConfig** — 6 fields: Transport, Stdio, SSE, HTTP, WS, Scope
- **ServerConnection** — 6 fields: Name, Status, Config, Client, Error, ReconnectAttempt
- **ServerInfo** — 4 fields: Name, Version, Capabilities, Instructions
- **StdioTransportConfig** — 3 fields: Command, Args, Env
- **WSTransportConfig** — 3 fields: URL, Headers, OAuth

## Function Types

- `NotificationHandler` — `func(method string, params json.RawMessage)`
- `ServerNotificationHandler` — `func(server string, method string, params json.RawMessage)`

## Functions

- `AdaptToTool(serverName string, def MCPToolDef, client MCPClient) tools.Tool`
- `NewPool() *Pool`
- `NewTransport(t TransportType, cfg any) (Transport, error)`
- `NormalizeToolName(name string) string`
- `WakesAgent(method string) bool`

## Constants

- `DefaultToolTimeout`
- `MaxMCPDescriptionLength`
- `StatusConnected`
- `StatusDisabled`
- `StatusFailed`
- `StatusNeedsAuth`
- `StatusPending`
- `TransportHTTP`
- `TransportSSE`
- `TransportStdio`
- `TransportWS`

## Change Impact

**Adapters (update when interfaces change):**
- `adapter.go`

**Test Mocks (must add new methods when interfaces change):**
- `fakePool` in `internal/commands/mcp_test.go`
- `fakeMCPClient` in `internal/commands/mcp_test.go`
- `mockTransport` in `internal/mcp/mcp_test.go`
- `mockMCPClient` in `internal/mcp/mcp_test.go`

**Exported type references (files that use types from this package):**
- `HTTPTransportConfig` → `internal/bootstrap/mcp_connect_test.go` (test), `internal/bootstrap/mcp_health.go`, `internal/tools/mcp/mcp_test.go` (test)
- `JSONRPCError` → `internal/bootstrap/mcp.go`, `internal/bootstrap/mcp_connect_test.go` (test), `internal/tools/mcp/mcp_test.go` (test)
- `JSONRPCMessage` → `internal/bootstrap/mcp.go`, `internal/bootstrap/mcp_connect_test.go` (test), `internal/tools/mcp/mcp_test.go` (test)
- `MCPClient` → `internal/commands/mcp_test.go` (test), `internal/tools/mcp/mcp.go`
- `MCPOAuthConfig` → `internal/bootstrap/mcp_health.go`
- `MCPResource` → `internal/commands/mcp_test.go` (test)
- `MCPResourceContent` → `internal/commands/mcp_test.go` (test)
- `MCPToolDef` → `internal/commands/mcp_test.go` (test)
- `MCPToolResult` → `internal/commands/mcp_test.go` (test)
- `Pool` → `internal/bootstrap/mcp_connect.go`, `internal/bootstrap/tools.go`, `internal/bootstrap/wire.go`, `internal/commands/mcp.go`, `internal/tools/mcp/mcp.go` + 1 more
- `SSETransportConfig` → `internal/bootstrap/mcp_health.go`
- `ServerConfig` → `internal/bootstrap/mcp_connect.go`, `internal/bootstrap/mcp_connect_test.go` (test), `internal/bootstrap/mcp_health.go`, `internal/commands/mcp_test.go` (test), `internal/tools/mcp/mcp_test.go` (test)
- `ServerConnection` → `internal/bootstrap/mcp_connect_test.go` (test), `internal/commands/mcp.go`, `internal/commands/mcp_test.go` (test), `internal/tools/mcp/mcp.go`
- `ServerInfo` → `internal/commands/mcp_test.go` (test)
- `ServerNotificationHandler` → `internal/bootstrap/wire.go`
- `StdioTransportConfig` → `internal/bootstrap/mcp_health.go`
- `Transport` → `internal/bootstrap/mcp_connect_test.go` (test), `internal/bootstrap/mcp_health.go`, `internal/bootstrap/mcp_health_test.go` (test), `internal/commands/mcp_test.go` (test), `internal/tools/mcp/mcp_test.go` (test)

## Dependencies

**Imports:** `internal/tools`

**Imported by:** `internal/bootstrap`, `internal/commands`, `internal/tools/mcp`

<!-- AUTO-GENERATED ABOVE — DO NOT EDIT -->
<!-- MANUAL NOTES BELOW — preserved across regeneration -->

## Design Notes

- **2026-11-18 内核搬回（反转 PR #31）**：本包原于 2026-09-23 逐字提取至外部仓 github.com/tunsuy/agtkeel；因该仓停止维护，经人类决策整併回本仓原始路径。来源：agtkeel main 3a77085＋后续提交，零逻辑改动（仅 import 路径恢复＋gofmt）。内核改动自此直接在本仓进行。
- **已知限制与待办**：WebSocket transport 为桩（BF-16，搬回后进入 debt 扫描范围，基线 18→19 属 scope 变化）；jsonRPCClient 后台读协程绑定 initialize ctx、httpTransport.Close 仅置旗标——根因修复待 BF-19（Pool 快照式 GetAll＋reader 生命周期解耦，原跨仓任务现为同仓）。产品侧规避见 bootstrap/mcp_connect.go 有界不遗弃 boot 连接与 tools/mcp 会话钉住。
- **2026-11-18 BF-16 WebSocket transport**：transport_ws.go（gorilla/websocket v1.5.3，Tech Lead 选型）；背景 reader＋recvCh/errCh（stdio 先例）、Recv ctx 取消、Send 写串行＋deadline、Close 幂等；ServerConfig.WS＋dialServer 分支（dial 用调用者 ctx）。测试：round-trip/取消/Close/dial 错误/Pool 全链路握手（initialize→tools/list）/nil config。自动重连待 BF-19（避免会话期后台 pool 变更）。
- **2026-11-18 EV-01 通知路由**：jsonRPCClient.notify（NotificationHandler）——reader 对 ID=nil＋method 的消息分发 handler（此前丢弃）；notify 读写 under c.mu（锁外调用，-race 钉住）。Pool.SetNotificationHandler（ServerNotificationHandler 含 server 名）覆盖既有＋未来连接含 Reconnect；dialServer 于 initialize 前设置。wire 转 msgqueue.ModeExternalPrompt 唤醒代理（零轮询事件驱动）。
