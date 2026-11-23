# `/provider` 服務商管理指令 設計

- 日期：2026-11-23
- 狀態：草稿，待負責人核准（已過 builder check，2026-11-23）
- 分支：`feat/provider-command`（從 `dev` 9143848 分出）
- 相關：`docs/config-toml.md`、`docs/setup.md`、`docs/superpowers/specs/2026-11-19-provider-tiers-design.md`、`docs/superpowers/specs/2026-11-19-setup-wizard-design.md`

## 1. 意圖

glaude 已經能同時用多家服務商（`config.toml` 的 `[providers.<id>]` ＋ 等級綁定），但**唯一的設定方式是手寫 TOML**，金鑰還得另外 `export` 到 shell 設定檔，改完再重開。`/setup` 幫不上忙：它只寫 `settings.json` 那一組主腦設定，重跑就覆蓋；「Anthropic / OpenAI 相容」選的是線格式而不是服務商，所以根本無法用它註冊第二家。

`/provider` 讓使用者在 TUI 裡**一家一家**新增、修改、刪除服務商——各自的 Base URL 與金鑰——並在**存檔後立即可用**（`/model`、`/tier`、sub-agent 派工），不需要重開、不需要碰 shell。

全程是程式邏輯，不呼叫 LLM（與 `/setup` 相同）：驗證只打 `GET /v1/models`。

## 2. 目標 / 非目標 / 反目標

**目標**
- `/provider` 開啟服務商清單，可新增、修改、刪除。
- 新增／修改流程沿用 `/setup` 精靈的畫面與行為：格式 → Base URL ＋ 金鑰 → 驗證 → 勾選模型 → （選填）綁定 lite / pro / max。
- 金鑰由指令直接儲存到 `~/.glaude/credentials.json`（權限 0600），使用者不必設定環境變數。
- 存檔後**立即生效**：新的或修改過的服務商馬上能被 `/model`、`/tier`、sub-agent 使用。
- 既有的 `api_key_env` 寫法照常支援。

**非目標**
- 主腦（`settings.json`）：仍由 `/setup` 管，`/provider` 完全不碰。
- 改名（改 id）：用「刪除＋新增」代替。
- Bedrock / Vertex、OAuth。
- 保留 `config.toml` 的註解（沿用現況：重寫整個 TOML 時註解遺失）。
- 專案層／本地層設定：只寫全域 `~/.glaude/`。

**反目標**（技術上可行但算失敗）
- 金鑰出現在 `config.toml`、log、警告訊息、TUI 畫面（確認頁只顯示末 4 碼）。
- 修改或刪除服務商時，**已派出的 sub-agent 中途被換掉連線**。
- 任何失敗被默默改用主腦（沿用 provider-tiers 的原則：失敗要大聲）。
- 寫壞或覆蓋一份無法解析的 `config.toml` / `credentials.json`。

## 3. 限制（現況）

- `modeltier.validate` 遇到 `[providers.<id>]` 下的 `api_key` 會丟棄整個 provider（`internal/modeltier/config.go:141`）。這條規則**保留**：金鑰不放 `config.toml`，放獨立檔案。
- 金鑰解析散在三處，都只讀環境變數：`loadTierRouter`（`internal/bootstrap/tiers.go:58`）、`tierManager.ListProviderModels`（`tiers.go:194`）、`Store.Providers` 的 `KeyPresent`（`internal/modeltier/store.go`）。
- `tierRouter.clients` / `keyErr` 是無鎖 map，啟動時建一次（`tiers.go:21`）；`modelSwitcher.clientFor` 與 `resolveSubAgentTarget` 直接讀它。
- `resolveSubAgentTarget` 對「啟動後才註冊的 provider」已經有明確錯誤（`tiers.go` 約 116 行），本設計要讓這條路徑變成正常可用。
- `/setup` 的線格式常數是 `direct` / `openai`（`commands.SetupFormat*`），`config.toml` 用 `anthropic` / `openai`，需要對應。
- 寫 `config.toml` 一律走 `Store.commitLocked`（原子寫入、無法解析時拒寫）。

## 4. 做法比較

| 做法 | 說明 | 取捨 |
|------|------|------|
| 金鑰只記環境變數名稱 | 指令寫 `api_key_env`，使用者自行 export | 最安全，但每加一家都要去改 shell 再重開，沒有解決痛點 |
| **金鑰存 `credentials.json`（選定）** | 指令直接存金鑰到 0600 檔案 | 一個指令搞定；放寬「glaude 不存金鑰」的範圍，但與 `settings.json` 的 `apiKey`（明文 0600）同等級 |
| 存完要求重開 | 與 `/setup` 一樣 | 改動最小；但加完還得退出才能用，打斷流程 |
| **存完立即生效（選定）** | 存檔後建 client 放進 router | 符合「設好就用」；需替 router 加鎖並定義「已派出 agent 沿用舊連線」 |

負責人選擇：金鑰存檔（B）；生效時機由負責人交給實作方判斷，實作方選立即生效，理由：client 建立時不連線、成本低；「已派出者沿用舊值」是 `/tier` 已有的規則。

主腦正用的 provider 被修改時：負責人選 A（存檔時自動套用到主腦，見 §5.6），不選 B（主腦不動、等使用者手動 `/model` 重新套用；需要改 `/model` 的「已在用就不切換」判斷）。

## 5. 設計

### 5.1 指令與畫面

```
/provider            開啟服務商清單
```

清單畫面（沿用 `/tier` 選單的風格）：

```
Providers                                  ~/.glaude/config.toml
  main        (main loop)
› zai         openai   https://api.z.ai/api/paas/v4      key: stored   lite
  openrouter  openai   https://openrouter.ai/api/v1      key: env OPENROUTER_API_KEY (missing)
  + Add provider…

enter edit · d delete · esc close
```

- `main` 只顯示、不可選（主腦請用 `/setup`）。
- 每列顯示格式、Base URL、金鑰來源與狀態、綁到它的等級。
- 啟動時的 `config.toml` 警告顯示在清單上方（同 `/tier`）。
- `config.toml` 無法解析時清單仍可看，但新增／修改／刪除都拒絕並說明原因。

不提供 `/provider add …` 之類的一行式子指令（YAGNI；金鑰不該出現在指令列歷史）。

### 5.2 新增流程

1. **id**：輸入 provider id。目前 `modeltier` **沒有** id 驗證（`validate` 只擋 `main` 與不合法 format），需新寫：`/provider` 新建的 id 只允許 `[a-z0-9_-]+`，`main` 保留、不可與既有 id 重複。一定要擋 `:`（`Resolve` 以第一個 `:` 切 `id:model`）。*（實作方決定）* 手寫在 `config.toml`、不符此規則的既有 id 照常列出、可修改、可刪除；`config.toml` 載入時不因此新增警告。
2. **格式**：Anthropic / OpenAI 相容。
3. **Base URL ＋ 金鑰**：沿用 `/setup` 第 2 步的欄位與遮蔽。金鑰欄下方多一個「不需要金鑰」切換（給 Ollama、LM Studio 等本機端點）*（實作方決定）*；沒勾它時新增必須填金鑰。
4. **驗證**：沿用 `/setup` 的 `ListModels`（含 OpenRouter `/api/v1/key` 檢查、無模型清單時改手打）。
5. **勾選模型**：沿用 `/setup` 的過濾＋空白勾選。勾選的模型寫入 `[providers.<id>.models."<m>"]`；清單回報了上下文長度就一併寫 `context_window`，沒有就寫空表（讓 `/tier` 列得出來）。
6. **綁定等級**（選填）：lite / pro / max 各自選「維持現狀」（預設）或某個勾選的模型。選了就 `Bind(tier, id, model)`。
7. **確認**：列出要寫入的內容，金鑰只顯示末 4 碼。Enter 寫入，Esc 返回。
8. **完成**：顯示「已可使用」，以及可直接輸入的 `/model <id>:<model>` 範例。

任何一步 Esc 回上一步；第 1 步 Esc 回清單。

### 5.3 修改流程

選一家 → 從第 2 步開始，各欄位帶入現值：
- 金鑰欄留空＝沿用目前金鑰（不論來自 `credentials.json` 或環境變數）。顯示「leave empty to keep using …」。
- 輸入新金鑰：存進 `credentials.json`，**移除**該 provider 的 `api_key_env`，寫入 `api_key_source = "stored"`（見 §5.5）。
- 勾「不需要金鑰」：移除 `api_key_env` 與 `api_key_source`，並刪除 `credentials.json` 裡的對應金鑰。
- 驗證、勾選模型、綁定等級同新增；勾選畫面預先勾好 `models` 表裡已有的模型。取消勾選的模型從 `models` 表移除，**但它若被某個等級綁定，綁定保留**（等級的 model ID 原樣傳給端點，不需要在 models 表裡）。
- 格式或 Base URL 改了、而某等級綁在這家：確認頁提醒「lite 綁在 zai:glm-5.3-flash，新端點不一定有這個模型」，不自動解除。

### 5.4 刪除

- 確認頁列出受影響的等級（綁在這家的 lite/pro/max/自訂等級）。確認後：移除 `[providers.<id>]`、解除那些等級的綁定（**留下空的 `[tiers.<name>]`**，與 `Unbind` 相同，之後繼承主腦；不整個刪掉，否則自訂等級名會變成「不是等級」而被當成模型 ID 傳給主腦）*（實作方決定）*、刪除 `credentials.json` 裡的對應金鑰、從 router 移除 client。
- **主腦目前正透過 `/model` 用這家時拒絕刪除**，提示「先用 `/model` 切到別處」。（主腦的 client 由 engine 持有，刪掉 provider 會讓狀態列與 `/model` 列表矛盾。）
- 已派出、正在跑的 sub-agent 不受影響（它們已經拿到 client 參照）。

### 5.5 金鑰儲存：`~/.glaude/credentials.json`

```json
{
  "providers": {
    "zai": { "api_key": "sk-..." }
  }
}
```

- 權限 0600，原子寫入，保留檔案中不認識的鍵。注意 `config.WriteOrderedJSONAtomic` **不能直接用**：它建新檔時權限是 0644（`existingModeOr(path, 0o644)`），暫存檔名寫死 `.mcp-*.json`；需公開一個可指定權限的版本（現有 `writeOrderedJSONAtomicMode` 是私有的）或另寫一個。
- 檔案存在但不是合法 JSON：讀取時當作空（並在 `/provider` 清單與啟動警告顯示問題，不顯示內容），**寫入一律拒絕**。
- 新增一個小元件（建議放 `internal/modeltier`，它已經負責 `config.toml` 且不依賴 `internal/api`）提供 `Get(id)` / `Set(id, key)` / `Delete(id)`。

**`config.toml` 的來源標記** *（實作方決定，為了「失敗要大聲」）*：用 `/provider` 存金鑰的 provider 會寫 `api_key_source = "stored"`。`modeltier.Provider` 加這個欄位；值只接受 `"stored"`（其他值→警告並忽略該 provider）；與 `api_key_env` 同時出現→警告，以 `api_key_env` 為準。

**金鑰解析順序**（統一成一個函式，取代 §3 列的三處）：

1. `api_key_env` 有寫 → 讀該環境變數；沒設定就是錯誤（錯誤訊息點名變數，維持現行）。
2. `api_key_source = "stored"` → 讀 `credentials.json` 的這個 id；檔案壞掉、不存在或沒有這一筆就是**錯誤**（記入 `keyErr`，派工與 `/model` 都明確失敗，訊息說明「credentials.json 裡沒有 <id> 的金鑰，請用 /provider 重新輸入」），**不會**改成不帶金鑰送出。
3. 兩者都沒有 → 無金鑰（本機端點，維持現行「留空＝不需要金鑰」）。

`ProviderStatus` 增加金鑰來源欄位（`env` / `stored` / `none`），`/tier` 和 `/provider` 都顯示它。

### 5.6 立即生效：router 熱更新

- `tierRouter` 加 `sync.RWMutex`，`clients` / `keyErr` 的所有讀取改走帶鎖的方法（`clientFor`、`resolveSubAgentTarget`、`modelSwitcher` 的可用性檢查）。
- 新增 `tierRouter.Upsert(id)`：從 `Store` 取 provider、依 §5.5 解析金鑰、用既有 `providerClientFactory` 建 client，替換 `clients[id]`、更新 `keyErr[id]`。`Remove(id)` 刪除兩者。
- 寫檔順序：`credentials.json` → `config.toml`（`Store` 更新記憶體與檔案）→ `router.Upsert`。任一步失敗就停止並在畫面上說明哪一步沒成功；`credentials.json` 先寫是因為多一把沒人用的金鑰無害，反過來則會出現「設定了卻沒金鑰」。
- **已派出的 sub-agent**：`subAgentTarget.Client` 在派工時就取得，之後替換 map 不影響它們——與 `/tier`「派工當下為準」的規則一致。續跑（resume）的 agent 同樣沿用原 client。
- **主腦正用這家**（`/model` 切過去）時修改它：存檔後**自動把主腦換到新 client**，模型 ID 不變（負責人決定 A）。確認頁先說明「主腦會改用新設定；prompt 快取會失效，下一則請求重送整段對話」。理由：會去改正在用的服務商，多半是金鑰失效或網址打錯，沿用舊設定沒有意義。
  - 實作：Upsert 後若 `eng.MainTarget().ProviderID == id`，以新 client 呼叫 `SetMainTarget`（`ContextWindow` / `MaxTokens` 依新設定重新解析），並同步更新 `MainLoopModel.Provider`（格式可能改了）。格式改變時比照 `/model` 跨 provider 的處理，移除歷史中的 thinking 區塊。
  - 查詢進行中不切換：比照 `/model` 規則，查詢進行中 `/provider` 拒絕修改「主腦正在用的那家」，提示等查詢結束。
  - `modelSwitcher.clientFor` 的「目前主腦就是這家就回傳目前 client」捷徑改成優先查 router，避免任何路徑拿到舊 client。
  - 不需要改 `/model` 的「已經在用就不切換」判斷（reviewer 指出原本「提示重新 `/model`」的做法會被它擋掉，A 不依賴這條路徑）。

### 5.7 TUI 與層次

- 新的對話框沿用 `/setup` 的純狀態機做法（`setupWizard`：不做 I/O，回傳 action，TUI 執行 `tea.Cmd`）。`/setup` 的端點、驗證、模型勾選步驟抽成可共用的部分，不複製貼上；角色步驟各自保留（`/setup` 分配主腦，`/provider` 只分配等級且預設「維持現狀」）。
- `commands` 層新增 `ProviderBackend` 介面（列表、ListModels、Apply 新增/修改、Delete），由 `bootstrap` 實作並注入，沿用 `SetupBackend` / `TierManager` 的模式（commands 不 import bootstrap）。
- 遵守層次規則：`modeltier` 不 import `internal/api`；client 建構留在 `bootstrap`。

## 6. 文件

- `docs/config-toml.md`：加「用 `/provider` 管理」一節、`credentials.json` 與金鑰解析順序；把「新增 provider 要重啟」改成「手動編輯要重啟，用 `/provider` 立即生效」。
- `docs/setup.md`：「不支援的部分」裡的「註冊第二個 provider」改指向 `/provider`。
- 新增 `docs/provider.md`（使用說明，風格同 `docs/setup.md`）。
- 受影響套件的 `CONTEXT.md` 用 `make docs` 重新產生。

## 7. 測試重點

- 金鑰解析順序三種情況；`credentials.json` 壞掉時讀為空、寫入拒絕；檔案權限 0600。
- 金鑰不出現在 `config.toml`、錯誤與警告字串。
- 新增 → 不重開即可 `resolveSubAgentTarget` 到新 client；刪除 → 回明確錯誤、不 fallback 主腦。
- 併發：派工中的 agent 持有舊 client，同時 Upsert/Remove（`go test -race`）。
- 主腦正用的 provider 拒絕刪除；修改後主腦自動換到新 client（模型不變、格式改變時移除 thinking 區塊）；查詢進行中拒絕修改它。
- `api_key_source = "stored"` 但 `credentials.json` 缺這筆／壞掉 → 明確錯誤，不會不帶金鑰送出。
- id 驗證：擋 `:`、`main`、重複、不合規字元；既有手寫 id 不受影響。
- 精靈狀態機：Esc 返回、修改時金鑰留空沿用、取消勾選但被等級綁定的模型保留綁定。
- `make all`、`make docs-check`、`make debt-check` 全綠；不新增 `TODO(dep)` 或 not-yet-implemented。

## 8. 留給實作方決定

- 共用 `/setup` 步驟的具體抽法（嵌入、組合或參數化）。
- `credentials.json` 元件的確切 API 與檔案位置（建議 `internal/modeltier`）。
- 清單畫面的細部排版與快捷鍵（以 `/tier` 選單為準）。
