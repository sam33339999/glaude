# TUI 文字選取與程式碼區塊複製 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 在 glaude TUI 訊息區用滑鼠拖曳／雙擊選取並複製文字，並在每個程式碼區塊上方加 `[copy]` 標題列整塊複製原文。

**Architecture:** 滑鼠模式維持 CellMotion。選取狀態存「內容座標」（viewport 內容的行、cell 欄），由新的 `selection.go` 處理座標換算、反白與取字；程式碼區塊由 `codeblock.go` 以 goldmark 抽出原文、在 glamour 輸出中定位並插入標題列，標題列位置（`codeBlockRef`）經 render cache 隨訊息輸出一起回傳。複製同時走 `tea.SetClipboard`（OSC 52）與 `internal/clipboard` 新增的原生 `WriteText`。

**Tech Stack:** Go、charm.land/bubbletea/v2 v2.1.1、bubbles/v2 viewport、github.com/charmbracelet/x/ansi、github.com/yuin/goldmark v1.7.13（由 indirect 改 direct）。

**Spec:** `docs/superpowers/specs/2026-11-22-tui-text-selection-design.md`

## Global Constraints

- 跑 Go 指令前不需切 Node；測試：`go test ./internal/tui/ ./internal/clipboard/`；收尾 `make all`、`make debt-check`（`TODO(dep)`／`not yet implemented` 只降不升）。
- 只處理訊息 viewport 的列；輸入框、狀態列、對話框、協調面板不可選。
- `Ctrl+C` 語意不變（loading 時中斷、否則離開）；滾輪捲動行為不變。
- 提示文字（英文，沿用 `pasteNotice` 一次性提示機制）：拖曳／雙擊複製 `Copied N characters`（N = rune 數）；整塊複製 `Copied code block (N lines)`。
- 雙擊判定：兩次左鍵 click 間隔 ≤ 400ms 且同一格。
- 標題列格式：`── <lang> ` + `─` 填充 + ` [copy] ──`，可見寬度 = `opts.width-4`（下限 20），次要色（`mutedStyle`）；無語言時為 `── ` + 填充 + ` [copy] ──`。
- 整塊複製內容 = 原文去掉結尾換行，不做其他清理。
- Layer isolation：`internal/tui` 可依賴 `internal/clipboard`，反之不行。

## Review Focus

1. **串流中選取**：回應持續追加、`pinnedToBottom` 時拖曳，反白不能跳行 → Task 4 測試 `TestSelection_StaysOnContentWhileAppending`。
2. **捲動後點擊座標**：使用者已往上捲（非 pinned）再拖曳，選到的必須是畫面上看到的那幾行 → Task 2 測試 `TestViewportGeometry_ScrolledUp`。
3. **CJK 寬字元端點**：從中文字的右半格開始拖曳，不可切出半個字或亂碼 → Task 2 測試 `TestExtractSelection_WideCharBoundary`。
4. **選取跨過標題列**：拖曳範圍包含 `── go ── [copy] ──`，複製結果不可含標題列 → Task 4 測試 `TestDragCopy_SkipsCodeHeaders`。
5. **未安裝 pbcopy/xclip**：原生寫入失敗不可報錯打斷，OSC 52 照送、提示照顯示 → Task 1 測試 `TestWriteText_MissingTool` ＋ Task 4 測試 `TestDragCopy_NativeWriteFailureStillNotifies`。

---

### Task 1: `internal/clipboard` 原生寫入 `WriteText`

**Files:**
- Modify: `internal/clipboard/clipboard.go`（加 `WriteText`、`Runner` 不動）
- Modify: `internal/clipboard/darwin.go`、`internal/clipboard/linux.go`
- Test: `internal/clipboard/clipboard_test.go`
- Modify: `internal/clipboard/CONTEXT.md`（新增 API 一行）

**Interfaces:**
- Produces: `func (r *Reader) WriteText(ctx context.Context, s string) error`
  - darwin：`pbcopy`；linux：`Getenv("WAYLAND_DISPLAY") != ""` 且 `LookPath("wl-copy")` 成功 → `wl-copy`，否則 `xclip -selection clipboard`；工具不存在回 `*MissingToolError`；其他平台回 `ErrUnsupported`。
  - 新增 `Reader.RunInput func(ctx context.Context, stdin string, name string, args ...string) error`（`New` 預設以 `exec.CommandContext` 把 stdin 寫入）；測試注入假實作。沿用 `runTimeout`。

- [ ] **Step 1: 寫失敗測試**
  - `TestWriteText_Darwin`：GOOS=darwin，假 `RunInput` 記錄 → 斷言 name=`pbcopy`、args 空、stdin=`"hi\n世界"`。
  - `TestWriteText_Wayland`：GOOS=linux、`WAYLAND_DISPLAY=wayland-0`、`lookPathOK("wl-copy")` → name=`wl-copy`。
  - `TestWriteText_X11`：無 WAYLAND_DISPLAY、`lookPathOK("xclip")` → name=`xclip`、args=`[-selection clipboard]`。
  - `TestWriteText_MissingTool`：linux、LookPath 全失敗 → `errors.As(err, &*MissingToolError)` 為真。
  - `TestWriteText_OtherPlatform`：GOOS=windows → `errors.Is(err, ErrUnsupported)`。
- [ ] **Step 2: 跑測試確認失敗** — `go test ./internal/clipboard/ -run WriteText` → FAIL（`WriteText` undefined）。
- [ ] **Step 3: 實作** `WriteText` 與 `RunInput`（`New()` 填預設）。
- [ ] **Step 4: 跑測試確認通過** — 同上指令 → PASS；`go test ./internal/clipboard/` 全綠。
- [ ] **Step 5: Commit** — `feat(clipboard): 新增 WriteText 原生寫入剪貼簿`

---

### Task 2: 選取模型、座標換算、取字與反白（純函式）

**Files:**
- Create: `internal/tui/selection.go`
- Test: `internal/tui/selection_test.go`
- Modify: `internal/tui/view.go`（抽出 `viewportGeometry`，`View()` 改用它）

**Interfaces:**
- Produces:
  ```go
  type contentPos struct{ line, col int }            // 內容行、cell 欄
  type selectionState struct {
      active   bool        // 有錨點（按下中或已完成）
      dragging bool        // 左鍵按住中
      anchor, cursor contentPos
  }
  func (s selectionState) ordered() (start, end contentPos) // start <= end；end 為含端點
  func (s selectionState) empty() bool                      // !active 或 anchor==cursor

  // 畫面幾何：top=訊息區第一列的螢幕 y；height=列數；offset=內容第一個可見行
  func (m AppModel) viewportGeometry() (top, height, offset int)
  func (m AppModel) screenToContent(x, y int) (contentPos, bool) // 落在訊息區外回 false

  func extractSelection(lines []string, start, end contentPos, skip map[int]bool) string
  func highlightLine(line string, from, to int, theme Theme) string // [from,to) cell 範圍反白
  func wordAt(line string, col int) (from, to int, ok bool)          // [from,to) cell
  ```
- `viewportGeometry`：`top` = `l.coordPanel` 的行數（空則 0）；`height = l.vpHeight`；`offset` = pinned 時 `max(0, TotalLineCount-height)`，否則 `m.viewport.YOffset()` 夾在 `[0, max(0,total-height)]`。`View()` 必須用同一個值渲染（以免點擊與畫面不一致）。
- `extractSelection` 演算法：對 `start.line..end.line` 每行（跳過 `skip` 中的行），`ansi.Strip` 後以 `ansi.Cut` 取 cell 範圍（首行從 `start.col`、末行到 `end.col+1`、中間整行）→ `strings.TrimRight(" \t")` → 計算所有非空行的最小開頭空白數並去掉 → `\n` 串接；整段 `TrimSpace` 為空時回 `""`。寬字元：起點落在寬字元右半格時往左對齊到字元起點，終點落在左半格時含整個字元。
- `wordAt`：字元類別 = (a) `unicode.IsLetter/IsDigit` 且非 CJK、或 `_`；(b) CJK（`unicode.Han/Hiragana/Katakana/Hangul`）。從 col 所在字元向兩側擴展同類別；空白或標點回 `ok=false`。

- [ ] **Step 1: 寫失敗測試**
  - `TestExtractSelection_TrimsTrailingAndDedents`：lines=`["  func a() {   ", "      x()  ", "  }"]`，start={0,0}、end={2,2} → `"func a() {\n    x()\n}"`。
  - `TestExtractSelection_PartialFirstLast`：lines=`["hello world", "foo bar"]`，start={0,6}、end={1,2} → `"world\nfoo"`。
  - `TestExtractSelection_StripsANSI`：line 為 `lipgloss` 上色的 `"abc"`，{0,0}–{0,2} → `"abc"`。
  - `TestExtractSelection_WideCharBoundary`：lines=`["生字測試"]`（每字 2 cell），start={0,1}、end={0,4} → `"生字測"`。
  - `TestExtractSelection_SkipsLines`：skip={1:true} 時第 1 行不出現。
  - `TestExtractSelection_BlankIsEmpty`：全空白 → `""`。
  - `TestWordAt_Latin`：`"call fooBar_2(x)"`，col=7 → 覆蓋 `fooBar_2`。
  - `TestWordAt_CJK`：`"這是生字，好"`，col 落在「生」→ 覆蓋 `這是生字`（到全形逗號前）。
  - `TestWordAt_Space`：col 落在空白 → ok=false。
  - `TestHighlightLine_PreservesOutside`：反白後 `ansi.Strip` 結果與原文相同、且輸出含 reverse SGR（`\x1b[7m`）。
  - `TestViewportGeometry_Pinned`：內容 50 行、vpHeight 10、pinned → offset=40；`screenToContent(3, top+2)` → {42,3}。
  - `TestViewportGeometry_ScrolledUp`：非 pinned、`viewport.SetYOffset(5)` → `screenToContent(0, top)` → {5,0}。
  - `TestScreenToContent_OutsideViewport`：y 落在輸入框列 → false。
- [ ] **Step 2: 跑測試確認失敗** — `go test ./internal/tui/ -run 'ExtractSelection|WordAt|HighlightLine|ViewportGeometry|ScreenToContent'` → FAIL（undefined）。
- [ ] **Step 3: 實作** 上述函式；`View()` 改用 `viewportGeometry()` 的 offset（`vp.SetYOffset(offset)`）。
- [ ] **Step 4: 跑測試確認通過** — 同上 → PASS；`go test ./internal/tui/` 全綠（確認 View 既有測試未壞）。
- [ ] **Step 5: Commit** — `feat(tui): 選取模型、座標換算與取字`

---

### Task 3: 程式碼區塊抽取、定位與標題列

**Files:**
- Create: `internal/tui/codeblock.go`
- Test: `internal/tui/codeblock_test.go`
- Modify: `internal/tui/messagelist.go`（`renderTextBlock`、`renderAssistantMessage`、`renderMessageOpts`、`renderMessageList`）
- Modify: `internal/tui/rendercache.go`（entry 存 refs）
- Modify: `go.mod`（goldmark 改 direct：`go mod tidy`）

**Interfaces:**
- Produces:
  ```go
  type codeBlock struct{ lang, code string }          // code 含原始換行
  type codeBlockRef struct {
      line             int    // 相對於所屬輸出字串的行號（0 起）
      colStart, colEnd int    // "[copy]" 的 cell 範圍 [colStart,colEnd)
      code             string // 原文，已去結尾換行
  }
  func extractCodeBlocks(md string) []codeBlock
  func codeHeader(lang string, width int, theme Theme) (header string, copyFrom, copyTo int) // copy 範圍相對 header 起點
  func insertCodeHeaders(rendered []string, blocks []codeBlock, width int, theme Theme) (out []string, headerAt map[int]codeBlock)

  func renderTextBlockRefs(text string, opts renderOptions, bullet string) (string, []codeBlockRef)
  func renderMessageRefs(msg types.Message, opts renderOptions, lookups MessageLookups) (string, []codeBlockRef)
  func renderMessageListRefs(messages []types.Message, opts renderOptions, cache *messageRenderCache) (string, []codeBlockRef) // refs 的 line 為整份清單輸出中的行號
  func (c *messageRenderCache) renderRefs(i int, msg types.Message, opts renderOptions, lookups MessageLookups) (string, []codeBlockRef)
  ```
  既有 `renderTextBlock`／`renderMessageOpts`／`renderMessageList`／`cache.render` 保留簽章，改為呼叫 `*Refs` 版並丟棄 refs（既有測試不動）。`cacheEntry` 加 `refs []codeBlockRef`。
- `extractCodeBlocks`：goldmark `Parser().Parse` 後 `ast.Walk`，收集 `*ast.FencedCodeBlock`（`Language(src)`）與 `*ast.CodeBlock`（lang=""），內容取 `Lines()` 各段串接。未閉合 fence 由 goldmark 自然延伸到文末，不另處理。
- `insertCodeHeaders` 演算法：游標 `i=0`；對每個 block，取其第一個非空行 `TrimSpace` 為 `key`（key 為空則略過該 block），從 `i` 往下找第一個 `strings.HasPrefix(strings.TrimSpace(ansi.Strip(rendered[j])), key)` 的 `j`；找到 → 在 `j` 前插入標題列、記 `headerAt[插入後的索引]=block`、`i=j+2`；找不到 → 略過、`i` 不動。
- `renderTextBlockRefs`：在既有 CJK 硬折行迴圈之前呼叫 `insertCodeHeaders(lines, extractCodeBlocks(text), opts.width-4, opts.theme)`；輸出迴圈中維護輸出行號，遇到 header 索引記 `codeBlockRef{line: outLine, colStart: 2+copyFrom, colEnd: 2+copyTo, code: strings.TrimRight(block.code, "\n")}`（2 = bullet＋空白或續行縮排）。標題列不參與 CJK 硬折行。
- 行號累加：`renderAssistantMessage` 以 `strings.Count(sb.String(), "\n")` 作為每個 text block 的基準；`renderMessageListRefs` 以寫入前的輸出行數作為每則訊息基準（含訊息間空行）。

- [ ] **Step 1: 寫失敗測試**
  - `TestExtractCodeBlocks_Multiple`：md 含 ```` ```go\nfunc a(){}\n``` ```` 與 ```` ```\nls -la\n``` ```` → 2 塊，lang `go`／``，code `"func a(){}\n"`／`"ls -la\n"`。
  - `TestExtractCodeBlocks_InList`：`"- step\n\n  ```sh\n  make\n  ```\n"` → 1 塊，code `"make\n"`。
  - `TestExtractCodeBlocks_Unclosed`：`"```py\nprint(1)\n"` → 1 塊，code `"print(1)\n"`。
  - `TestCodeHeader_Format`：`codeHeader("go", 30, theme)` 去色後 = `"── go ────────────── [copy] ──"`（寬 30），且 `stripped[copyFrom:copyTo]`（以 cell 計）= `"[copy]"`；`codeHeader("", 30, theme)` 以 `"── ─"` 開頭。
  - `TestInsertCodeHeaders_NotFoundSkipped`：第一塊找不到、第二塊找得到 → 只插一個標題列、位置在第二塊首行之前。
  - `TestRenderTextBlockRefs_RefPointsAtHeader`：`opts.width=60`，text=`"看這段：\n\n```go\nfmt.Println(1)\n```\n"` → 輸出第 `ref.line` 行去色後含 `[copy]`，且 `ansi.Cut(stripped, ref.colStart, ref.colEnd)=="[copy]"`，`ref.code=="fmt.Println(1)"`。
  - `TestRenderMessageListRefs_OffsetsAcrossMessages`：兩則 assistant 訊息各含一塊程式碼 → 兩個 ref 的 line 指向整份輸出中各自的標題列行。
  - `TestRenderCache_RefsSurviveCacheHit`：同一清單 render 兩次（第二次走 cache）→ refs 相同。
- [ ] **Step 2: 跑測試確認失敗** — `go test ./internal/tui/ -run 'CodeBlock|CodeHeader|InsertCodeHeaders|Refs'` → FAIL。
- [ ] **Step 3: 實作** 上述函式與 cache 調整；`go mod tidy` 讓 goldmark 成為 direct。
- [ ] **Step 4: 跑測試確認通過** — 同上 → PASS；`go test ./internal/tui/` 全綠（既有渲染快照若因標題列變動，逐一確認是預期的新增行再更新）。
- [ ] **Step 5: Commit** — `feat(tui): 程式碼區塊標題列與原文定位`

---

### Task 4: 滑鼠事件串接、反白顯示、複製與提示

**Files:**
- Modify: `internal/tui/model.go`（欄位：`sel selectionState`、`contentLines []string`、`codeRefs []codeBlockRef`、`lastClickAt time.Time`、`lastClickPos contentPos`、`clipWriter clipboardWriter`）
- Modify: `internal/tui/view.go`（`syncViewportContent` 存 `contentLines`／`codeRefs`，refs 行號加上 welcome header 行數；`View()` 對可見選取行套 `highlightLine`）
- Modify: `internal/tui/update.go`（分派 `MouseClickMsg`／`MouseMotionMsg`／`MouseReleaseMsg`）
- Modify: `internal/tui/keys.go`（`Esc` 清選取；`handleMouse` 旁新增滑鼠處理）
- Modify: `internal/tui/selection.go`（事件處理方法）
- Modify: `internal/tui/update.go` 寬度變更處（`tea.WindowSizeMsg`）清選取
- Test: `internal/tui/selection_events_test.go`
- Modify: `internal/tui/CONTEXT.md`（記錄選取／複製行為）

**Interfaces:**
- Consumes: Task 1 `WriteText`；Task 2 全部；Task 3 `renderMessageListRefs`、`codeBlockRef`。
- Produces:
  ```go
  type clipboardWriter interface{ WriteText(ctx context.Context, s string) error }
  func (m AppModel) handleMouseClick(msg tea.MouseClickMsg) (tea.Model, tea.Cmd)
  func (m AppModel) handleMouseMotion(msg tea.MouseMotionMsg) (tea.Model, tea.Cmd)
  func (m AppModel) handleMouseRelease(msg tea.MouseReleaseMsg) (tea.Model, tea.Cmd)
  func (m AppModel) copyText(s, notice string) (AppModel, tea.Cmd) // 設提示；回 tea.Batch(tea.SetClipboard(s), 原生寫入 cmd)
  ```
  `newAppModel` 以 `clipboard.New()` 設 `clipWriter`（與 `clip` 同一個 `*clipboard.Reader`）。原生寫入 cmd 忽略錯誤、回 `nil` msg。
- 行為（照 spec「事件」段）：
  - click 左鍵：先判 `[copy]`——`screenToContent` 的 line 等於某 ref.line 且 col ∈ `[colStart,colEnd)` → `copyText(ref.code, fmt.Sprintf("Copied code block (%d lines)", strings.Count(ref.code,"\n")+1))`，不建立選取。否則距上次 click ≤400ms 且同格 → `wordAt` 成功則設選取為該字並 `copyText`；否則在訊息區內設 `anchor=cursor=pos, active, dragging`；訊息區外 → 清選取、照舊（不攔截）。
  - motion（`dragging` 時）：y < top → 捲上 1 行（`scrollUp(1)`），游標夾在第一可見行 col 0；y ≥ top+height → `scrollDown(1)`，夾在最後可見行行尾；否則游標 = 換算位置。
  - release：`dragging=false`；`empty()` → `active=false`；否則 `extractSelection(contentLines, start, end, skip=refs 行號集合)`，非空則 `copyText(text, fmt.Sprintf("Copied %d characters", utf8.RuneCountInString(text)))`。
  - `Esc`（idle 且無對話框時，置於既有 Esc 處理之前、且只在 `sel.active` 時攔截）→ 清選取；其他情況 Esc 照舊。
  - 寬度改變 → 清選取。

- [ ] **Step 1: 寫失敗測試**（用 `newTestModel()`、設 `termWidth/termHeight`、塞訊息後 `syncViewportContent()`；`m.clipWriter = &fakeClipWriter{}` 記錄寫入）
  - `TestDragCopy_WritesSelection`：click(0,top)→motion(4,top)→release → fake 收到第 0 行前 5 格去色結果；`m.pasteNotice == "Copied 5 characters"`；回傳 cmd 非 nil。
  - `TestClick_NoDragNoCopy`：click→release 同格 → 未寫入、`sel.active==false`。
  - `TestDoubleClick_CopiesWord`：注入 `m.now`，兩次 click 間隔 300ms 同格（落在 `fooBar`）→ 寫入 `"fooBar"`；間隔 500ms → 不觸發。
  - `TestCopyButton_CopiesBlock`：assistant 訊息含 ```` ```go\nfmt.Println(1)\nfmt.Println(2)\n``` ```` → 點在 ref 的 `[copy]` 欄 → 寫入 `"fmt.Println(1)\nfmt.Println(2)"`、提示 `Copied code block (2 lines)`；點標題列 col 0 → 不寫入整塊。
  - `TestDragCopy_SkipsCodeHeaders`：拖曳範圍跨過標題列 → 寫入內容不含 `[copy]`。
  - `TestDragCopy_NativeWriteFailureStillNotifies`：fake 回錯 → 執行回傳的 cmd 不 panic、提示仍為 `Copied N characters`。
  - `TestSelection_StaysOnContentWhileAppending`：建立選取後追加一則訊息並 `syncViewportContent()`（pinned）→ `sel.anchor/cursor` 不變、extract 結果不變。
  - `TestDragPastBottom_Autoscrolls`：非 pinned、YOffset=0，dragging 時 motion y=top+height → `viewport.YOffset()` 增 1。
  - `TestEsc_ClearsSelectionOnly`：有選取時 Esc → `sel.active=false` 且輸入框內容不變；無選取時 Esc 走原路徑（既有 Esc 測試仍綠）。
  - `TestResize_ClearsSelection`：`tea.WindowSizeMsg{Width: 新寬}` 後 `sel.active=false`。
  - `TestClickOutsideViewport_PassesThrough`：click 在輸入框列 → 無選取、無寫入。
  - `TestWheelStillScrolls`：既有滾輪行為不變（`MouseWheelMsg` 上捲 3 行）。
- [ ] **Step 2: 跑測試確認失敗** — `go test ./internal/tui/ -run 'DragCopy|Click|DoubleClick|CopyButton|Selection_|DragPast|Esc_Clears|Resize_Clears|WheelStill'` → FAIL。
- [ ] **Step 3: 實作** 欄位、事件分派、`copyText`、`View()` 反白（只對 `[offset, offset+height)` 內的選取行呼叫 `highlightLine`，首末行用選取欄位、中間行整行）。
- [ ] **Step 4: 跑測試確認通過** — 同上 → PASS；接著 `go test ./internal/tui/ ./internal/clipboard/` 全綠。
- [ ] **Step 5: 全量檢查** — `make all` → vet/test/build 皆通過；`make debt-check` → 不超過基線。
- [ ] **Step 6: 實機驗證** — `make build && ./bin/glaude`，請 AI 回一段含 go 程式碼的回答：拖曳選兩行貼到別處確認無行尾空白；雙擊中文詞確認選整段 CJK；點 `[copy]` 確認整塊原文；滾輪仍可捲；在 Terminal.app 與 iTerm2 各試一次。
- [ ] **Step 7: Commit** — `feat(tui): 滑鼠拖曳／雙擊選取複製與程式碼區塊 [copy]`
