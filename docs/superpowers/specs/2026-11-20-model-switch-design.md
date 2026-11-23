# `/model` 選單切換主腦（等級＋快取警示）設計

- 日期：2026-11-20
- 狀態：已核准並實作（2026-11-20，分支 `feat/model-switch`）。
- 範圍：provider-tiers 設計 §10 的第 2 件（原本只寫「切換時警示快取失效」，討論後擴大為「讓 `/model` 真正能切換」）。第 3 件 `/btw` 另寫設計。
- 相關：`docs/superpowers/specs/2026-11-19-provider-tiers-design.md`、`docs/config-toml.md`、`docs/PROMPT_CACHE_AND_COST.md`。

## 1. 意圖與現況

負責人回報「`/model` 根本沒辦法切換不同模型」。查證（2026-11-20）的現況：

- `/model <ID>` 會呼叫 `engine.SetModel`，同一個 provider 上打對完整 ID 確實會換；但沒有清單、不檢查 ID、打錯要等送出才 404。
- 別名不認得：`/model opus` 會把字面 `"opus"` 原樣送給 API。
- 主腦 client 啟動時綁死 `settings.json` 的 provider / baseUrl / 金鑰，**無法切到別的 provider**（打了 qwen 的 ID 也還是送到原端點）。等級可以綁不同 provider，主腦不行。
- 切換時沒有任何快取失效的警示。

目標：`/model` 打開選單，從 `default`（settings.json 的主腦）與 lite / pro / max 等級中選一個，主腦就換成該等級的 **provider client、模型、上下文長度與輸出上限**；對話已有內容時先警示快取會失效。

## 2. 目標 / 非目標 / 反目標

**目標**
- `/model`（不帶參數）開啟選單：`default` 一列加上每個等級一列，顯示 `provider:model` 與上下文長度；未綁定的等級顯示為灰色、不可選。
- 選擇後主腦切換到該目標；所有「跟著主腦」的查詢一起換（見 §5.2）。
- 對話已有內容時，切換前跳確認框說明快取失效，按 yes 才換。
- `/model <參考>` 打字用法保留，並與選單共用同一套解析：等級名、舊別名（haiku/sonnet/opus → lite/pro/max）、`providerID:modelID`、完整模型 ID（在目前主腦的 provider 上）。
- 狀態列顯示新模型與新的上下文長度。

**非目標**
- **寫回 `settings.json`**：負責人決定**只在本 session 有效**，重開回到 `settings.json`（主腦預設由 `/setup` 或手動設定決定）。
- 在 `/model` 裡新增或編輯 provider、等級（那是 `config.toml` / `/tier` / `/setup` 的事）。
- 連網列舉 provider 的全部模型（選單只列 `config.toml` 的 models 表，見 `2026-11-23-model-picker-provider-models-design.md`）。

**反目標**
- 沒有選擇切換時，主腦的請求位元組改變（golden 測試 `TestQuery_MainLoopRequestUnchangedWithoutTiers` 必須維持）。
- 切換後只有主對話換了，繼承主腦的 sub-agent、記憶抽取等背景查詢卻還在打舊 provider（不一致）。
- 查詢進行中切換，導致同一輪對話前後用了不同的模型或 provider。
- 未警示就讓使用者付出整段前綴重算的成本。
- 金鑰缺失的等級被選中後默默退回舊 client。

## 3. 限制

- 依賴方向：TUI/commands 不得 import bootstrap；切換能力以介面注入。
- MVU：切換本身不做 I/O（client 在啟動時就建好），可以在 Update 內同步完成；確認框是一般的對話框狀態。
- Cache-first：切換主腦**本來就會**讓前綴快取失效（模型或 provider 變了），這是使用者明確選擇的結果；不新增其他前綴變動路徑，不新增 LLM 呼叫。
- 檔案 ≤800 行、函式 ≤80 行；導出符號 GoDoc；改到導出符號的包 `make docs` 後補 Design Notes。

## 4. 做法比較（負責人選 A）

| 做法 | 內容 | 取捨 |
|------|------|------|
| **A. engine 新增「換主腦目標」** ✅ | `SetMainTarget(client, model, contextWindow, maxTokens)`（mutex 保護），取代只換模型名的 `SetModel` | 主腦、繼承主腦的 sub-agent、背景查詢都讀 engine 預設值，一致跟著換；壓縮門檻與摘要自動改用新目標。代價是預設 client 從固定變成可變，要補並發測試 |
| B. TUI 每次在 `QueryParams` 帶 Client/Model | engine 不改 | 只有主對話換了；繼承主腦的 sub-agent、fork 快照、記憶抽取仍走舊 client——不符合「切換主腦」的預期 |

## 5. 設計

### 5.1 解析：`ModelSwitcher`

- 新介面放在 `commands`（與 `TierManager`、`SetupBackend` 同處），bootstrap 實作：
  ```go
  // ModelOption is one /model menu entry.
  type ModelOption struct {
      Name          string // "default" | tier name
      Provider      string // provider id, "main" for the startup client
      Model         string
      ContextWindow int
      Available     bool   // false: unbound tier or missing key
      Reason        string // why not available (e.g. "unbound", "ZAI_API_KEY not set")
      Current       bool   // the main loop is on this target now
  }
  type ModelSwitcher interface {
      Options() []ModelOption
      // Switch resolves ref (option name, legacy alias, providerID:modelID,
      // or a plain model ID on the current provider) and retargets the main
      // loop. It returns the applied option.
      Switch(ref string) (ModelOption, error)
  }
  ```
- `default` = 啟動時的主腦（settings.json 的 client、模型、`[main] context_window`、引擎預設 max_tokens）。
- 解析順序：`default` → 等級名／舊別名（`modeltier.Resolve`）→ `providerID:modelID`（provider 已註冊或 `main`）→ 其餘視為**目前主腦 provider 上的**模型 ID（保留現在 `/model <ID>` 的行為）。
- 等級綁定的 `context_window` / `max_tokens` 依 provider-tiers 的既有解析順序；`main:<模型>` 用主腦的 client。
- 等級的 provider 沒有 client（金鑰缺失、或啟動後才加入）→ `Available=false`，`Switch` 回錯誤點名原因，**不切換**。
- 每次 `Options()` 即時讀執行中的 `modeltier.Store`，所以 `/tier` 改綁定後選單立刻反映。

### 5.2 Engine：`SetMainTarget`

- 新增：
  ```go
  // MainTarget is what the main loop (and every query without its own
  // Client/Model) runs on.
  type MainTarget struct { Client api.Client; Model string; ContextWindow, MaxTokens int }
  SetMainTarget(t MainTarget)   // QueryEngine 介面新增
  ```
  以既有的 `modelMu`（改名為 `mainMu` 亦可）保護 client、model、mainContextWindow、maxTokens 四個值；`clientFor`、`activeModelFor`、`targetFor`、`buildRequestWithModel` 改讀快照。`SetModel(m)` 保留為「只換模型」的便捷方法（內部呼叫 SetMainTarget）。
- 跟著主腦一起換的：主對話、`Client` 為 nil 且 `Model` 為空的 sub-agent（inherit）、fork（快照取 activeModel，provider 跟 engine 預設 client 走）、記憶抽取／session memory 等背景 `Query`。
- 不受影響的：等級路由的 sub-agent（自帶 `Client`）；**已派出的 sub-agent 續跑時凍結在派工當下的目標**：inherit 與 fork 的 sub-agent 在派工時就把當下的主腦 client、model、window、max_tokens 寫進 params（目前 inherit 存的是 nil/空字串，續跑時會讀到新主腦——要改），沿用 provider-tiers「續跑沿用派工當下綁定」的快取連續性原則。
- **`main:<模型>` 等級永遠指啟動時的主腦 client**（settings.json 的 provider 與金鑰），不是「目前的主腦」：bootstrap 保留啟動 client，`resolveSubAgentTarget` 遇到 `main` 綁定時明確帶入它。否則主腦切到 zai 後，`lite = main:claude-haiku` 會把 haiku 送到 zai 而 404。
- **切換存的是解析後的目標快照**，不是等級名：之後 `/tier` 改了該等級，主腦不跟著換（要換就再選一次）。`Options().Current` 以 provider＋model 比對判定。
- **跨 provider 的歷史**：provider 改變時，engine 從之後送出的請求中移除 thinking 區塊（原廠簽章到別家 Anthropic 相容端點可能回 400；切換本來就讓前綴失效，所以這不增加額外的快取損失）。tool_use / tool_result 照舊保留。同一 provider 只換模型時不清洗。
- **一輪查詢內不換**：`runQueryLoop` 在查詢開始時取一次目標快照，整個 loop 都用它（修正前每輪重讀 `e.model`）。
- bootstrap 同步更新 `AppState.MainLoopModel`（`ModelID`，以及 `Provider` 填線格式 `direct` / `openai`），並把 `buildRunAgentFn` 傳給別名解析的 provider 從 `st.Settings.Provider` 改為 `MainLoopModel.Provider`（空值時退回 `Settings.Provider`），讓切到 openai 格式的 zai 後，inherit 的別名規則用 openai 邏輯。resume 一個切換過的 session 時回到 default（切換只在 session 有效）；`MainLoopModel` 的序列化格式不變。

### 5.3 TUI

- `/model` 不帶參數 → `Result{OpenDialog: "model"}` → 新的 `dialogModelPicker`（比照 `/resume` 選單：↑/↓、Enter、Esc），列出 `Options()`；不可選的列以灰色顯示原因，游標跳過。
- `/model <參考>` → 直接走 `Switch(ref)`，結果或錯誤以 system 訊息顯示。
- **查詢進行中**（`isLoading`）：拒絕切換並提示「先等這一輪結束或按 Esc 中斷」。
- **快取警示**：引擎歷史非空、且目標與目前不同時，選擇後先開確認框：
  ```
  Switch main model to pro (openrouter:qwen/qwen3.8-27b)?
  The prompt cache is tied to the current model and provider: the next
  request re-sends the whole conversation (≈<估算> tokens) without cache.
  y switch · n cancel
  ```
  估算值用目前歷史的 token 估算（`compact.EstimateTokens`，不含 overhead 也可，標示「≈」）。
  - 目前歷史估算超過新目標的自動壓縮門檻時，確認框多一行警告：「conversation is larger than the new model's window: it will be compacted on the next turn and the oldest part may be trimmed」。壓縮的摘要輸入會經 `fitForSummary` 截到新目標放得下，不會失敗，但最舊的內容可能被捨棄。不在切換前用舊目標先壓縮（那會新增 LLM 呼叫）。歷史為空或選的就是目前目標 → 不警示（選目前目標 = no-op，提示「already on …」）。
- 成功後：system 訊息「Main model: pro → openrouter:qwen/qwen3.8-27b (session only)」；狀態列更新 model 與 `contextWindow`。
- `commands` 層不得 import bootstrap；`ModelSwitcher` 由 `tui.New` 注入（nil → 退回現在的 `SetModel` 行為）。

## 6. Cache-first 三問

- **是否移動前綴？** 只有使用者在確認框按 yes 時才會，而且是預期中的（換模型或 provider 必然失效）。不切換時主腦請求位元組不變（golden 測試）。工具 schema 不變（`/model` 是 slash 指令）。
- **是否增大窗口？** 否。（切到更大窗口的模型時，門檻依新目標計算，屬正確行為。）
- **是否新增呼叫？** 否。

## 7. 測試重點

- engine：`SetMainTarget` 後主對話與 inherit sub-agent 都打新 client；帶 `Client` 的 sub-agent 不受影響；一輪查詢中途 `SetMainTarget` 不改變該輪後續請求的 client/model；並發 `SetMainTarget` 與 `Query` 在 `-race` 下無報告；壓縮門檻用新目標的窗口。
- bootstrap：`Options()` 反映 `/tier` 改綁定、未綁定與金鑰缺失顯示為不可用；`Switch` 的四種解析路徑；不可用目標回錯誤且不切換；`default` 換回啟動時的 client；切換後 `AppState.MainLoopModel` 的 ID 與 Provider 正確。
- TUI：選單開啟與按鍵；不可選列被跳過；有歷史時出現確認框、n 取消不切換、y 切換；無歷史時直接切換；查詢中拒絕；狀態列更新；`/model <參考>` 的成功與錯誤訊息；`ModelSwitcher` 為 nil 時維持舊行為。

## 8. 留給實作者決定

- 選單與確認框的排版、文字（英文，與其他 slash 指令一致）。
- `ModelOption` 的確切欄位、`MainTarget` 是否沿用既有欄位名。
- token 估算是否加上 system prompt 與 schema。

## 9. 負責人決定紀錄

- 2026-11-20：`/model` 改為從 default ＋ lite/pro/max 選單切換，跨 provider 也可以；只在 session 有效；做法 A（engine 換主腦目標）。

## 10. 我替負責人做的決定（核准時請確認）

1. 保留 `/model <參考>` 打字用法，並與選單共用解析；純模型 ID 視為目前主腦 provider 上的模型（維持現行行為）。
2. 查詢進行中拒絕切換，不排隊。
3. 一輪查詢開始時取目標快照，整輪不換。
4. 選目前已在用的目標 = no-op，不警示。
5. 等級金鑰缺失時選單顯示不可用，不退回舊 client。
6. 確認框顯示的 token 數是估算值。
7. （審閱後）`main:<模型>` 等級永遠指啟動時的主腦 client。
8. （審閱後）已派出的 sub-agent 續跑時凍結在派工當下的目標（inherit / fork 派工時寫入當下主腦目標）。
9. （審閱後）切到窗口比歷史小的目標：確認框警告、下一輪壓縮並可能截掉最舊內容；不在切換前先壓縮。
10. （審閱後）provider 改變時移除歷史中的 thinking 區塊；同 provider 換模型不清洗。
11. （審閱後）切換存目標快照，之後 `/tier` 改綁定不影響主腦；`Current` 以 provider＋model 判定。
12. （審閱後）別名解析改看 `MainLoopModel.Provider`；resume 回到 default。

## 11. 已知、之後再處理（2026-11-20 審閱後）

- 記憶 stop hook 的 fork 沒有帶 model / client / 上限（`RunForkedAgent` 忽略 `CacheSafeParams.Client`），切換 provider 後記憶抽取可能失敗。
- `buildForkQuery` 在快照有 Model 但沒有 Client 時，model 與 client 分別回退。
- `buildRunAgentFn` 的 parentModel 取自 AppState、main 取自 engine，兩個來源。
- 以純模型 ID 切換時 `ContextWindow` 歸 0，改用內建表。
- 等級綁到 `main:<啟動模型>` 時，選單可能有兩列都標示 current。
- `/resume` 舊 session 時，若主腦已在非啟動 provider，不會清除 thinking 區塊。
- 確認框開著時，agent 喚醒可能已讓主腦開始一輪；按 y 時沒有再檢查 isLoading。
- 雜項：engine 與 modeltier 各有一個 `"main"` 常數；壓縮 debug log 文字仍寫「tiered sub-agent」；`wire.go`、`buildSubAgentQuery`、`buildRunAgentFn` 超出行數上限。
