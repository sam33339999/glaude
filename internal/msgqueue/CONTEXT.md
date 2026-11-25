---
package: msgqueue
import_path: internal/msgqueue
layer: services
generated_at: 2026-11-25T15:40:49Z
source_files: [command.go, guard.go, queue.go, signal.go]
---

# internal/msgqueue

> Layer: **Services** · Files: 4 · Interfaces: 0 · Structs: 4 · Functions: 5

## Structs

- **MessageQueue** — 3 fields
- **QueryGuard** — 4 fields
- **QueuedCommand** — 8 fields: ID, Value, Mode, Priority, AgentID, CreatedAt, Origin, Content
- **Signal** — 3 fields

## Functions

- `NewCommand(value string, mode CommandMode, priority Priority) QueuedCommand`
- `NewCommandWithAgent(value string, mode CommandMode, priority Priority, agentID string) QueuedCommand`
- `NewMessageQueue() *MessageQueue`
- `NewQueryGuard() *QueryGuard`
- `NewSignal() *Signal`

## Constants

- `Dispatching`
- `Idle`
- `ModeExternalPrompt`
- `ModePrompt`
- `ModeSlashCommand`
- `OriginBTW`
- `PriorityLater`
- `PriorityNext`
- `PriorityNow`
- `Running`

## Change Impact

**Exported type references (files that use types from this package):**
- `MessageQueue` → `internal/bootstrap/wire.go`, `internal/engine/engine.go`, `internal/tui/init.go`, `internal/tui/model.go`
- `QueryGuard` → `internal/bootstrap/wire.go`, `internal/tui/init.go`, `internal/tui/model.go`
- `QueuedCommand` → `internal/engine/query.go`

## Dependencies

**Imports:** `pkg/types`

**Imported by:** `internal/bootstrap`, `internal/engine`, `internal/tui`

<!-- AUTO-GENERATED ABOVE — DO NOT EDIT -->
<!-- MANUAL NOTES BELOW — preserved across regeneration -->

## Design Notes

- **2026-11-18 内核搬回（反转 PR #31）**：本包原于 2026-09-23 逐字提取至外部仓 github.com/tunsuy/agtkeel；因该仓停止维护，经人类决策整併回本仓原始路径。来源：agtkeel main 3a77085＋后续提交，零逻辑改动（仅 import 路径恢复＋gofmt）。内核改动自此直接在本仓进行。
- **2026-11-18 EV-01**：ModeExternalPrompt——外部注入事件（MCP 通知）专用模式；与 ModePrompt 区别：display 訊息由 TUI drain 点附加（外部注入未经 handleSubmit）。
- **2026-11-20**：`QueuedCommand` +`Origin`（`OriginBTW`）。TUI 用它在主腦一輪結束時把沒被接走的 btw 備註取回，改成附加到下一則訊息，避免自動開新一輪。
- **2026-11-20 貼上快照**：`QueuedCommand.Content` 在 TUI 的 Update goroutine、入隊當下寫入，只給使用者自己的 ModePrompt（Origin 維持空字串）。nil 表示引擎把 Value 當成一個文字 block 送出；有值就是該則的引擎內容（圖片 block 再接一個文字 block）。Value 仍是畫面、歷史，以及回合之間 drain 用的標記文字。引擎 goroutine 不讀 paste store。`OriginBTW` 不寫這個欄位。沒有東西把 `QueuedCommand` 做 JSON，所以不加 json tag。
