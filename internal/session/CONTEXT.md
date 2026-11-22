---
package: session
import_path: internal/session
layer: infra
generated_at: 2026-11-22T14:16:46Z
source_files: [store.go]
---

# internal/session

> Layer: **Infra** · Files: 1 · Interfaces: 1 · Structs: 2 · Functions: 3

## Interfaces

### SessionStorer (3 methods)
> SessionStorer is the interface for reading and writing session entries.

```go
type SessionStorer interface {
    AppendEntry(entry any) error
    ReadAll() ([]types.EntryEnvelope, error)
    Close() error
}
```

## Structs

- **SessionManager** — 3 fields: SessionId
- **SessionStore** — 3 fields

## Functions

- `New(projectDir string) (types.SessionId, *SessionManager, error)`
- `OpenSessionStore(path string) (*SessionStore, error)`
- `Resume(sessionIDStr string, projectDir string) (types.SessionId, *SessionManager, error)`

## Change Impact

**Exported type references (files that use types from this package):**
- `SessionManager` → `internal/bootstrap/session.go`
- `SessionStore` → `internal/transcript/recorder.go`
- `SessionStorer` → `internal/transcript/recorder.go`

## Dependencies

**Imports:** `pkg/types`, `pkg/utils/fs`, `pkg/utils/ids`

**Imported by:** `internal/bootstrap`, `internal/commands`, `internal/transcript`

<!-- AUTO-GENERATED ABOVE — DO NOT EDIT -->
<!-- MANUAL NOTES BELOW — preserved across regeneration -->

## Design Notes

- **2026-11-18 内核搬回（反转 PR #31）**：本包原于 2026-09-23 逐字提取至外部仓 github.com/tunsuy/agtkeel；因该仓停止维护，经人类决策整併回本仓原始路径。来源：agtkeel main 3a77085＋后续提交，零逻辑改动（仅 import 路径恢复＋gofmt）。内核改动自此直接在本仓进行。
- **产品侧消费**：internal/transcript Recorder（BF-18）在 turn 边界经 SessionStorer 落盘；entry 形状（type 信封＋SerializedMessage）与 bootstrap/commands 读者 round-trip 相容。
