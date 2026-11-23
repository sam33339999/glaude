---
package: hooks
import_path: internal/hooks
layer: infra
generated_at: 2026-11-23T14:09:22Z
source_files: [hooks.go]
---

# internal/hooks

> Layer: **Infra** · Files: 1 · Interfaces: 0 · Structs: 1 · Functions: 1

## Structs

- **Dispatcher** — 2 fields

## Functions

- `NewDispatcher(hooks map[types.HookType][]types.HookDefinition, disabled bool) *Dispatcher`

## Change Impact

**Exported type references (files that use types from this package):**
- `Dispatcher` → `internal/permissions/checker.go`

## Dependencies

**Imports:** `pkg/types`

**Imported by:** `internal/bootstrap`, `internal/permissions`

<!-- AUTO-GENERATED ABOVE — DO NOT EDIT -->
<!-- MANUAL NOTES BELOW — preserved across regeneration -->

## Design Notes

- **2026-11-18 内核搬回（反转 PR #31）**：本包原于 2026-09-23 逐字提取至外部仓 github.com/tunsuy/agtkeel；因该仓停止维护，经人类决策整併回本仓原始路径。来源：agtkeel main 3a77085＋后续提交，零逻辑改动（仅 import 路径恢复＋gofmt）。内核改动自此直接在本仓进行。
