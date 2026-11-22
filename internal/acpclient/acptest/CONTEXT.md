---
package: acptest
import_path: internal/acpclient/acptest
layer: services
generated_at: 2026-11-22T08:07:32Z
source_files: [agent.go]
---

# internal/acpclient/acptest

> Layer: **Services** · Files: 1 · Interfaces: 0 · Structs: 1 · Functions: 2

## Structs

- **Agent** — 8 fields

## Functions

- `New() *Agent`
- `Pipe(ctx context.Context) (agent *Agent, stdin io.WriteCloser, stdout io.Reader, done chan any, kill func())`

## Dependencies

**Imports:** *(none — zero-dependency)*

<!-- AUTO-GENERATED ABOVE — DO NOT EDIT -->
<!-- MANUAL NOTES BELOW — preserved across regeneration -->

## Design Notes

- **2026-11-21 可腳本化假 ACP agent**：獨立子包只依賴 `acp-go-sdk`，供 `internal/acpclient` 與 `internal/bootstrap` 測試共用而不產生循環 import；以 `gatedReader` 迴避 SDK agent 端建構時的 `asc.conn` 賦值競態。
