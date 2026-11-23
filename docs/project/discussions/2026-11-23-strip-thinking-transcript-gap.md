# 切換服務商時清除 thinking，導致 transcript 漏寫訊息

> 日期：2026-11-23
> 狀態：**已修正**（分支 `fix/strip-thinking-transcript`，做法見文末「修正內容」）
> 發現於：確認「thinking 是否寫進 jsonl」時，順著 `/model` 切換的程式碼路徑追到

## 背景：thinking 有落地，而且應該維持

thinking 區塊會原文寫進 session jsonl，`signature` 也一起保存：

- `internal/engine/query.go` 的 `buildAssistantMessage` 保留 `Thinking` 和 `Signature`
- `internal/transcript/recorder.go` 的 `Record` 整則寫入 user／assistant 訊息，不過濾任何區塊
- OpenAI 相容端點的 `reasoning_content` 會先轉成 thinking 區塊（`internal/api/openai_stream.go`），一樣會落地

負責人決定**保留這個行為**，因為事後回看 thinking 才知道接下來要怎麼調整。所以修法不能靠「不寫 thinking」來繞過。

## 現象

透過 `/model` 或 `/provider` 切到別的服務商之後，下一輪的部分訊息**不會寫進 jsonl**。這時記憶體裡的對話沒有問題，畫面也正常，但之後 `/resume` 回來，歷史會缺一段。

## 機制（附出處）

1. **清除 thinking**：`SetMainTarget`（`internal/engine/engine.go`）遇到服務商改變時（`ProviderID` 不同，或 `StripThinking` 為 true），會呼叫 `stripThinking`，把歷史中所有 thinking 區塊拿掉。**只剩 thinking 的 assistant 訊息會整則刪除**。
   - `/model`：`internal/bootstrap/modelswitch.go` 的 `Apply` 會呼叫 `SetMainTarget`
   - `/provider`：`internal/bootstrap/provider.go` 的 `switchMain`，在格式或 BaseURL 改變時設定 `StripThinking`
2. **recorder 依索引記錄進度**：`Recorder.Record` 用 `r.persisted`（已寫入的訊息數）當基準，每次只寫 `msgs[persisted:]`。它不知道歷史中間有訊息被刪掉。
3. **錯位**：假設切換前歷史有 N 則、已全部寫入（`persisted = N`），刪掉 k 則後剩 N−k 則。下一輪新增 m 則以後：
   - 如果 N−k+m < N，`persisted` 會被重設成目前長度，**這一輪整輪都沒寫入**
   - 否則從索引 N 開始寫，**新增的前 k 則被跳過**

### 觸發條件

以下兩個條件要同時成立：

- 歷史裡有「只有 thinking、沒有 text 也沒有 tool_use」的 assistant 訊息。例如在 thinking 階段按 Esc 中斷，或在 thinking 期間碰到 max_tokens。
- 之後切換到別的服務商（同一服務商內換模型不會清除 thinking，不受影響）。

一般 assistant 訊息都會有 text 或 tool_use，所以平常碰不到，屬於邊角情況。但一旦發生，資料就**默默遺失**，沒有任何錯誤訊息。

## 重現（已驗證）

在 `internal/engine` 寫了暫時的測試，把 engine 和 recorder 串起來跑：

1. `SetMessages([u1, a1(只有 thinking), u2, a2])`，接著 `Record`
2. `SetMainTarget(ProviderID: "other")`，歷史變成 3 則
3. `SetMessages(append(歷史, u3, a3))`，接著 `Record`
4. 用 `session.Resume` + `ReadAll` 讀回 jsonl

```
history after switch: 3 messages
jsonl texts: [u1 [thinking] u2 a2 a3]
want:        [u1 [thinking] u2 a2 u3 a3]   ← u3 遺失
```

驗證完已刪除測試檔；修 bug 時要把它改寫成正式的回歸測試。

## 修法方向

### 建議：改在組請求時過濾，不修改歷史

- 歷史一律保留原樣，thinking 留在裡面，不再呼叫 `stripThinking` 修改 `e.messages`。
- thinking 區塊要記錄它是**哪個服務商產生的**（例如在 `types.ContentBlock` 或訊息層加上來源欄位，序列化時一併寫進 jsonl）。
- 組 API 請求時，把不是目前服務商產生的 thinking 區塊濾掉。濾完變成空的 assistant 訊息也只在請求裡省略。

好處：

- recorder 的索引不會再錯位，bug 從根本消失
- jsonl 和記憶體裡的歷史一致，thinking 完整落地
- 之後切回原本的服務商，thinking 和 signature 還能繼續用

要注意的地方：

- 濾掉 thinking 以後，請求裡可能出現連續兩則 user 訊息，要確認各服務商能不能接受，不行的話就合併。
- 舊的 jsonl 沒有來源欄位，要決定 resume 時怎麼看待（建議視為「未知來源」，只在第一次送出時跟目前服務商比對）。
- `compact` 和 `/btw` 這類會讀取或改寫歷史的路徑，要確認都經過同一個過濾點。

### 其他選項（不建議）

| 選項 | 做法 | 不建議的原因 |
|------|------|------|
| `/model` 改成開新 session | 切換時換 session ID 和 jsonl | 中途換模型接手同一件事是主要用途，開新 session 會斷掉脈絡；`/provider` 那條路徑一樣會清除 thinking；原版 Claude Code 也是沿用同一個 session |
| recorder 改用訊息身分（UUID）記錄進度 | 不依賴索引 | 能修好漏寫，但歷史和 jsonl 依然不一致，thinking 切回原服務商也回不來 |
| 只剩 thinking 的訊息保留為空殼 | 不刪整則，只清內容 | API 不接受 content 為空的 assistant 訊息，送出前還是要處理，等於把問題移到請求端 |

## 順帶觀察（尚未驗證影響）

- **`redacted_thinking` 的內容會遺失**：`buildAssistantMessage` 沒有處理 `redacted_thinking` 類型，`Data` 欄位沒有複製，寫進 jsonl 的只剩一個空的區塊。整個 `internal/` 都找不到處理 `redacted_thinking` 的程式碼。之後 resume，把這種區塊送回 Anthropic 時可能被拒絕，需要另外驗證。可以跟本問題一起修，因為都在「thinking 區塊的保存」範圍內。

## 相關檔案

- `internal/engine/engine.go`（`SetMainTarget`、`stripThinking`）
- `internal/engine/query.go`（`buildAssistantMessage`、組請求）
- `internal/transcript/recorder.go`（`Record` 的 `persisted` 基準）
- `internal/bootstrap/modelswitch.go`（`/model` 的 `Apply`）
- `internal/bootstrap/provider.go`（`/provider` 的 `switchMain`）
- `pkg/types/message.go`（`ContentBlock`）

## 修正內容（2026-11-23）

照「建議」方向實作：歷史不再被修改，thinking 只在組請求時過濾。

- **來源欄位**：`types.ContentBlock.ThinkingOrigin`（JSON `thinking_origin`），會寫進 jsonl。值是服務商 ID；有端點資訊時再加上 `Format + BaseURL` 的短雜湊（`zai@1a2b3c4d`），所以用 `/provider` 改了端點，舊的 thinking 也會失效。取代原本的 `MainTarget.StripThinking`，改成 `MainTarget.Endpoint`（`/model` 的 `Apply`／`TargetFor`、`/provider` 的 `switchMain` 會填）。
- **標記**：`streamResponse` 收到回應後，把 thinking 標上這次查詢的來源（`QueryParams.ThinkingOrigin`，主迴圈開跑時固定）。舊 jsonl 沒有來源，`SetMessages`（resume）時當成目前主目標產生的。
- **過濾點**：`thinkingForRequest`（`internal/engine/thinking.go`）會濾掉其他來源的 thinking，濾完變空的 assistant 訊息只在請求裡省略，前後兩則 user 會合併，讓角色維持交替。會用到它的有：
  - `buildRequest`（主迴圈、fallback、去圖重送）
  - auto compact 的摘要輸入
  - fork 快照 `CacheSafeParams.ContextMessages`（快照另外帶 `ThinkingOrigin`）
  - `/btw`（含 `--tier`）
- **wire 清理**：`types.WireContent` 在送出前清掉 `thinking_origin`。`buildRequest` 和 compact 的 `messagesToAPIParams` 都會經過這一步。
- **不變的地方**：用 tier client 的子代理（`QueryParams.Client` 有設定、來源未知）不過濾也不標記，跟以前一樣。它們的歷史從零開始，不含主對話的 thinking。
- **回歸測試**：`internal/engine/thinking_test.go` 的 `TestProviderSwitch_TranscriptKeepsEveryMessage` 把 engine 和 recorder 串起來，重現本文的情境。把舊的刪除行為塞回去時，這個測試會失敗（`jsonl = [u1 [thinking] u2 a2]`）。

`redacted_thinking` 沒有一起修：`internal/api` 的累加器和 `api.ContentBlock` 都沒有 `data` 欄位，要從串流解析那一層開始補，留給另一個分支處理。
