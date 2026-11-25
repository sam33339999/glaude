---
package: textutil
import_path: internal/textutil
layer: infra
generated_at: 2026-11-25T16:40:37Z
source_files: [truncate.go]
---

# internal/textutil

> Layer: **Infra** · Files: 1 · Interfaces: 0 · Structs: 0 · Functions: 5

## Functions

- `TailBytes(s string, n int) string`
- `TruncateBytes(s string, n int) string`
- `TruncateLeftWidth(s string, maxWidth int) string`
- `TruncateWidth(s string, maxWidth int) string`
- `TruncateWidthTail(s string, maxWidth int, tail string) string`

## Constants

- `Ellipsis`

## Dependencies

**Imports:** *(none — zero-dependency)*

**Imported by:** `internal/api`, `internal/bootstrap`, `internal/coordinator`, `internal/memdir`, `internal/soul`, `internal/tools/agent`, `internal/tools/codemode`, `internal/tools/memory`, `internal/tools/misc`, `internal/tools/shell`, `internal/tools/tasks`, `internal/tui`

<!-- AUTO-GENERATED ABOVE — DO NOT EDIT -->
<!-- MANUAL NOTES BELOW — preserved across regeneration -->
