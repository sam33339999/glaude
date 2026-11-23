---
package: transcript
import_path: internal/transcript
layer: unknown
generated_at: 2026-11-23T14:09:22Z
source_files: [recorder.go]
---

# internal/transcript

> Layer: **unknown** · Files: 1 · Interfaces: 0 · Structs: 1 · Functions: 2

## Structs

- **Recorder** — 6 fields

## Functions

- `New(cwd string, version string) (*Recorder, error)`
- `Resume(cwd string, sessionID string, version string, existing int) (*Recorder, error)`

## Change Impact

**Exported type references (files that use types from this package):**
- `Recorder` → `internal/bootstrap/session.go`, `internal/tui/cmds.go`, `internal/tui/init.go`, `internal/tui/model.go`

## Dependencies

**Imports:** `internal/session`, `pkg/types`

**Imported by:** `internal/bootstrap`, `internal/tui`

<!-- AUTO-GENERATED ABOVE — DO NOT EDIT -->
<!-- MANUAL NOTES BELOW — preserved across regeneration -->

## Design Notes

- **2026-11-18 BF-18 会话持久化接线（新包）**：产品侧决定 WHEN 写（turn 边界由 TUI/headless 驱动），agtkeel session 只读复用（存储与生命周期）。写入基线 persisted 防重复：Record 只追加 msgs[persisted:]；失败停在失败条以便重试；历史收缩（/clear）重置基线但文件追加-only（v1 局限，不新建会话文件）。entry 形状＝type 信封＋types.SerializedMessage 嵌入，与既有读者（bootstrap/session.go、commands/resume.go）round-trip 相容（有测试钉住）。nil *Recorder 全方法安全 no-op（测试/持久化降级）。SwitchSession 支持 /resume 中途换文件（关旧开新＋基线）。mutex 保护（tea.Cmd goroutine 并发调用，-race 测试覆盖）。
