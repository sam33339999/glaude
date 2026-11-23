# `/model` 選單列出服務商模型 設計（短版）

- 日期：2026-11-23
- 狀態：已核准並實作（2026-11-23）
- 分支：`feat/model-picker-provider-models`（從 `dev` b1c3299 分出）
- 相關：`docs/superpowers/specs/2026-11-20-model-switch-design.md`（`/model`）、`docs/superpowers/specs/2026-11-23-provider-command-design.md`（`/provider`）

## 意圖

`/provider` 會把勾選的模型寫進 `[providers.<id>.models]`，但 `/model` 選單只列 `default` 和 lite / pro / max，服務商的模型要先綁到等級，或手打 `/model <id>:<模型>` 才能用。使用者要在選單裡直接挑服務商的模型。

## 目標 / 非目標 / 反目標

**目標**
- `/model` 選單在等級之後，依服務商 id 排序分段（`── <id> ──`），列出該服務商 `models` 表的每個模型（`<id>:<模型>` ＋上下文長度）。
- 選了之後走現有流程：已有對話時先確認快取失效，再切換。
- 選單可捲動：超過可用高度時只畫游標附近的一段，上下顯示 `↑ N more` / `↓ N more`。

**非目標**
- 打字過濾。
- 連網列舉主腦或服務商的全部模型（挑新模型用 `/provider`）。
- `main` 的模型：沒有 models 表，不列。
- `/model <參考>` 打字用法不變。

**反目標**
- 金鑰讀不到的服務商的模型可以被選（必須灰色、游標跳過、附原因）。
- 確認框或通知出現空白名稱或 "custom"。

## 做法

只有一種合理做法：在 `modelSwitcher.Options()` 追加服務商模型選項，`modelPicker` 負責分段與捲動。另一種做法是在 TUI 端自己讀 `TierManager.Providers()` 組選項，但會重複 `modelSwitcher` 已有的可用性判斷和上下文長度解析，所以不採用。

## 設計

- **`commands.ModelOption` 加 `Group string`**：服務商 id，等級和 `default` 是 `""`。選單在 `Group` 改變時畫分段標題。
- **`modelSwitcher.Options()` 的追加**：
  - 依 `router.store.Providers()`（排除 `main`）逐家處理。
  - 有模型的服務商：每個模型產生一個 `ModelOption{Group: id, Provider: id, Model: m}`，`ContextWindow` / `MaxTokens` 由 `store.Resolve("id:m")` 解析（`MaxTokens` 為 0 時沿用 startup 值，與 `fromBinding` 相同）。可用性同 `fromBinding`：`router.client(id)` 回錯 → `Available=false`，`Reason` 為錯誤訊息。
  - 沒有模型的服務商：產生一個 `ModelOption{Group: id, Reason: "no models listed — check some with /provider"}`，不可選。
  - `Current` 判斷沿用現有規則（provider 與 model 相同）。
- **名稱顯示**：服務商模型的 `Name` 是 `""`；`applyModelSwitch` 的通知與 `renderModelConfirm` 的標題，在 `Name == ""` 時只顯示 `Label()`（不再顯示 "custom" 或空括號）。手打 `/model id:m` 也因此受益。
- **捲動**：`modelPicker.view` 加 `maxHeight` 參數（呼叫端用 `m.resumePickerMaxHeight()`）。可見列數 = `max(maxHeight-6, 3)`，分段標題也算一列；保證游標那一列在可見範圍內。
- **文件**：model-switch 設計文件的非目標「從 provider 列出所有模型讓人挑」改成「連網列舉 provider 的全部模型（只列 models 表）」；`docs/config-toml.md` 的「在會話中切換主腦（`/model`）」一節補一句選單會列出服務商 models 表的模型。

## 留給實作方

- 分段標題與灰色列的確切樣式（沿用 `mutedStyle`）。
- 捲動時分段標題是否固定顯示（不要求）。
