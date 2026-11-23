# `/setup`：設定主腦模型

第一次使用 glaude、還沒有可用模型時，在 TUI 裡輸入 `/setup`，照畫面一步一步設定端點、API key、模型和等級。整個流程只是程式邏輯，**不會呼叫 LLM、也不花 token**。

啟動時如果找不到任何可用的認證（沒有 OAuth、`settings.json` 沒有 `apiKey`、也沒有 `ANTHROPIC_API_KEY` / `OPENAI_API_KEY`），畫面會提示你輸入 `/setup`。

設計背景：`docs/superpowers/specs/2026-11-19-setup-wizard-design.md`。

## 流程

1. **API 格式**：Anthropic（Messages API）或 OpenAI 相容（Chat Completions）。↑/↓ 選擇，Enter 下一步。
2. **端點與金鑰**：`Base URL` 依格式帶入預設值（可改）；`API key` 輸入時以 `•` 遮蔽，可以直接貼上。Tab 切換欄位。
   - 已經有金鑰時會顯示「leave empty to keep using …」，留空就沿用：
     - 全域 `settings.json` 的 `apiKey`（只有格式沒換時）。
     - 或環境變數 `ANTHROPIC_API_KEY` / `OPENAI_API_KEY`（依格式；這種情況不會把金鑰寫進檔案）。
3. **驗證**：呼叫 `GET {base}/v1/models`。失敗時回到第 2 步並顯示原因（unreachable / unauthorized / forbidden / timeout / HTTP 狀態），輸入內容保留。
   - OpenRouter 的模型清單是公開的，所以會另外打 `GET /api/v1/key` 檢查金鑰。
   - 其他端點只要列得出模型就算通過；有些端點不檢查金鑰，這種情況要等第一則訊息才會看到 401，屆時再跑一次 `/setup`。
   - 端點沒有模型清單（404、回傳格式不對、或清單是空的）時，改成手打模型 ID，可以用逗號分隔輸入多個。
4. **選模型**：直接打字即時過濾（不分大小寫）；↑/↓ 移動；**空白鍵勾選**（可以勾多個，換過濾條件也會保留）；Enter 下一步。一個都沒勾時，Enter 用游標所在的模型。模型清單有提供上下文長度時（例如 OpenRouter）會顯示在右側。
5. **分配角色**（勾選兩個以上才會出現）：主腦必填；lite / pro / max 可以選勾選的模型或「unbound」。↑/↓ 選角色，←/→ 換模型。
   - 會帶入目前的 `main:<模型>` 綁定。原本綁在 `main:X`、這次選 unbound 的等級，寫入時會**解除綁定**（畫面標示 “will be unbound”）。
   - 沒有改動的等級（原本就是 `main:X`、這次也選 X）完全不碰，等級上手寫的 `max_tokens` / `context_window` 會保留。
   - 綁在其他 provider（例如 `zai:glm-5.3-flash`）的等級不會被修改，只會顯示出來；要改請用 `/tier` 或手動編輯 `config.toml`。
6. **確認**：列出要寫入的內容（金鑰只顯示末 4 碼）與會蓋過設定的來源（見下方）。Enter 寫入，Esc 返回。
7. **完成**：請**重新啟動 glaude**，新設定才會生效（包括等級，避免會話中途出現新舊設定混用）。

任何一步按 Esc 都是回上一步；在第 1 步按 Esc 就關閉，什麼都不寫。

## 寫入哪些東西

**`~/.glaude/settings.json`**（只改這四個鍵，其他設定和順序保留；檔案權限 `0600`；原檔不是合法 JSON 時拒絕寫入）：

| 鍵 | 值 |
|----|----|
| `provider` | `direct`（Anthropic）或 `openai` |
| `baseUrl` | 第 2 步的 Base URL |
| `model` | 主腦模型 |
| `apiKey` | 有輸入新金鑰時才寫；留空沿用時不動 |

**`~/.glaude/config.toml`**（只在等級或 `[main]` 有變動時才寫；其他段落保留，但註解會遺失）：

- 分配到的等級寫成 `provider = "main"`、`model = "<模型>"`，沿用主腦的金鑰。模型清單有上下文長度時，一併寫入該等級的 `context_window`。
- 主腦模型換了：清掉舊的 `[main] context_window`；新模型有上下文長度、而且不在內建表（非 `claude-*`）時寫入新值。
- `config.toml` 無法解析時拒絕寫入；這時 `settings.json` 可能已經寫好，畫面會說明哪一個沒寫成。

## 「會蓋過設定」的警告

`/setup` 只寫全域設定。以下情況會在確認頁和完成頁警告，因為新的全域設定實際上不會被用到：

- 專案層 `.glaude/settings.json` 或本地層 `.glaude/settings.local.json`（舊位置 `.glaude.local/settings.json` 仍有效）設了 `provider` / `baseUrl` / `model` / `apiKey`。
- `managed-settings.json` 鎖定了這些欄位。
- 有設定以下環境變數：`CLAUDE_PROVIDER`、`API_PROVIDER`、`ANTHROPIC_API_KEY`、`OPENAI_API_KEY`、`ANTHROPIC_BASE_URL`、`OPENAI_BASE_URL`、`OPENAI_API_BASE`、`ANTHROPIC_MODEL`、`CLAUDE_MODEL`、`OPENAI_MODEL`。
- 已經用 OAuth 登入：主腦會優先用 OAuth，API key 只在未登入時使用。

## 已知限制

- 不需要金鑰的本機端點（例如 Ollama、LM Studio）：API key 欄位仍是必填，請隨意填一個值；而且因為 settings 裡有了 `apiKey`，啟動就不會再出現 /setup 提示。
- 換格式但沿用環境變數的金鑰時，`settings.json` 裡舊格式的 `apiKey` 會留著（目前環境變數優先，所以能用）。

## 不支援的部分

OAuth 登入、Bedrock / Vertex、在 `/setup` 裡註冊第二個 provider（請用 [`/provider`](provider.md)，或照 `docs/config.example.toml` 手動編輯 `config.toml`）、會話中途立即生效。
