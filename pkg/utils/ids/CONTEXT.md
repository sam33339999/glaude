---
package: ids
import_path: pkg/utils/ids
layer: types
generated_at: 2026-11-25T16:40:37Z
source_files: [ids.go]
---

# pkg/utils/ids

> Layer: **Types (zero-dep)** · Files: 1 · Interfaces: 0 · Structs: 0 · Functions: 2

## Functions

- `NewAgentId(prefix string) types.AgentId`
- `NewSessionId() types.SessionId`

## Dependencies

**Imports:** `pkg/types`

**Imported by:** `internal/coordinator`, `internal/session`, `internal/tools/interact`

<!-- AUTO-GENERATED ABOVE — DO NOT EDIT -->
<!-- MANUAL NOTES BELOW — preserved across regeneration -->

## Design Notes

- **2026-11-18 内核搬回（反转 PR #31）**：本包原于 2026-09-23 逐字提取至外部仓 github.com/tunsuy/agtkeel；因该仓停止维护，经人类决策整併回本仓原始路径。来源：agtkeel main 3a77085＋后续提交，零逻辑改动（仅 import 路径恢复＋gofmt）。内核改动自此直接在本仓进行。
