# `~/.glaude/config.toml`：provider 註冊與模型等級

這個檔案讓 sub-agent 依**等級**（`lite` / `pro` / `max`）把請求送到不同的 provider 和模型。主腦（main loop）的 provider 和模型仍由 `settings.json` 決定，不受這個檔案影響；`[main]` 段只影響主腦的上下文長度。

- 只讀全域這一份，沒有專案層或本地層。
- 檔案不存在：沒有 provider、沒有等級，行為和沒有這個功能時完全一樣。
- 格式錯誤或欄位不合法：啟動時印警告（stderr，並在 TUI 啟動訊息和 `/tier` 列表裡顯示），忽略有問題的部分，程式照常啟動。檔案無法解析時 `/tier` 拒絕寫檔。
- 用 TUI 的 **`/provider`** 新增、修改、刪除 provider，存檔後**立即生效**，金鑰可以直接存在 `~/.glaude/credentials.json`，見 [`docs/provider.md`](provider.md)。手動編輯檔案新增的 provider 則要**重新啟動**才會建立連線；`/tier` 只能在已註冊的 provider 之間改綁定。

還沒有任何模型時，可以在 TUI 輸入 `/setup` 用引導的方式設定主腦與等級，見 [`docs/setup.md`](setup.md)。

可直接複製的範例檔：[`docs/config.example.toml`](config.example.toml)（`cp docs/config.example.toml ~/.glaude/config.toml`；`TestExampleConfigParses` 確保它能被正確解析、沒有警告）。

設計背景見 `docs/superpowers/specs/2026-11-19-provider-tiers-design.md`。

## 範例

```toml
# Provider 註冊：id 自訂（"main" 是保留字，不能用）
[providers.qwencloud]
format      = "openai"          # "openai" | "anthropic"
base_url    = "https://dashscope.aliyuncs.com/compatible-mode/v1"
api_key_env = "DASHSCOPE_API_KEY"

[providers.openrouter]
format      = "openai"
base_url    = "https://openrouter.ai/api/v1"
api_key_env = "OPENROUTER_API_KEY"

[providers.zai]
format      = "openai"
base_url    = "https://api.z.ai/api/paas/v4"
api_key_env = "ZAI_API_KEY"

# 選填：provider 層的模型表
[providers.openrouter.models."qwen/qwen3.8-27b"]
context_window = 262144
max_tokens     = 16384

# 選填：只管主腦的上下文長度
[main]
context_window = 131072

# 等級綁定：provider id + 模型 ID（模型 ID 原樣傳給該端點）
[tiers.lite]
provider   = "zai"
model      = "glm-5.3-flash"
max_tokens = 8192

[tiers.pro]
provider = "openrouter"
model    = "qwen/qwen3.8-27b"

[tiers.max]
provider       = "main"              # 保留 id：沿用主腦的 client 與認證（含 OAuth）
model          = "claude-opus-5-5"
context_window = 200000
```

## 欄位

| 位置 | 欄位 | 說明 |
|------|------|------|
| `[providers.<id>]` | `format` | 必填。`"anthropic"`（Anthropic Messages）或 `"openai"`（OpenAI Chat Completions）。`bedrock` / `vertex` 不支援 |
| | `base_url` | 端點網址；結尾的 `/v1` 會自動去掉 |
| | `api_key_env` | 存放金鑰的**環境變數名稱**。不接受明文 `api_key`（出現時整個 provider 會被忽略）。和 `api_key_source` 都留空表示這個端點不需要金鑰 |
| | `api_key_source` | 選填，只接受 `"stored"`：金鑰存在 `~/.glaude/credentials.json`（由 `/provider` 寫入）。其他值會讓整個 provider 被忽略；和 `api_key_env` 同時出現時以 `api_key_env` 為準 |
| `[providers.<id>.models."<模型>"]` | `context_window`、`max_tokens` | 選填，正整數 |
| `[main]` | `context_window` | 選填。主腦的壓縮門檻和狀態列顯示用；不改主腦的請求內容 |
| `[tiers.<名稱>]` | `provider`、`model` | 兩個都寫才算綁定；都不寫（空表）＝已知但未綁定 |
| | `context_window`、`max_tokens` | 選填，優先於 provider 模型表 |

等級名稱和數量由檔案決定，`[tiers.<任意名>]` 都合法；`lite` / `pro` / `max` 永遠是已知等級，即使沒寫。

## 值的解析順序

- **上下文長度**（sub-agent）：`CLAUDE_CODE_AUTO_COMPACT_WINDOW` > 等級的 `context_window` > provider 模型表 > 內建表 > 預設值 200000。
- **上下文長度**（主腦）：`CLAUDE_CODE_AUTO_COMPACT_WINDOW` > `[main] context_window` > 內建表 > 預設值。
- **`max_tokens`**：等級 > provider 模型表 > engine 預設。摘要請求的輸出上限取 `min(20000, max_tokens)`。

綁到 `main:<模型>` 的等級用等級自己的值，不套用 `[main]`。

自動壓縮的門檻是 `context_window − min(max_tokens, 20000, context_window/4) − min(13000, context_window/10)`。小窗口（例如 32k）的模型請務必寫上正確的 `context_window`，否則會被當成 200k，直到 API 回報超限才出錯。

## sub-agent 怎麼選模型

順序不變：`CLAUDE_CODE_SUBAGENT_MODEL` > Agent 工具的 `model` 參數 > agent profile 的 `model` > 繼承主腦。拿到的值：

- 是**已綁定的等級**（或舊別名 `haiku`→`lite`、`sonnet`→`pro`、`opus`→`max`）→ 用該等級的 provider 和模型。
- 是 `providerID:modelID`（只以第一個 `:` 切開，而且 provider 已註冊或是 `main`）→ 直接用。
- 是**未綁定的** `lite` / `pro` / `max` → 照舊別名的規則：Anthropic 系 provider 用對應的模型家族，openai provider 則繼承主腦。
- 是未綁定的自訂等級 → 繼承主腦。
- 其他字串 → 當成模型 ID 原樣傳給主腦的 provider。

內建的 explore agent 預設用 `lite`。fork agent 永遠繼承主腦（才能共用 prompt cache），不受等級影響。

金鑰的解析順序：`api_key_env`（變數必須有值）→ `api_key_source = "stored"`（`credentials.json` 必須有這一筆）→ 不帶金鑰。金鑰讀不到時，啟動照常進行，但派工到那個等級會直接失敗，錯誤訊息會點名環境變數或請你用 `/provider` 重新輸入，**不會**默默改用主腦或不帶金鑰送出。

## 在會話中切換主腦（`/model`）

`/model` 會打開選單，列出 `default`（`settings.json` 的主腦）和 lite / pro / max，接著依 provider id 分段列出每個 provider `[providers.<id>.models]` 裡的模型（不連網；沒有模型的 provider 顯示一行提示，請用 `/provider` 勾選）。選了就把主腦換成該目標的 provider client、模型、`context_window` 和 `max_tokens`，可以跨 provider。選單太長時可以捲動。**只在本 session 有效**，重開回到 `settings.json`。

- `/model <參考>` 也可以：等級名、舊別名（`opus` → `max`）、`providerID:modelID`，或目前主腦 provider 上的模型 ID。
- 對話已有內容時會先確認：換模型或 provider 會讓 prompt 快取失效，下一則請求要重送整段對話；新模型的窗口比目前對話小時，下一輪會壓縮並可能截掉最舊的內容。
- 未綁定或金鑰缺失的等級顯示為不可用；查詢進行中不能切換。
- 跟著主腦一起換的：主對話、繼承主腦的 sub-agent、記憶抽取等背景查詢。已派出的 sub-agent 續跑時仍用派工當下的模型。
- 綁到 `main:<模型>` 的等級永遠用啟動時（`settings.json`）的 provider 和金鑰，不受 `/model` 影響。
- 換 provider 時，歷史中的 thinking 區塊會被移除（簽章只對原 provider 有效）。

## `/tier` 指令

```
/tier                                  列出等級綁定、provider 與金鑰狀態
/tier <等級> <providerID>:<modelID>    改綁定並寫回檔案
/tier <等級> none                      解除綁定（之後繼承主腦）
```

- 改綁定在**下一個派出的 sub-agent** 生效；已在跑的 agent（包括之後被續跑的）沿用派工當下的綁定。
- 改綁定會清掉該等級原本寫的 `context_window` / `max_tokens`（那是舊模型的值），需要的話請寫在 provider 模型表。
- 寫回檔案時會重新輸出整個 TOML，**檔案裡的註解會遺失**。
