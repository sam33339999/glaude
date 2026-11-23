---
package: fs
import_path: pkg/utils/fs
layer: types
generated_at: 2026-11-23T08:02:40Z
source_files: [fs.go]
---

# pkg/utils/fs

> Layer: **Types (zero-dep)** · Files: 1 · Interfaces: 0 · Structs: 0 · Functions: 4

## Functions

- `AtomicWriteFile(path string, data []byte, perm os.FileMode) error`
- `EnsureDir(path string) error`
- `ProjectHash(projectRoot string) (string, error)`
- `SafeReadFile(path string) ([]byte, error)`

## Dependencies

**Imports:** *(none — zero-dependency)*

**Imported by:** `internal/bootstrap`, `internal/commands`, `internal/session`

<!-- AUTO-GENERATED ABOVE — DO NOT EDIT -->
<!-- MANUAL NOTES BELOW — preserved across regeneration -->

## Design Notes

- **2026-11-18 内核搬回（反转 PR #31）**：本包原于 2026-09-23 逐字提取至外部仓 github.com/tunsuy/agtkeel；因该仓停止维护，经人类决策整併回本仓原始路径。来源：agtkeel main 3a77085＋后续提交，零逻辑改动（仅 import 路径恢复＋gofmt）。内核改动自此直接在本仓进行。
