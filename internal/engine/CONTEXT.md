---
package: engine
import_path: internal/engine
layer: core
generated_at: 2026-11-25T16:47:52Z
source_files: [budget.go, cache_params.go, compaction.go, engine.go, forked_agent.go, msg.go, orchestration.go, query.go, stop_hooks.go, thinking.go, tool_hooks.go, tool_images.go]
---

# internal/engine

> Layer: **Core** · Files: 12 · Interfaces: 1 · Structs: 16 · Functions: 9

## Interfaces

### QueryEngine (7 methods)
> QueryEngine is the top-level interface exposed to the TUI/SDK layer.

```go
type QueryEngine interface {
    Query(ctx context.Context, params QueryParams) (<-chan Msg, error)
    Interrupt(ctx context.Context)
    GetMessages() []types.Message
    SetMessages(messages []types.Message)
    SetModel(model string)
    MainTarget() MainTarget
    SetMainTarget(t MainTarget)
}
```

## Structs

- **BudgetTracker** — 4 fields: ContinuationCount, LastDeltaTokens, LastGlobalTurnTokens, StartedAt
- **CacheSafeParams** — 8 fields: SystemPrompt, ContextMessages, ToolUseContext, Model, Client, ContextWindow, MaxTokens, ThinkingOrigin
- **Config** — 10 fields: Client, Registry, Model, MaxTokens, PermissionChecker, StopHooks, ToolHooks, MsgQueue, ...
- **ForkUsage** — 13 fields: QuerySource, Model, SessionID, WorkingDir, Start, End, Outcome, APIRequests, ...
- **ForkedAgentConfig** — 7 fields: PromptMessages, CacheSafeParams, QuerySource, MaxTurns, AllowedTools, OnMessage, OnUsage
- **MainTarget** — 6 fields: Client, ProviderID, Model, ContextWindow, MaxTokens, Endpoint
- **Msg** — 21 fields: Type, TextDelta, ToolUseID, ToolName, InputDelta, ToolInput, ToolResult, AssistantMsg, ...
- **PermissionRequestMsg** — 6 fields: RequestID, ToolUseID, ToolName, Message, Input, RespFn
- **QueryParams** — 22 fields: Messages, SystemPrompt, UserContext, SystemContext, ToolUseContext, QuerySource, MaxOutputTokensOverride, MaxTurns, ...
- **StopHookContext** — 6 fields: Messages, ToolUseContext, QuerySource, IsBareMode, Engine, CacheParams
- **StopHookRegistry** — 2 fields
- **SystemPrompt** — 1 fields: Parts
- **SystemPromptPart** — 2 fields: Text, CacheControl
- **TaskBudget** — 1 fields: Total
- **TokenBudgetDecision** — 8 fields: Action, NudgeMessage, ContinuationCount, Pct, TurnTokens, Budget, DiminishingReturns, DurationMs
- **ToolResultMsg** — 3 fields: ToolUseID, Content, IsError

## Function Types

- `StopHookFn` — `func(ctx context.Context, hookCtx *StopHookContext)`

## Functions

- `CreateCacheSafeParams(params QueryParams, messages []types.Message) *CacheSafeParams`
- `LoadCacheSafeParams() (*CacheSafeParams, error)`
- `LoadCacheSafeParamsFrom(filePath string) (*CacheSafeParams, error)`
- `New(cfg Config) QueryEngine`
- `NewBudgetTracker() *BudgetTracker`
- `NewStopHookRegistry() *StopHookRegistry`
- `RunForkedAgent(ctx context.Context, eng QueryEngine, cfg ForkedAgentConfig) ([]types.Message, error)`
- `SaveCacheSafeParams(params *CacheSafeParams) error`
- `SaveCacheSafeParamsTo(params *CacheSafeParams, filePath string) error`

## Constants

- `CompletionThreshold`
- `DefaultMaxToolUseConcurrency`
- `DiminishingThreshold`
- `MainProviderID`
- `MaxContinuations`
- `MsgTypeAssistantMessage`
- `MsgTypeCompactEnd`
- `MsgTypeCompactStart`
- `MsgTypeError`
- `MsgTypePermissionRequest`
- `MsgTypePermissionResponse`
- `MsgTypeProgress`
- `MsgTypeRequestStart`
- `MsgTypeStreamRequestStart`
- `MsgTypeStreamText`
- `MsgTypeSystemMessage`
- `MsgTypeThinkingDelta`
- `MsgTypeTombstone`
- `MsgTypeToolResult`
- `MsgTypeToolUseComplete`
- `MsgTypeToolUseInputDelta`
- `MsgTypeToolUseStart`
- `MsgTypeTurnComplete`
- `MsgTypeUserMessage`

## Change Impact

**Test Mocks (must add new methods when interfaces change):**
- `stubQueryEngine` in `internal/bootstrap/wire_fork_test.go`

**Exported type references (files that use types from this package):**
- `CacheSafeParams` → `internal/bootstrap/tiers_test.go` (test), `internal/bootstrap/wire.go`, `internal/bootstrap/wire_fork_test.go` (test), `internal/memdir/dream.go`, `internal/tui/btw.go` + 2 more
- `Config` → `internal/bootstrap/wire.go`
- `ForkUsage` → `internal/bootstrap/fork_usage_test.go` (test), `internal/bootstrap/wire.go`, `internal/memdir/dream.go`, `internal/memdir/extract.go`
- `ForkedAgentConfig` → `internal/memdir/dream.go`, `internal/memdir/extract.go`, `internal/memdir/session_memory.go`
- `MainTarget` → `internal/bootstrap/modelswitch.go`, `internal/bootstrap/modelswitch_test.go` (test), `internal/bootstrap/provider.go`, `internal/bootstrap/soul_subagent_test.go` (test), `internal/bootstrap/tiers_test.go` (test) + 9 more
- `Msg` → `internal/bootstrap/acp_integration_test.go` (test), `internal/bootstrap/run.go`, `internal/bootstrap/tiers_test.go` (test), `internal/bootstrap/wire.go`, `internal/bootstrap/wire_fork_test.go` (test) + 11 more
- `QueryEngine` → `internal/bootstrap/modelswitch.go`, `internal/bootstrap/provider.go`, `internal/bootstrap/wire.go`, `internal/memdir/dream.go`, `internal/tui/cmds.go` + 2 more
- `QueryParams` → `internal/bootstrap/acp_integration_test.go` (test), `internal/bootstrap/run.go`, `internal/bootstrap/wire.go`, `internal/bootstrap/wire_fork_test.go` (test), `internal/tui/btw.go` + 3 more
- `StopHookContext` → `internal/bootstrap/wire.go`, `internal/memdir/dream.go`, `internal/memdir/extract.go`, `internal/memdir/extract_test.go` (test), `internal/memdir/extract_throttle_test.go` (test) + 1 more
- `StopHookRegistry` → `internal/bootstrap/wire.go`
- `SystemPrompt` → `internal/bootstrap/run.go`, `internal/bootstrap/wire.go`, `internal/bootstrap/wire_fork_test.go` (test), `internal/tui/btw.go`, `internal/tui/btw_test.go` (test) + 1 more
- `SystemPromptPart` → `internal/bootstrap/run.go`, `internal/bootstrap/wire.go`, `internal/bootstrap/wire_fork_test.go` (test), `internal/tui/btw_test.go` (test), `internal/tui/cmds.go`
- `ToolResultMsg` → `internal/tui/tui_test.go` (test)

## Dependencies

**Imports:** `internal/api`, `internal/compact`, `internal/hooks`, `internal/msgqueue`, `internal/permissions`, `internal/tools`, `pkg/types`

**Imported by:** `internal/bootstrap`, `internal/memdir`, `internal/tui`

<!-- AUTO-GENERATED ABOVE — DO NOT EDIT -->
<!-- MANUAL NOTES BELOW — preserved across regeneration -->

## Design Notes

- **2026-11-18 内核搬回（反转 PR #31）**：本包原于 2026-09-23 逐字提取至外部仓 github.com/tunsuy/agtkeel；因该仓停止维护，经人类决策整併回本仓原始路径。来源：agtkeel main 3a77085＋后续提交，零逻辑改动（仅 import 路径恢复＋gofmt）。内核改动自此直接在本仓进行。
- **2026-11-18 cache 斷點佈局＋fork 快照＋inbox（feat/coordinator-fork-cache）**：buildRequestWithModel 確定性三處斷點——system（顯式 part 標記優先；無標記則掛末段，一個斷點快取整個穩定段）、tools 末位（會話內釘住）、messages 滾動斷點（最後 user message 末 block，clone 後標記不污染歷史）。SkipCacheWrite 抑制全部。runQueryLoop 於 turn 邊界（壓縮後、inbox drain 後）快照 CacheSafeParams（+Model）至 UseContext.CacheParams，供 Agent tool spawn fork 時繼承——快照與請求前綴一致，fork 前綴 byte-identical（守護測試 TestForkPrefix_ByteIdentity；訊息比對需排除 cache_control 標記，標記是 write-position metadata、provider 前綴匹配忽略其位置）。QueryParams.InboxCh 於每個 turn 邊界 non-blocking drain，以 <coordinator-message> 包裝追加尾部（前綴不動）。
- **2026-11-18 實機驗證修復：Interrupt slot 不被 background query 劫持（fix/agent-lifetime）**：Query 原無條件覆蓋 e.abortFn——sub-agent/fork 的 Query 跑在同一 engine 上會劫持 Interrupt（Ctrl+C 取消到 background query 而非主對話）。現仅 write-back（foreground）query 擁有 interrupt slot；NoWriteBack query 經自身 ctx 取消（coordinator cancelFn／fork ctx）。守護測試 TestInterrupt_NotHijackedByBackgroundQuery。
- **2026-11-19 streamResponse 接上 WithRetry（feat/api-rate-control）**：e.client.Stream 呼叫包 api.WithRetry（Model/QuerySource from req）——429/5xx/網路錯誤指數退避重试（retry-after 優先），401 fail-fast（無 refresh hook）。此前 WithRetry 僅測試使用、生產路徑 429 直接失敗（使用者 qwen 上限痛點）。守護測試 retry_wiring_test.go×2（429→重试→成功；401→單次失敗）；TestQuery_StreamError_EmitsErrorMsg 改用非重试 400（通用網路錯誤現會重试，TS parity）。
- **2026-11-19 thinking 顯示鏈守護（feat/openai-thinking-display）**：engine 既有 thinking_delta→MsgTypeThinkingDelta 路徑原僅 Anthropic 路徑觸及；OpenAI reasoning_content 現經 api 層轉譯為 thinking 事件後同鏈生效（零 engine 改動）。新增守護測試 thinking_display_test.go（thinking delta 事件＋assistant message thinking block 累積）。
- **2026-11-19 `QueryParams.Client` 與壓縮依查詢目標（provider 等級，做法 C）**：
  - `QueryParams.Client` 有值就用它發串流請求（`clientFor`），否則用 engine 預設 client；主腦與 fork 永遠不帶，前綴與 provider 不變（`TestQuery_MainLoopRequestUnchangedWithoutTiers` 以 golden 位元組把關）。`ContextWindow`、`CompactTracker` 同為每次查詢的設定；max_tokens 沿用既有 `MaxOutputTokensOverride`（C11）。
  - **修正前行為（spec §6）**：sub-agent 觸發自動壓縮時用主腦的 client 與啟動時的模型（費用算在主腦模型上），門檻也用 `e.model`；全 engine 共用一份壓縮狀態。現在壓縮管線（`compaction.go` `runCompaction`）依 `targetFor(params)` 計算門檻並在該 target 的 client/model 上摘要；sub-agent 壓縮在 `--debug` 時記一行 log。
  - C8：門檻估算＝最近一次 API 回報的 prompt tokens（input+cache read+cache creation）＋之後新增的訊息；沒有 usage 或歷史被改寫過時，退回 JSON÷4 估算並加上 system prompt 與工具 schema。**主腦也一起改**，所以主腦會比修正前早一點觸發壓縮——負責人認可的修正，是反目標「主腦行為不變」唯一的例外。
  - C5：`SetModel` 加 `modelMu`；摘要模型在呼叫當下解析，`/model` 後立即生效。C6：帶 `Client` 的查詢忽略 `FallbackModel`（它指的是主腦 provider 上的模型）。`[main] context_window` 經 `Config.MainContextWindow` 只套用在跑主腦模型、未帶 Client 的查詢。
- **2026-11-20 `MainTarget`（/model 切換主腦）**：主腦的 client、ProviderID、model、context window、max_tokens 合成 `MainTarget`，以 `mainMu` 保護；`SetMainTarget` 讓 /model 在會話中換到別的 provider。`runQueryLoop` 開始時取一次快照，整輪不換（切換只影響下一個查詢）。換 provider 時移除歷史中的 thinking 區塊（簽章只對原 provider 有效，切換本來就讓快取失效）；被中斷的查詢稍後寫回時若主腦已換 provider 會再清一次，鎖順序固定為 `e.mu → mainMu`。`CacheSafeParams` 記錄請求的 client、context window、max_tokens，讓 fork 與它的續跑停在原本的 provider。沒有切換時主腦請求位元組不變（golden 測試）。
- **2026-11-20 /btw 開關與佇列修正**：`QueryParams` +`ToolFilter`（執行時過濾，送出的工具 schema 不變以保住快取）、`NonInteractivePermissions`（只用 `CanUseTool`，Ask 視為 Deny，不跳對話框也不記錄拒絕）、`NoCompaction`。開關經 `UseContext` 的**私有副本**傳到 orchestration（絕不改呼叫端的 UseContext；呼叫端沒給時也會建立，避免失效放行）。修正：只有主腦（`NoWriteBack=false`）會在工具邊界接收主腦佇列訊息，sub-agent、fork、記憶抽取、btw 不再搶走它們。快照除了寫入 `CacheParams`，也寫進 `UseContext.Snapshot`（mutex 保護），讓 TUI 從別的 goroutine 安全讀取。
- **2026-11-20 tool result 保留圖片 + 不支援視覺時降級（feat/tool-result-images）**：此前 `executeTool` 把 `MapResultToToolResultBlock` 的 `content` 解析成字串，Read 讀圖時 content 是陣列，解析失敗後退回把整段原始 JSON（含 base64）當文字送出——模型看不到圖，token 卻照算（實測 GLM 只好自己跑 Python 解 PNG）。現在 `toolResultContent`：字串 content 照舊一個 text block；**含圖片的陣列**原樣保留成 blocks（Anthropic 路徑直接送 image，OpenAI 路徑由 api 層轉 image_url）；其他形狀維持舊的原始 JSON 退路，既有工具的位元組不變。TUI 的 ToolResultMsg 顯示 `[image image/png, N KB]`，不帶 base64。降級：串流回 `api.IsImageUnsupportedError` 且本輪訊息帶圖、該模型尚未標記時，`imageRejected`（sync.Map，key 為模型 ID，session 內有效）記下模型、送 system 提示，`buildRequest` 對已標記模型一律以 `stripImages` 把圖片換成 `imageOmittedText` 後重送一次；之後每輪都在送出前先過濾，不會每輪白打一次 400。`stripImages` 只回傳副本，不改共用 history（切回支援視覺的模型時圖片仍在）。守護測試 tool_images_test.go（含 TestQueryLoop_ImageRejectedRetriesWithoutImages）。Cache-first：無圖對話前綴不變；已標記模型的請求從第一次降級起前綴固定；不新增呼叫，僅在 400 時重送一次。
- **2026-11-20 工具邊界注入貼上快照**：`drainMidTurn` 只讀 `QueuedCommand.Content`，不讀 paste store。每一則 Content 都是 nil 時，引擎訊息仍是 `strings.Join(values, "\n\n")` 的單一文字 block。任一則有快照時依佇列順序拼接：快照的 blocks 原樣附上；`OriginBTW` 或 nil 快照則是一個 Value 文字 block；項目之間另放一個內容為 `"\n\n"` 的文字 block，不跨則合併圖片。回給 TUI 的 `MsgTypeUserMessage` 永遠是接合後的 Value（標記），引擎的 messages 才帶展開後的內容。快照掛在這一則使用者訊息（turn 尾部），不移動 system 前綴。
- **2026-11-23 max_tokens 提示改為「從中斷處續寫」（fix/repetitive-replies）**：`buildNudgeMessage` 原本要求「wrap up … provide a final response」。被截斷的回應仍留在歷史與畫面上，模型收到「給最終回應」後會**從頭重寫整段答案**（實機 transcript：第二段與第一段相似度 68%，使用者讀兩遍）。現在三段提示都要求從截斷處直接接著寫、不道歉、不重述、不重來，並把剩餘工作切小。注入位置不變（turn 尾部的 user 訊息），不移動前綴、不新增呼叫。守護測試 TestBudgetTracker_NudgeMessages 斷言每段提示都禁止重複且不要求 final response。計數問題見下一條。
- **2026-11-23 max_tokens 復原三修（fix/repetitive-replies）**：(1) **計數**：`budget.Check` 要的是累計 output tokens，`runQueryLoop` 卻傳單輪值，第二次截斷時 delta≈0 被判成 diminishing returns，查詢**靜默結束**。現在 loop 累計 `totalOutput` 再傳入，且只在 max_tokens 分支呼叫。(2) **無 usage 的 provider**：舊碼在 usage 未回報時 Check 回 continue 卻沒有提示訊息，loop 以 assistant 結尾**無限重送**（守護測試在舊碼 5 秒內打出 4623 個請求）。新增 `BudgetTracker.RecoverFromMaxTokens`：stop reason 為準，每次截斷都計入 continuation，必定回傳 stop 或 nudge，上限 `MaxContinuations`；停止時送 system 訊息告知使用者，不再無聲結束。(3) **被截斷的工具呼叫**：max_tokens 回應中的 tool_use 從未執行，留在歷史會讓下一個請求因「tool_use 無 tool_result」被兩種 API 拒絕（即使 loop 停止，使用者下一句也會失敗）。現在不論繼續或停止都先以 `dropToolUse` 移除（只剩 tool_use 時補 `cutOffText` 避免空訊息），nudge 附 `cutToolCallNote` 請模型改小後重發。Cache-first：只動 turn 尾部（被截斷的 assistant 訊息與 nudge），不移動前綴；不增大窗口；呼叫次數上限不變，且修掉無限呼叫。守護測試 max_tokens_test.go（三案例皆在舊碼上失敗）。
