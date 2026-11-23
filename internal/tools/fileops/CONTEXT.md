---
package: fileops
import_path: internal/tools/fileops
layer: tools
generated_at: 2026-11-23T14:09:22Z
source_files: [dangerous.go, doc.go, fileedit.go, fileread.go, filewrite.go, glob.go, grep.go, helpers.go, notebookedit.go, write_atomic.go]
---

# internal/tools/fileops

> Layer: **Tools** · Files: 10 · Interfaces: 0 · Structs: 10 · Functions: 0

## Structs

- **FileEditInput** — 3 fields: FilePath, OldString, NewString
- **FileReadInput** — 3 fields: FilePath, Offset, Limit
- **FileReadOutput** — 8 fields: Type, FilePath, Content, NumLines, StartLine, TotalLines, Base64, MediaType
- **FileWriteInput** — 2 fields: FilePath, Content
- **GlobInput** — 2 fields: Pattern, Path
- **GlobOutput** — 3 fields: Filenames, NumFiles, Truncated
- **GrepInput** — 5 fields: Pattern, Path, Include, OutputMode, MaxResults
- **GrepMatch** — 3 fields: Path, Line, Content
- **GrepOutput** — 5 fields: Matches, Files, Counts, NumResults, Truncated
- **NotebookEditInput** — 5 fields: NotebookPath, CellNumber, NewSource, CellType, EditMode

## Dependencies

**Imports:** `internal/tools`

**Imported by:** `internal/bootstrap`

<!-- AUTO-GENERATED ABOVE — DO NOT EDIT -->
<!-- MANUAL NOTES BELOW — preserved across regeneration -->

## Design Notes

- **2026-09-23 基座换库（agtkeel import-swap）**：可复用 agent 内核已逐字提取到 `github.com/tunsuy/agtkeel`（MIT），本包 import 随之改写：`internal/{api,compact,msgqueue,engine,session,permissions,hooks,mcp,config}` → `agtkeel/*`、`internal/tools`（接口契约+注册表）→ `agtkeel/tools`、`pkg/types` → `agtkeel/types`、`pkg/utils/{fs,ids}` → `agtkeel/utils/*`。本包代码**仅 import 路径重写，零逻辑改动**，`go build`/`go vet`/`go test -race ./...` 全绿。已知表现局限：docgen 依赖图按本模块前缀分类，agtkeel 视作第三方库，故上方 Imports 栏不再展示内核依赖（与 cobra 等一致）。
- **2026-11-18 BF-11 NotebookEdit 实现**：.ipynb loose-JSON round trip（map[string]json.RawMessage 保留未知字段）；replace/insert/delete；source string|[]string 双格式输入、[]string 行输出；markdown 转换清 outputs/execution_count；insert 需 cell_type；绝对路径＋.ipynb 扩展名校验；writeFileAtomic 原子写。CheckPermissions/GetPath 既有真实逻辑保留。
- **2026-11-18 WorkingDir 路徑解析（feat/agent-cwd）**：expandPathFrom(raw, workDir) 新增（expandPath 保留為 process-CWD fallback 版，供無 ctx 的 GetPath/PathTool 用）；Read/Write/Edit/NotebookEdit Call 與 Glob/Grep 預設目錄改讀 uctx.WorkingDir。Glob 預設 baseDir、Grep 預設 root 不再硬用 os.Getwd()。守護測試 cwd_test.go（相對路徑寫讀＋glob＋grep 全落 WorkingDir）。
- **2026-11-20 Read 讀圖的限制（feat/tool-result-images）**：圖片現在會真的以 image 送給模型（見 engine Design Notes），所以補上 API 的限制：原始檔超過 `maxImageBytes`（3.75 MB，對應 Anthropic base64 5 MB 上限）回錯誤並建議先縮圖；只送 API 接受的 PNG／JPEG／GIF／WebP（`apiImageExtensions`），`.bmp`／`.ico` 回錯誤；`.svg` 改以文字讀取（它是 XML，image API 不收）。避免不合法的圖片觸發 400，被 engine 誤判成「模型不支援視覺」。
- **2026-11-20 圖片 media type 依內容判斷**：`imageMediaType` 先用 `http.DetectContentType` 看檔案內容（png/jpeg/gif/webp），認不出來才退回副檔名。截圖工具常把 JPEG 存成 `.png`，標錯 media type 會被 API 以 400 拒絕。守護測試 TestFileReadTool_Call_ImageMediaTypeSniffed。
