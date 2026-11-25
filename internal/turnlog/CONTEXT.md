---
package: turnlog
import_path: internal/turnlog
layer: infra
generated_at: 2026-11-25T16:40:37Z
source_files: [turnlog.go]
---

# internal/turnlog

> Layer: **Infra** · Files: 1 · Interfaces: 0 · Structs: 1 · Functions: 2

## Structs

- **Record** — 14 fields: Start, End, DurationMs, Outcome, QuerySource, SessionID, Cwd, Model, ...

## Functions

- `Append(path string, rec Record) error`
- `DefaultPath() string`

## Constants

- `QuerySourceForeground`

## Change Impact

**Exported type references (files that use types from this package):**
- `Record` → `internal/bootstrap/wire.go`, `internal/tui/turntiming.go`

## Dependencies

**Imports:** *(none — zero-dependency)*

**Imported by:** `internal/bootstrap`, `internal/tui`

<!-- AUTO-GENERATED ABOVE — DO NOT EDIT -->
<!-- MANUAL NOTES BELOW — preserved across regeneration -->
