---
package: soul
import_path: internal/soul
layer: unknown
generated_at: 2026-11-25T16:03:34Z
source_files: [soul.go]
---

# internal/soul

> Layer: **unknown** · Files: 1 · Interfaces: 0 · Structs: 1 · Functions: 7

## Structs

- **Source** — 3 fields: Scope, Path, Text

## Functions

- `Clear(path string) (ok bool, err error)`
- `GlobalPath(home string) string`
- `ProjectPath(projectDir string) string`
- `Prompt(home string, projectDir string) string`
- `PromptForCwd(projectDir string) string`
- `Save(path string, text string) error`
- `Sources(home string, projectDir string) []Source`

## Constants

- `FileName`
- `MaxBytes`

## Change Impact

**Exported type references (files that use types from this package):**
- `Source` → `internal/commands/soul.go`

## Dependencies

**Imports:** `internal/textutil`

**Imported by:** `internal/bootstrap`, `internal/commands`, `internal/tui`

<!-- AUTO-GENERATED ABOVE — DO NOT EDIT -->
<!-- MANUAL NOTES BELOW — preserved across regeneration -->

## Design Notes

- **2026-11-21 常駐靈魂設定（`/set-soul`、`/soul`）**：讀取全域 `~/.glaude/SOUL.md` 與專案 `.glaude/SOUL.md`，組裝進 system prompt tail，使其不受對話歷史自動壓縮影響。
