---
package: codemode
import_path: internal/codemode
layer: unknown
generated_at: 2026-11-22T14:16:46Z
source_files: [catalog.go, hooks.go, moejs.go, runtime.go]
---

# internal/codemode

> Layer: **unknown** · Files: 4 · Interfaces: 1 · Structs: 4 · Functions: 5

## Interfaces

### Runtime (1 methods)
> Runtime executes model-written code in a sandbox. Implementations must be

```go
type Runtime interface {
    Execute(ctx context.Context, code string, call CallFunc, opts Options) (*Result, error)
}
```

## Structs

- **CatalogEntry** — 3 fields: Name, Description, InputSchema
- **MoejsRuntime**
- **Options** — 13 fields: Timeout, MemoryLimit, Search, Describe, Typescript, ImportModule, History, Replay, ...
- **Result** — 2 fields: Value, Logs

## Function Types

- `CallFunc` — `func(ctx context.Context, name string, input json.RawMessage) (json.RawMessage, error)`

## Functions

- `BuildCatalog(reg *tools.Registry) []CatalogEntry`
- `CatalogText(entries []CatalogEntry) string`
- `Describe(entries []CatalogEntry, name string) (CatalogEntry, bool)`
- `NewMoejsRuntime() *MoejsRuntime`
- `Search(entries []CatalogEntry, query string) []string`

## Constants

- `DefaultMemoryLimit`
- `DefaultTimeout`

## Change Impact

**Exported type references (files that use types from this package):**
- `CallFunc` → `internal/tools/codemode/bridge.go`
- `CatalogEntry` → `internal/tools/codemode/tool.go`
- `Options` → `internal/tools/codemode/replay.go`
- `Result` → `internal/tools/codemode/replay.go`, `internal/tools/codemode/tool.go`
- `Runtime` → `internal/tools/codemode/tool.go`

## Dependencies

**Imports:** `internal/tools`

**Imported by:** `internal/bootstrap`, `internal/tools/codemode`

<!-- AUTO-GENERATED ABOVE — DO NOT EDIT -->
<!-- MANUAL NOTES BELOW — preserved across regeneration -->

## Design Notes

- **2026-11-18 CM-01 Code Mode 引擎**：moejs v0.1.1（纯 Go、alpha 钉版）；Runtime 介面隔离引擎（goja fallback 预留）。沙箱：DisableDynamicCode、MemoryLimit 64MiB、Timeout 120s、ctx/AbortCh→Interrupt；唯一对外通道 CallFunc bridge（权限管线在 tools/codemode 侧）。代码契约＝async 函数 body，.then 捕获 return/rejection（Error.message 字串化）。tools.* 经 Proxy 动态分发；__call 回传 promise＋跨 goroutine settle＝真并行（实测 3×60ms≈60ms）。catalog/search/describe 为 progressive discovery（构造期快照，会话钉住）。
- **2026-11-18 CM-02 v2**：Options.Typescript（CompileTS＋rt.Load 路径）；Options.ImportModule（moejs.Importer.Resolve→host resolver→Compile，沙箱 import 受控）；Options.History（__history native＋codemode.history）。引擎仍无状态，每次 Execute 新建隔离 runtime。
- **2026-11-18 CM-03 v3**：Options +Replay/Run/Snippets/Audit/Approvals/Revoke 六个可选 hook；hooks.go installStringHook/installNullaryHook（nil→"not available" 错误，绝不静默）。prelude codemode.* 对应方法。巢状 replay/run 阻塞外层 loop（同步语义），外层 timeout/ctx 取消仍适用。
