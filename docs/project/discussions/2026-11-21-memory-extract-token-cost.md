# 記憶萃取（extract_memories）每輪重送整段歷史：token 成本問題

> 日期：2026-11-21
> 狀態：**已定案，實作中**（分支 `fix/memory-extract-cost`；決策與計畫見文末，2026-11-25）
> 發現於：`fix/openai-tool-arguments`（同分支修了萃取 agent 觸發的 400 與 log 蓋畫面）

## 現象

使用者用 OpenAI 相容 provider（DashScope／Qwen coder）時，TUI 狀態列被這段錯誤蓋掉：

```
memdir: extract memories: engine: forked agent: engine: stream: API error 400:
"The \"function.arguments\" parameter of the code model must be in JSON format."
```

400 本身已在 `fix/openai-tool-arguments` 修掉（`normalizeToolArguments`）。追查時發現另一個更大的問題：**萃取在每一輪都會觸發，而且每次都把整段對話歷史重送一遍**。

## 機制（附出處）

1. **觸發點**：`internal/engine/query.go` 的 stop reason 是 `end_turn`／`stop_sequence`／`""` 時，會呼叫 `fireStopHooks`，讓每個 stop hook 在背景 goroutine 跑。
   `extract_memories` 只有遇到以下情況會跳過（`internal/memdir/extract.go`）：
   - 設定停用（`autoMemoryEnabled: false`）
   - bare 模式
   - `QuerySource` 不是 `foreground`
   - 這一輪主模型已經寫過 `MemoryWrite`
   - 摘要是空的

   **沒有節流**，也就是使用者每完成一輪，背景就多跑一個 forked agent。
2. **送了什麼**：`fireStopHooks` 用 `CreateCacheSafeParams(params, messages)` 建立 CacheParams，`ContextMessages` 是**完整的 messages**（`internal/engine/cache_params.go`）。
   `RunForkedAgent` 會把 `ContextMessages` 和 `PromptMessages` 串起來送出，所以一次請求的內容是：
   ```
   system prompt + 整段對話歷史 + BuildExtractionPrompt(最近 20 則訊息的摘要，≤4000 字)
   ```
   摘要涵蓋的範圍跟歷史尾端重複。
3. **回合數**：`MaxTurns: 3`。萃取 agent 呼叫 Memory* 工具後會再進下一回合，整包內容最多重送 3 次。
4. **prompt cache 的前提不成立**：原本的設計靠跟主對話共用前綴、命中快取來壓低成本，但目前：
   - `ExcludeTools` 把工具清單縮成 3 個 Memory* 工具，request 前綴（tools）已經跟主對話不一樣了。
   - OpenAI 相容 provider 是隱式前綴快取，命中規則各家不同，無法保證。

**粗估**：快取沒命中時，每輪 input token 大約是原本的 2～4 倍，對話越長越明顯。這是推算，**沒有實測**，處理前應先量一次（`--debug` 或 provider 的帳單／usage）。

`auto_dream` 也用同樣的 CacheParams，但有鎖檔節流（`minHoursBetweenDream = 24`），影響小。

## 緩解（使用者側，立即可用）

```json
{ "autoMemoryEnabled": false }
```

這個設定會同時關掉 extract 和 dream。

## 修法選項（待決定）

| 選項 | 做法 | 優點 | 缺點 |
|------|------|------|------|
| A. 只送摘要 | 萃取 agent 的 messages 只放 extraction prompt，不帶 `ContextMessages` | 最省，每次成本固定 | 只看得到最近 20 則、≤4000 字；跟主對話的快取共用徹底放棄 |
| B. 保留歷史＋節流 | 每 N 輪，或新增內容超過門檻才跑；同時拿掉重複的摘要 | 看到完整脈絡 | 仍然昂貴；要設計節流狀態（跨輪計數） |
| C. A＋節流 | 只送摘要，再加最低間隔 | 最省 | 可能漏掉短暫提及的資訊 |

建議方向：A 或 C。萃取只需要「最近發生了什麼」，摘要本來就是為這個設計的；整段歷史只是為了快取命中才帶，而快取在這裡已經不成立。

## 順帶查證（不是 bug）

最初懷疑 `fireStopHooks` 沒帶 `Model`／`Client`，會讓萃取 agent 跑在別的模型上。查證後**不成立**：
- 萃取只從主迴圈（`foreground`）觸發。
- forked query 的 `Model` 留空時，用的是當下的 `MainTarget`，跟剛結束的那一輪相同。
- 只有在「這一輪結束」到「fork 開始」之間剛好有人執行 `/model`，才可能不一致，可以忽略。

## 相關檔案

- `internal/engine/query.go`（`fireStopHooks` 觸發點）
- `internal/engine/stop_hooks.go`、`internal/engine/cache_params.go`、`internal/engine/forked_agent.go`
- `internal/memdir/extract.go`、`internal/memdir/dream.go`
- `internal/bootstrap/wire.go`（`registerMemoryStopHooks`）

## 實測補充（2026-11-25，rs-stream session `1795612667200-4f8738e23eae8d90`）

- **萃取確實每輪都在跑**：rs-stream 的 memory 目錄在 22:20、22:25、22:30、22:33、22:37 都有寫入，時間跟各輪結束對得上；22:37 那次在最後一輪結束之後。
- **fork 的用量完全沒有記錄**：`turn-timings.jsonl` 只記前景那一輪，`RunForkedAgent` 沒有累計 usage。
- **前景實際用量**（14 輪、51 個 request、glm-5.4）：沒命中快取的 input 0.75M，cache read 3.1M。
- **fork 推算**：每輪的歷史從約 27k 長到 107k token（用「jsonl 字元數 ÷ 3」估），一個 session 共 1.0M～3.1M。如果完全沒命中快取，沒命中快取的 input 大約是原本的 **2.3～5 倍**。
- **另一個 bug**：`buildConversationSummary` 取最後 20 則訊息後，從最舊的往新的累加，超過 4000 字就停，結果截掉的是**最新**的內容。
- **另一個 race**：`MemoryStore` 完全沒有鎖。主對話的 `MemoryWrite`、萃取、dream，以及同一個專案的其他 glaude process，可能同時改檔；`BuildIndex` 會整份重寫 `MEMORY.md`。

## 決策（2026-11-25）

考慮過三種寫入模型：

| 模型 | 結論 | 理由 |
|------|------|------|
| fork 只能提議，由主 loop 審核 | ✗ | 沒有人能有效審核：交給主模型要多打一次 LLM，交給使用者會太煩，用程式規則判斷則可以直接放在寫入層 |
| fork 的寫入一律交給主 loop 代寫（透過 channel） | ✗ | 解決不了跨 process 的 race；跟 bubbletea 和結束時的清空綁在一起；fork 拿不到真正的寫入結果 |
| **fork 直接寫，store 層加鎖讓寫入排隊；channel 只回報事件** | ✓ | 一致性在 store 層解決，連跨 process 都涵蓋；fork 拿得到真實結果；主 loop 透過事件看得到狀態和錯誤 |

另外：
- fork 改用**跟主對話相同的工具清單**，在執行階段用 `ToolFilter` 只放行 Memory* 工具，讓 request 前綴跟主對話一致，才有機會命中快取。
- 在 `UseContext` 加明確的 `QuerySource` 欄位，用來標記來源，不用 `ctx.Value`。這個欄位只用來標記和過濾工具，**不用來阻擋寫入**。
- 節流照樣要做。相同工具清單只讓單次萃取變便宜，不會減少萃取次數。

## 實作計畫

四個 PR，依序做。每一步都用 turn-timings 比較前後差異。

### PR 1：量測（先做，其他步驟都靠它驗證）
- `RunForkedAgent` 加總 channel 上每個 `MsgTypeTurnComplete` 的 usage，連同 request 次數一起回傳。
- `ExtractMemoriesConfig`／`AutoDreamConfig` 加 `OnUsage` 回呼，由 bootstrap 接上。
- `turnRecord` 加 `query_source` 欄位（前景是 `foreground`，舊資料沒有這個欄位時視為 foreground）。fork 的紀錄寫進同一個 `turn-timings.jsonl`。
- 在 rs-stream 跑一段 session，留下**基準線**。

### PR 2：相同工具清單加執行階段過濾
- `ForkedAgentConfig.AllowedTools` 改成產生 `ToolFilter`，不再產生 `ExcludeTools`，工具清單維持跟主對話相同。被擋下的工具回傳明確的原因。
- `UseContext` 加 `QuerySource`，由 engine 在建立 `UseContext` 時填入。
- 萃取用的 prompt 明確寫「只能用 MemoryRead/MemoryWrite/MemoryDelete」，避免浪費回合。
- 測試：fork 的 request 工具清單跟主對話相同；fork 呼叫 Bash 會被擋。
- **再量一次，這個結果決定 PR 4 要送什麼**：
  - fork 的 cache read 佔總 prompt ≥ 80%：保留整段歷史。
  - 否則改成只送游標之後的新訊息（選項 C）。

### PR 3：store 一致性加上 MemoryEvents
- `MemoryStore` 加 `sync.Mutex`，`WriteMemory`／`DeleteMemory`／`BuildIndex` 放進同一個臨界區。
- 跨 process 用檔案鎖（memory 目錄裡的 `.lock`，`flock`）。
- `MEMORY.md` 和各記憶檔改成先寫暫存檔、再 rename。
- `UseContext` 注入 `MemoryEvents` 介面（例如 `Emit(MemoryEvent)`）。memory 工具只依賴這個介面，符合工具之間不互相 import 的規則。
- TUI 接收事件：狀態列顯示「背景記住了 X」或錯誤，取代現在只寫進 `logf`、使用者看不到的狀況。
- 測試：多個 goroutine 同時寫；兩個 store 實例（模擬兩個 process）同時重建 index。

### PR 4：節流和萃取內容
- 加一個游標：記錄上次萃取到第幾則訊息（每個 session 一份，用 mutex 保護）。
- 觸發條件：游標之後新增的文字 ≥ 2000 字，**或**距離上次已過 5 輪。這兩個數字先寫成常數，要不要做成設定，看量測結果再決定。
- compaction 之前補跑一次，因為 compaction 後舊歷史就不見了。session 結束時是否補跑先不做，列為後續。
- 依 PR 2 的結論決定送整段歷史還是只送新增部分。
- 修 `buildConversationSummary`：改成從最新的往回取，優先保留最新的內容。
- 量測：跟 PR 1 的基準線比，目標是萃取相關的沒命中快取 input 降到前景的 ≤ 20%。

### 收尾
- 修完後從 `.claude/CLAUDE.md` 的「待處理問題」移除這一項，本文狀態改成「已完成」。
