# `/model` 選單列出服務商模型 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** `/model` 選單在等級之後依服務商分段列出 `[providers.<id>.models]` 的模型，可捲動，選了照現有流程切換主腦。

**Architecture:** `modelSwitcher.Options()`（bootstrap）追加帶 `Group` 的服務商模型選項，沿用 `router.client` 的可用性判斷；`modelPicker`（tui）依 `Group` 畫分段並捲動；確認框與通知在 `Name == ""` 時只顯示 `Label()`。

**Tech Stack:** Go、Bubble Tea v2。

**Spec:** `docs/superpowers/specs/2026-11-23-model-picker-provider-models-design.md`

## Global Constraints

- 驗證：`make all`、`make docs-check`、`make debt-check`。
- 不連網：選項只來自 `config.toml` 的 models 表。
- `main` 不產生服務商分段。
- `/model <參考>` 打字用法行為不變（既有 `TestModelPicker_TypedRef` 等不改斷言）。
- commit message：`feat(scope): 中文描述`，結尾 `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`。

## Review Focus

1. **游標初始位置落在不可選列**（例如目前主腦在某服務商、但第一列是不可選的分段提示）→ 游標停在 current 列，不停在不可選列。測試加在 Task 2 `TestModelPicker_CursorStartsOnCurrentProviderModel`。
2. **同一個 `provider:model` 同時出現在等級列和服務商分段** → 兩列都標 current，選哪一列結果相同。測試加在 Task 1 `TestModelSwitcher_ProviderModelCurrentAlsoOnTier`。
3. **選單比畫面高、游標移到最後一列** → 最後一列可見、上方顯示 `↑ N more`。測試加在 Task 2 `TestModelPicker_ScrollsToCursor`。
4. **服務商在 `/provider` 剛新增**（啟動後才有 client）→ 不重開即出現在選單且可選。測試加在 Task 1 `TestModelSwitcher_ProviderAddedAtRuntimeListed`。
5. **手打 `/model id:m`** → 通知顯示 `Main model: id:m (session only).`，不出現 "custom"。測試加在 Task 2 `TestModelPicker_ProviderModelNotice`。

---

### Task 1: `Options()` 追加服務商模型

**Files:**
- Modify: `internal/commands/modelswitch.go`（`ModelOption.Group string`）、`internal/bootstrap/modelswitch.go`（`Options`）
- Test: `internal/bootstrap/modelswitch_test.go`

**Interfaces:**
- Produces: `commands.ModelOption.Group string` — 服務商 id；`default` 與等級為 `""`。
- `func (s *modelSwitcher) providerOptions() []commands.ModelOption`（`Options` 在等級之後 append；`s.router == nil` 回 nil）。
- 每個有模型的服務商：依模型 ID 排序，每個模型 `{Group: id, Provider: id, Model: m, ContextWindow, MaxTokens}`，限額由 `s.router.store.Resolve(id+":"+m)` 解析，`MaxTokens == 0` 時用 `s.startup.MaxTokens`；可用性：`s.router.client(id)` 錯誤 → `Available=false, Reason=err.Error()`。
- 沒有模型的服務商：一列 `{Group: id, Reason: "no models listed — check some with /provider"}`。
- `Current` 由 `Options` 末尾既有迴圈統一設定。

- [ ] **Step 1: Write the failing tests**

```go
// config: zai (env key set) with models {"glm-5.3-flash": {context_window=131072}, "glm-x": {}}; openrouter (env key set) without models.
func TestModelSwitcher_ProviderModelsListed(t *testing.T)
	// opts[4] = openrouter placeholder (Group "openrouter", Model "", !Available, Reason contains "/provider")
	// opts[5] = zai:glm-5.3-flash (Group "zai", Available, ContextWindow 131072); opts[6] = zai:glm-x
func TestModelSwitcher_ProviderModelsUnavailableWithoutKey(t *testing.T) // ZAI_API_KEY="" → zai rows !Available, Reason contains "ZAI_API_KEY"
func TestModelSwitcher_ProviderModelCurrentAlsoOnTier(t *testing.T)     // Apply lite (zai:glm-5.3-flash) → lite row and zai:glm-5.3-flash row both Current   ← Review Focus 2
func TestModelSwitcher_ProviderAddedAtRuntimeListed(t *testing.T)       // UpsertProvider + WriteCredential + router.upsert → new rows appear, Available   ← Review Focus 4
```

更新既有 `TestModelSwitcher_Options` 的 `len(opts) != 4` 為「前 4 列是 default/lite/pro/max」（`switchTestConfig` 的兩家服務商沒有 models 表，會多出兩列提示）。

- [ ] **Step 2: Run** `go test ./internal/bootstrap/ -run ModelSwitcher` — Expected: FAIL（`Group` undefined）
- [ ] **Step 3: Implement** 依 Interfaces。
- [ ] **Step 4: Run** `go test -race ./internal/bootstrap/ ./internal/commands/` — Expected: PASS
- [ ] **Step 5: Commit** `feat(bootstrap): /model 選項列出服務商 models 表的模型`

---

### Task 2: 選單分段、捲動與名稱顯示

**Files:**
- Modify: `internal/tui/modelpicker.go`（`view(width, maxHeight int, theme Theme)`、分段、捲動、`applyModelSwitch` 通知、`renderModelConfirm` 標題）、`internal/tui/view.go:215`（傳 `m.resumePickerMaxHeight()`）
- Test: `internal/tui/modelpicker_test.go`

**Interfaces:**
- Consumes: Task 1 `ModelOption.Group`
- Produces: `func (p *modelPicker) view(width, maxHeight int, theme Theme) string`；`func optionTitle(o commands.ModelOption) string` — `Name != ""` 回 `Name + " (" + Label() + ")"`，否則回 `Label()`。

渲染規則：
- 逐列輸出；某列 `Group != ""` 且與上一列 `Group` 不同時，先輸出一列 `mutedStyle("── <Group> ──")`。
- 分組列的文字：`Name == ""` 時 `fmt.Sprintf("  %s", o.Label())`；`Model == ""`（提示列）時只顯示 `Reason`（灰色）。
- 可見列數 `rows = max(maxHeight-6, 3)`（分段標題算一列）。以游標列為準捲動，被截掉時上下各一列 `↑ N more` / `↓ N more`（N 為被隱藏的選項數，不含標題）。
- 通知：`Name != ""` → `Main model: <Name> → <Label> (session only).`（現狀不變）；`Name == ""` → `Main model: <Label> (session only).`。
- 確認框標題：`Switch main model to <optionTitle(opt)>?`

- [ ] **Step 1: Write the failing tests**

```go
func TestModelPicker_ProviderGroupsRendered(t *testing.T)          // view 含 "── zai ──"、"zai:glm-x"；提示列含 "/provider"；游標 down 跳過提示列
func TestModelPicker_CursorStartsOnCurrentProviderModel(t *testing.T) // Current 在分組列 → newModelPicker cursor 指向它   ← Review Focus 1
func TestModelPicker_ScrollsToCursor(t *testing.T)                 // 30 個分組選項、maxHeight 15：移到最後 → view 含最後一個模型與 "↑"，不含第一個分組模型   ← Review Focus 3
func TestModelPicker_ProviderModelNotice(t *testing.T)             // runSlash "/model zai:glm-x"（空對話、fake switcher）→ lastText == "Main model: zai:glm-x (session only)."   ← Review Focus 5
func TestModelPicker_ConfirmTitleWithoutName(t *testing.T)         // 有歷史時選分組列 → renderModelConfirm 含 "Switch main model to zai:glm-x?"，不含 "()"
```

（`modelpicker_test.go` 既有的 fake switcher 若不支援自訂 Options，擴充它的欄位即可。）

- [ ] **Step 2: Run** `go test ./internal/tui/ -run ModelPicker` — Expected: FAIL
- [ ] **Step 3: Implement** 依上述規則。
- [ ] **Step 4: Run** `go test ./internal/tui/` — Expected: PASS（既有 ModelPicker 測試不改斷言）
- [ ] **Step 5: Commit** `feat(tui): /model 選單依服務商分段並可捲動`

---

### Task 3: 文件與收尾

**Files:**
- Modify: `docs/config-toml.md`（「在會話中切換主腦（`/model`）」第一段補：選單也會依服務商列出 `[providers.<id>.models]` 的模型；沒有模型的服務商提示用 `/provider` 勾選）、`docs/provider.md`（完成頁一段補「也可以直接在 `/model` 選單挑」）、`docs/superpowers/specs/2026-11-20-model-switch-design.md`（非目標「從 provider 列出所有模型讓人挑」改為「連網列舉 provider 的全部模型（選單只列 models 表，見 2026-11-23-model-picker-provider-models-design.md）」）、spec 狀態改為已實作、`CONTEXT.md`（`make docs`）

- [ ] **Step 1:** 改文件。
- [ ] **Step 2: Run** `make docs && make all && make docs-check && make debt-check` — Expected: 全部成功
- [ ] **Step 3: Commit** `docs: /model 選單列出服務商模型`

**分支審查**：完成後開一個 reviewer subagent 審 `git diff dev...HEAD`。
