# PreToolUse / PostToolUse Hooks — 設計

日期：2026-11-24　分支：`feat/tool-hooks`

## 目的

讓使用者在 settings.json 設定的工具 hook 真正生效，語意對齊原版 Claude Code，
使既有的 Claude Code hook 設定可直接搬到 glaude 使用。

現況：`internal/hooks` 有 Dispatcher，但 `wire.go` 傳入 `nil`，設定從未生效；
只有 PreToolUse 一個呼叫點（在權限 checker 內，只能擋）；matcher 未實作；
結束碼語意、updatedInput、PostToolUse 全無。

## 範圍

- 做：PreToolUse、PostToolUse、PostToolUseFailure（工具失敗時的 post 事件，原版行為）。
- 不做：其他事件（SessionStart、Stop、Notification…）、PermissionRequest hook、
  `type: "prompt"` 型 hook、hook 設定熱重載（原版也是啟動時快照）。

## 設定格式（與原版相同）

```json
{
  "hooks": {
    "PreToolUse": [
      { "matcher": "Bash|Write", "hooks": [ { "type": "command", "command": "~/bin/check.sh", "timeout": 30 } ] }
    ]
  }
}
```

- `matcher`：空字串或 `*` 匹配全部；只含字母、數字、`_`、`-`、空格、`,`、`|` 時視為以 `|` 或 `,` 分隔的精確名稱清單（前後空白忽略）；其餘當正規表示式（未錨定）。無效 regex 會警告並略過。
- `timeout`：秒，預設 600（同原版 command hook）。
- 其他 command 欄位：`args`（不經 shell 直接執行）、`shell`（`bash` 預設，找不到時用 `sh`；`powershell`）、`if`（權限規則語法，如 `Bash(git *)`，用 checker 的規則比對）、`onFailure`（`block` 時 hook 本身失敗也算阻擋）、`async`（背景執行、不等結果）。`asyncRewake` 當成 `async`；`statusMessage`、`once` 接受但不作用（原版在 settings 裡也忽略 `once`）。
- 不支援的類型（`http`、`prompt`、`agent`、`mcp_tool`）與無法使用的欄位：該 hook 略過，第一次有工具呼叫時以系統訊息警告一次。
- `onFailure: "block"`：hook 崩潰、逾時、或以 0/2 以外的結束碼結束（即使有 JSON）都算阻擋。
- 舊的扁平格式（`{command, matcher, timeout_ms}`）從未生效，直接移除。
- 各設定層 append 合併；`disableAllHooks` 全關；`allowManagedHooksOnly` 為 true 時只用 policy 層的 hooks。

## 執行

- 同一事件所有匹配的 command 並行執行，相同 command 字串去重。
- `sh -c <command>`，工作目錄 = session cwd，環境變數加 `CLAUDE_PROJECT_DIR` 與 `GLAUDE_PROJECT_DIR`。
- stdin JSON：`session_id`、`transcript_path`、`cwd`、`permission_mode`、`hook_event_name`、
  `tool_name`、`tool_input`、`tool_use_id`；Post 加 `tool_response`；Failure 加 `error`、`is_interrupt`。

### 結果判讀（同原版）

| 情況 | 意義 |
|---|---|
| exit 2 | 阻擋：stderr 回饋給模型（Pre＝拒絕這次呼叫，即使 JSON 寫 allow；Post＝附在工具結果後，工具已執行）。stdout JSON 的共通欄位（systemMessage、continue）仍會讀 |
| 其他結束碼且 stdout 是合法 JSON 物件 | 由 JSON 欄位決定，忽略結束碼 |
| exit 0 且 stdout 是一般文字 | 無作用 |
| 其他（exit 1 等、逾時、無法執行、JSON 壞掉） | 非阻擋錯誤，只提示使用者；`onFailure: "block"` 時改為阻擋 |

stdout JSON 欄位：`continue`（false＝這次呼叫照常執行，之後同一批的工具全部跳過並停止對話迴圈，`stopReason` 顯示給使用者；**不會**擋掉這次呼叫，要擋需另回 deny）、
`systemMessage`（提示使用者）、`decision`/`reason`（舊式：approve/block）、
`hookSpecificOutput`：
- Pre：`permissionDecision`（allow/deny/ask/defer）、`permissionDecisionReason`、`updatedInput`、`additionalContext`
- Post：`additionalContext`、`updatedToolOutput`（任何工具）、`updatedMCPToolOutput`（只對 `mcp__` 工具）

多個 hook 的 Pre 決策優先序：deny > ask > allow；`defer` 視為沒有決定（交回其他 hook 與一般權限流程，不會蓋掉別的 hook 的 ask）；`updatedInput` 取最後一個。
`additionalContext` 以 `<system-reminder>` 包住附在工具結果後送給模型。
背景子程序握住 pipe 不影響結果判讀（最多再等 1 秒）。hook 留在 glaude 的 process group：可以使用終端機，也會跟著 Ctrl+C 結束；代價是逾時時只砍得到直接子行程。

## 接入點

1. **engine/orchestration.go `executeOneTool`**：輸入驗證、ExecFilter 之後，權限檢查之前跑 PreToolUse。
   - deny → 不執行，理由回給模型。
   - updatedInput → 替換輸入，並重新套用 ValidateInput、ExecFilter（/btw）與並行安全檢查。
   - hook 執行中被中斷 → 不執行工具。
   - allow → 只省略模式的預設詢問；deny 規則、ask 規則、plan 模式、工具自身安全檢查仍然生效（同原版：deny 與 ask 規則不論 hook 回什麼都會評估）。
   - ask → 一定詢問使用者（即使規則允許／bypass 模式）；非互動模式視為拒絕；deny 規則仍優先。
   - 工具執行後依成功／失敗跑 PostToolUse／PostToolUseFailure，回饋文字附加到 tool_result。
   - `continue:false` → 這次呼叫照常執行；之後的工具呼叫都回「skipped」tool_result（維持 tool_use/tool_result 成對），本批結果寫入對話後停止迴圈。
2. **permissions checker**：移除內部的 PreToolUse 呼叫與 `Dispatcher` 欄位；新增 `PermissionRequest.ForceAsk`／`HookReason`（hook ask）與 `HookAwareChecker`（`CanUseToolHookAllowed`：hook allow 時的決策鏈；`MatchRule`：`if` 欄位比對）。
3. **CodeMode 內部工具呼叫**：同樣套 Pre（deny/ask/allow/updatedInput）與 Post。hook 的通知、additionalContext、Post 回饋寫進執行紀錄（`[hook]` 開頭，回給模型）；Post 不會把已執行的呼叫變成錯誤；`continue:false` 讓這次呼叫完成，之後的內部呼叫失敗，CodeMode 結束後主迴圈也停止（`tools.Result.HookStopReason`）。
4. **ACP 外部 agent 的權限請求**：不跑 hook（工具不由 glaude 執行）。
5. **session 資訊**：`UseContext` 新增 `SessionID`、`TranscriptPath`，由 TUI／headless 填入。
6. **使用者提示**：systemMessage、非阻擋錯誤、stopReason 用 `MsgTypeSystemMessage` 顯示。

## 我替你做的決定

- Post 回饋以文字區塊附在同一個 tool_result 後（原版是緊鄰工具結果的附件，模型看到的內容相同）。
- CodeMode 內的 hook 訊息走執行紀錄，不另外發系統訊息（沙箱內拿不到 TUI 訊息通道）。
- ACP 外部 agent 的工具不跑 hook（工具不由 glaude 執行）。
- 不支援：`asyncRewake` 的喚醒行為、`statusMessage` 顯示、http/prompt/agent/mcp_tool 類型。
