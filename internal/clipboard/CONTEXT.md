---
package: clipboard
import_path: internal/clipboard
layer: unknown
generated_at: 2026-11-22T08:07:32Z
source_files: [clipboard.go, darwin.go, image.go, linux.go]
---

# internal/clipboard

> Layer: **unknown** · Files: 4 · Interfaces: 0 · Structs: 2 · Functions: 2

## Structs

- **MissingToolError** — 1 fields: Hint
- **Reader** — 5 fields: Run, LookPath, Getenv, GOOS, TempDir

## Function Types

- `Runner` — `func(ctx context.Context, name string, args ...string) ([]byte, error)`

## Functions

- `New() *Reader`
- `NormalizeImage(data []byte) ([]byte, string, error)`

## Constants

- `MaxImageBytes`
- `MaxImageEdge`

## Change Impact

**Exported type references (files that use types from this package):**
- `MissingToolError` → `internal/tui/clipboard_paste.go`, `internal/tui/clipboard_paste_test.go` (test)

## Dependencies

**Imports:** *(none — zero-dependency)*

**Imported by:** `internal/tui`

<!-- AUTO-GENERATED ABOVE — DO NOT EDIT -->
<!-- MANUAL NOTES BELOW — preserved across regeneration -->

## Design Notes

- **2026-11-20 剪貼簿讀取**：`Reader` 的 `Run`、`LookPath`、`Getenv`、`GOOS`、`TempDir` 可注入。`run` 對每次命令套 5 秒逾時。沒有 build tags；darwin 與 linux 都編進同一套件，其他平台 `ReadImage` 回 `ErrUnsupported`。macOS 用 AppleScript 把剪貼簿 PNG 寫進暫存檔，路徑經 `escapeAppleScriptString`（反斜線先跳脫，避免剛寫入的 `\"` 再被跳一次）。不使用 CGO。
- **NormalizeImage**：長邊與位元組都在上限內的 PNG、JPEG、GIF、WebP 原樣通過，因此未超限的動畫 GIF 保持原檔。其餘先解碼；長邊超過 1568 時用 CatmullRom 縮小，再編成 PNG，PNG 仍超過 3.75 MB 就改 JPEG quality 85，還超過則 `ErrImageTooLarge`。TIFF 與 BMP 一律重新編碼。超過上限的 GIF 只保留第一幀（`image.Decode`）。
