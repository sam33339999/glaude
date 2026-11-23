# `/provider`：管理服務商

在 TUI 輸入 `/provider`，就能新增、修改、刪除 `~/.glaude/config.toml` 裡的服務商（`[providers.<id>]`），每一家都有自己的 Base URL 和金鑰。**存檔後立即可用**：不用重新啟動，馬上就能用 `/model`、`/tier` 和 sub-agent 派工。整個流程只是程式邏輯，**不會呼叫 LLM、也不花 token**（驗證只打 `GET /v1/models`）。

主腦（`settings.json`）不歸 `/provider` 管，請用 [`/setup`](setup.md)。等級綁定和 `config.toml` 的完整說明見 [`docs/config-toml.md`](config-toml.md)。

設計背景：`docs/superpowers/specs/2026-11-23-provider-command-design.md`。

## 清單

```
Providers
  main        (main loop — configure with /setup)
❯ zai         openai   https://api.z.ai/api/paas/v4   key: stored   lite
  openrouter  openai   https://openrouter.ai/api/v1   key: env OPENROUTER_API_KEY (missing)
  + Add provider…

Enter edit/add · d delete · Esc close
```

- 每一列：id、格式、Base URL、金鑰來源（`stored` / `env <變數>` / `none`，讀不到時標 `(missing)`）、綁在這家的等級。
- `main` 只顯示，不能選。
- `config.toml` 在啟動時無法解析的話，清單仍可以看，但新增、修改、刪除都會被拒絕。

## 新增

1. **id**：只能用小寫英數字、`-`、`_`；`main` 是保留字；不能和現有的 id 重複。
2. **API 格式**：Anthropic（Messages API）或 OpenAI 相容（Chat Completions）。
3. **端點與金鑰**：Base URL、API key（輸入時遮蔽，可以直接貼上，前後空白會去掉）。本機端點（Ollama、LM Studio）勾 **No key needed**。Tab 切換欄位，空白鍵切換勾選。
4. **驗證**：和 `/setup` 相同；端點沒有模型清單時改成手打模型 ID（逗號分隔）。
5. **勾選模型**：打字過濾、空白鍵勾選。勾選的模型寫進 `[providers.<id>.models]`，清單有回報上下文長度時一併寫 `context_window`。
6. **綁定等級**（選填）：lite / pro / max 各自用 ←/→ 選「keep current binding」（預設）或某個勾選的模型。
7. **確認**：列出要寫入的內容，金鑰只顯示末 4 碼。Enter 寫入。
8. **完成**：顯示可以直接輸入的 `/model <id>:<模型>`。

任何一步按 Esc 都是回上一步。

## 修改

選一家按 Enter，從格式那一步開始，欄位帶入現值：

- **金鑰留空**：沿用目前的金鑰，不論它來自環境變數還是 `credentials.json`。
- **輸入新金鑰**：存進 `credentials.json`，原本的 `api_key_env` 會被移除。
- **勾 No key needed**：移除金鑰設定，並刪除 `credentials.json` 裡的那一筆。
- **模型**：已有的模型會先勾好。取消勾選的模型會從 `models` 表移除，但如果有等級綁著它，綁定保留。已寫在模型上的 `max_tokens` / `context_window` 也保留。
- **換了格式或 Base URL**，而且有等級綁在這家時，確認頁會提醒「新端點不一定有這些模型」，但不會自動解除綁定。

**主腦正在用這家時**（之前用 `/model` 切過來），存檔後主腦會**自動改用新設定**，模型 ID 不變。確認頁會先提醒：prompt 快取會失效，下一則請求要重送整段對話。換了格式或 Base URL 時，歷史中的 thinking 區塊會被移除。主腦正在回應時，不能修改它在用的那家。

已經派出去的 sub-agent 繼續用派工當下的連線，不受修改影響。

## 刪除

在清單上選一家按 `d`，確認後：

- 移除 `[providers.<id>]` 和 `credentials.json` 裡的金鑰。
- 綁在這家的等級會解除綁定（留下空的 `[tiers.<名稱>]`，之後繼承主腦）。
- 主腦正在用這家時拒絕刪除，請先用 `/model` 切到別處。

## 寫入哪些東西

**`~/.glaude/credentials.json`**（權限 `0600`、原子寫入，保留不認識的鍵）：

```json
{
  "providers": {
    "zai": { "api_key": "sk-..." }
  }
}
```

檔案存在但內容不合法時，啟動會顯示警告（不印出內容），並且**拒絕覆寫**；請手動修正或刪除它。

**`~/.glaude/config.toml`**：`[providers.<id>]` 整段重寫；用 `/provider` 存金鑰的服務商會帶 `api_key_source = "stored"`，**金鑰本身不會寫進這個檔案**。重寫時檔案裡的註解會遺失（和 `/tier` 相同）。

## 金鑰從哪裡來

依序：

1. 有 `api_key_env` → 讀那個環境變數；沒設定就是錯誤。
2. 有 `api_key_source = "stored"` → 讀 `credentials.json`；檔案壞掉或缺這一筆就是錯誤，派工和 `/model` 會明確失敗，不會不帶金鑰送出。
3. 兩者都沒有 → 不帶金鑰。

## 已知限制

- 不能改 id（請刪除後重新新增）。
- 同時開兩個 glaude 視窗、都用 `/provider` 新增同一個 id 時，後寫的會蓋掉先寫的；`credentials.json` 沒有跨程序鎖。
- 手動編輯 `config.toml` 新增的服務商，要重新啟動 glaude 才會出現在 `/provider` 清單並建立連線。
- 修改時金鑰欄留空會沿用原金鑰，即使 Base URL 換到別的網域也一樣；換到不同的服務時請一併輸入新金鑰。
