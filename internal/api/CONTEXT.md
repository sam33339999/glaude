---
package: api
import_path: internal/api
layer: services
generated_at: 2026-11-22T15:05:08Z
source_files: [accumulate.go, client.go, debug_logger.go, errors.go, factory.go, json.go, listmodels.go, openai_client.go, openai_images.go, openai_stream.go, openai_tool_schema.go, openai_types.go, ratelimit.go, retry.go, stream.go, usage.go]
---

# internal/api

> Layer: **Services** · Files: 16 · Interfaces: 2 · Structs: 31 · Functions: 9

## Interfaces

### Client (2 methods)
> Client is the unified Anthropic API entry point.

```go
type Client interface {
    Stream(ctx context.Context, req *MessageRequest) (StreamReader, error)
    Complete(ctx context.Context, req *MessageRequest) (*MessageResponse, error)
}
```

### StreamReader (1 methods)
> StreamReader wraps SSE event stream reading, following io.Closer semantics.

```go
type StreamReader interface {
    Next() (*StreamEvent, error)
}
```

## Structs

- **APIError** — 4 fields: StatusCode, Message, Headers, Kind
- **APIErrorData** — 2 fields: Type, Message
- **Accumulator** — 3 fields
- **CacheControl** — 1 fields: Type
- **CacheCreationUsage** — 2 fields: Ephemeral1hInputTokens, Ephemeral5mInputTokens
- **CannotRetryError** — 2 fields: Cause, RetryContext
- **ClientConfig** — 14 fields: Provider, APIKey, BaseURL, MaxRetries, TimeoutSeconds, RatePolicy, CustomHeaders, AWSRegion, ...
- **ContentBlock** — 12 fields: Type, Text, ID, Name, Input, ToolUseID, Content, IsError, ...
- **ContentBlockDeltaData** — 2 fields: Index, Delta
- **DebugLogger** — 4 fields
- **Delta** — 4 fields: Type, Text, PartialJSON, Thinking
- **ExtraUsage** — 4 fields: IsEnabled, MonthlyLimit, UsedCredits, Utilization
- **FallbackTriggeredError** — 2 fields: OriginalModel, FallbackModel
- **ImageSource** — 4 fields: Type, MediaType, Data, URL
- **ListModelsError** — 3 fields: Kind, StatusCode, Message
- **MessageDeltaData** — 2 fields: Delta, Usage
- **MessageParam** — 2 fields: Role, Content
- **MessageRequest** — 9 fields: Model, MaxTokens, Messages, System, Tools, Stream, ThinkingBudget, SystemBlocks, ...
- **MessageResponse** — 7 fields: ID, Type, Role, Content, Model, StopReason, Usage
- **MessageStartData** — 1 fields: Message
- **ModelListing** — 2 fields: ID, ContextWindow
- **RateLimit** — 2 fields: Utilization, ResetsAt
- **RatePolicy** — 2 fields: RPM, MaxConcurrent
- **RetryContext** — 3 fields: Attempt, Model, QuerySource
- **RetryOptions** — 7 fields: MaxRetries, Model, FallbackModel, QuerySource, InitialConsecutive529, Signal, RefreshToken
- **ServerToolUse** — 2 fields: WebSearchRequests, WebFetchRequests
- **StreamEvent** — 6 fields: Type, Data, MessageStart, ContentBlockDelta, MessageDelta, Error
- **SystemBlock** — 3 fields: Type, Text, CacheControl
- **ToolSchema** — 4 fields: Name, Description, InputSchema, CacheControl
- **Usage** — 9 fields: InputTokens, CacheCreationInputTokens, CacheReadInputTokens, OutputTokens, ServerToolUse, ServiceTier, CacheCreation, InferenceGeo, ...
- **Utilization** — 6 fields: FiveHour, SevenDay, SevenDayOAuthApps, SevenDayOpus, SevenDaySonnet, ExtraUsage

## Function Types

- `RetryableFunc` — `func(ctx context.Context, attempt int) (T, error)`

## Functions

- `Is529Error(err error) bool`
- `IsImageUnsupportedError(err error) bool`
- `IsOAuthTokenRevokedError(err error) bool`
- `ListModels(ctx context.Context, cfg ClientConfig, httpClient *http.Client) ([]ModelListing, error)`
- `NewClient(cfg ClientConfig, httpClient *http.Client) (Client, error)`
- `NewDebugLogger(debug bool, debugFile string) (*DebugLogger, error)`
- `ParseContextOverflowError(err error) (inputTokens int, contextLimit int, ok bool)`
- `RetryDelay(attempt int, retryAfterHeader string, maxDelayMS int) time.Duration`
- `WithRetry(ctx context.Context, fn any, opts RetryOptions) (T, error)`

## Constants

- `BaseDelayMS`
- `DefaultMaxRetries`
- `ErrKindConnectionError`
- `ErrKindConnectionTimeout`
- `ErrKindContextWindow`
- `ErrKindForbidden`
- `ErrKindInvalidRequest`
- `ErrKindOverloaded`
- `ErrKindRateLimit`
- `ErrKindServerError`
- `ErrKindUnauthorized`
- `ErrKindUnknown`
- `EventContentBlockDelta`
- `EventContentBlockStart`
- `EventContentBlockStop`
- `EventError`
- `EventMessageDelta`
- `EventMessageStart`
- `EventMessageStop`
- `EventPing`
- `HeartbeatInterval`
- `ListModelsForbidden`
- `ListModelsHTTP`
- `ListModelsNotSupported`
- `ListModelsTimeout`
- `ListModelsUnauthorized`
- `ListModelsUnreachable`
- `Max529Retries`
- `MaxDelayMS`
- `PersistentMaxBackoff`
- `PersistentResetCap`
- `ProviderBedrock`
- `ProviderDirect`
- `ProviderFoundry`
- `ProviderOpenAI`
- `ProviderVertex`

## Change Impact

**Test Mocks (must add new methods when interfaces change):**
- `mockClient` in `internal/engine/engine_test.go`
- `stubClient` in `internal/tui/btw_test.go`
- `mockClient` in `test/integration/engine_e2e_test.go`

**Exported type references (files that use types from this package):**
- `APIError` → `internal/engine/engine_test.go` (test), `internal/engine/query.go`, `internal/engine/retry_wiring_test.go` (test), `internal/engine/tiers_test.go` (test), `internal/engine/tool_images_test.go` (test)
- `Accumulator` → `internal/engine/query.go`
- `Client` → `internal/bootstrap/modelswitch.go`, `internal/bootstrap/setup.go`, `internal/bootstrap/setup_test.go` (test), `internal/bootstrap/tiers.go`, `internal/bootstrap/tiers_test.go` (test) + 9 more
- `ClientConfig` → `internal/bootstrap/setup.go`, `internal/bootstrap/setup_test.go` (test), `internal/bootstrap/tiers.go`, `internal/bootstrap/wire.go`
- `ContentBlock` → `internal/bootstrap/acp_integration_test.go` (test), `internal/compact/auto_test.go` (test), `internal/engine/engine_test.go` (test), `internal/engine/query.go`, `internal/engine/thinking_display_test.go` (test) + 2 more
- `ContentBlockDeltaData` → `internal/bootstrap/acp_integration_test.go` (test), `internal/engine/engine_test.go` (test), `internal/engine/thinking_display_test.go` (test)
- `Delta` → `internal/bootstrap/acp_integration_test.go` (test), `internal/engine/engine_test.go` (test), `internal/engine/thinking_display_test.go` (test)
- `ListModelsError` → `internal/bootstrap/setup.go`
- `MessageDeltaData` → `internal/bootstrap/acp_integration_test.go` (test), `internal/engine/engine_test.go` (test), `internal/engine/thinking_display_test.go` (test)
- `MessageParam` → `internal/compact/auto.go`, `internal/engine/query.go`, `internal/tui/autorun.go`
- `MessageRequest` → `internal/bootstrap/acp_integration_test.go` (test), `internal/bootstrap/tiers_test.go` (test), `internal/compact/auto.go`, `internal/compact/auto_test.go` (test), `internal/engine/btw_test.go` (test) + 14 more
- `MessageResponse` → `internal/bootstrap/acp_integration_test.go` (test), `internal/bootstrap/tiers_test.go` (test), `internal/compact/auto.go`, `internal/compact/auto_test.go` (test), `internal/engine/engine_test.go` (test) + 5 more
- `MessageStartData` → `internal/bootstrap/acp_integration_test.go` (test), `internal/engine/engine_test.go` (test), `internal/engine/thinking_display_test.go` (test)
- `RetryOptions` → `internal/engine/query.go`
- `StreamEvent` → `internal/bootstrap/acp_integration_test.go` (test), `internal/engine/engine_test.go` (test), `internal/engine/thinking_display_test.go` (test)
- `StreamReader` → `internal/bootstrap/acp_integration_test.go` (test), `internal/bootstrap/tiers_test.go` (test), `internal/compact/auto_test.go` (test), `internal/engine/btw_test.go` (test), `internal/engine/cache_breakpoints_test.go` (test) + 12 more
- `SystemBlock` → `internal/engine/query.go`
- `ToolSchema` → `internal/engine/compaction.go`
- `Usage` → `internal/bootstrap/acp_integration_test.go` (test), `internal/engine/compaction.go`, `internal/engine/engine_test.go` (test), `internal/engine/query.go`, `internal/engine/thinking_display_test.go` (test) + 1 more

## Dependencies

**Imports:** *(none — zero-dependency)*

**Imported by:** `internal/bootstrap`, `internal/compact`, `internal/engine`, `internal/tui`

<!-- AUTO-GENERATED ABOVE — DO NOT EDIT -->
<!-- MANUAL NOTES BELOW — preserved across regeneration -->

## Design Notes

- **2026-11-18 内核搬回（反转 PR #31）**：本包原于 2026-09-23 逐字提取至外部仓 github.com/tunsuy/agtkeel；因该仓停止维护，经人类决策整併回本仓原始路径。来源：agtkeel main 3a77085＋后续提交，零逻辑改动（仅 import 路径恢复＋gofmt）。内核改动自此直接在本仓进行。
- **OpenAI 兼容增强（随搬回正式入库）**：normalizeBaseURL 去除 baseURL 尾部 /v1 避免重复版本路径（原 agtkeel 提交 9262ebf）；openai_types.go 新增 ReasoningContent 字段支持 DeepSeek-R1/QwQ 类推理模型（原 .agtkeel-uncommitted.diff WIP，现已落定）。
- **2026-11-18 prompt-cache wire 支援（feat/coordinator-fork-cache）**：此前 Anthropic 路徑**完全未送 cache_control**（TUI 設的 SystemPromptPart.CacheControl 被 engine 壓成純字串忽略）——prompt cache 從未寫入。現 MessageRequest.SystemBlocks 非空時 MarshalJSON 以 block 陣列序列化 system（帶斷點）；ToolSchema/ContentBlock +CacheControl；EphemeralCache 共享標記值。System 仍為 canonical 純文字（既有測試與 compact 路徑不受影響）；OpenAI 相容路徑經 SystemText() 讀純文字（provider 隱式前綴快取，不帶標記）。滾動斷點由 engine 佈局，api 層只負責序列化。
- **2026-11-19 OpenAI 相容路徑 cache usage 映射（feat/openai-cache-usage）**：此前 OpenAI 路徑 CacheReadInputTokens 恆 0——openaiUsage 無 cached_tokens 欄位、StreamOptions.IncludeUsage 從未設定、usage chunk（常於 finish chunk 之後、choices 空）不被發出事件、accumulator 只合併 OutputTokens。現：openaiUsage +PromptTokensDetails（OpenAI 式）＋PromptCacheHitTokens（DeepSeek 式）＋cachedTokens()；streaming 請求設 stream_options.include_usage=true；usage chunk 觸發补充 message_delta（createUsageDeltaEvent，.repeat lastStopReason）携带 final input/output/cache counts；accumulator 非零 input/cache 欄位覆寫 message_start 快照、空 stop reason 視為不變（Anthropic delta 語義保留）。Complete 路徑同步映射。守護測試 openai_cache_usage_test.go×4。注意：OpenAI prompt_tokens 含 cached（與 Anthropic input_tokens 不含 cache read 的語義不同），CacheReadInputTokens 独立報告命中數。
- **2026-11-19 客戶端速率控制＋retry 接線（feat/api-rate-control）**：使用者實機踩 qwen 429。診斷：WithRetry 完整實作但 engine 從未呼叫（429 直接失敗）；無主動限速（平行 fork＝無上限併發）。新增 RatePolicy{RPM, MaxConcurrent}＋rateLimitedClient wrapper（provider-agnostic，NewClient 統一 wrap 所有 provider）：RPM 走 strict-spacing token bucket（burst 1）、MaxConcurrent 走 semaphore（streaming 持 slot 至 reader.Close，releasedReader sync.Once 釋放）；env CLAUDE_CODE_API_RPM／CLAUDE_CODE_API_MAX_CONCURRENT，ClientConfig.RatePolicy 優先。WithRetry +RefreshToken hook（nil 時 401 fail-fast，不烧退避梯）；foreground529RetrySources +foreground（Go engine 主 query source，TS repl_main_thread 對應）。Provider 層「拉出來」採內部抽象化（Client 介面＋factory＋wrapper seam），**不對外抽倉**（agtkeel 教訓）。守護測試 ratelimit_test.go×10。
- **2026-11-19 OpenAI reasoning_content→thinking 顯示鏈（feat/openai-thinking-display）**：此前串流 reader 只處理 Delta.Content，reasoning_content 直接丟棄（非串流同样）——推理模型「想了但看不見」。現：串流 reasoning_content→Anthropic thinking block 事件（content_block_start thinking＋thinking_delta＋stop），block index 改動態分配（thinking 先於 text 先於 tools；無 reasoning 時 text=0/tools=1 與舊制約相容）；closeOpenBlocks 冪等（finish 與 [DONE] 共用，修此前雙重 stop）；同 chunk 帶 reasoning＋content 不丟失（queue 後 fall-through）。非串流 convertToMessageResponse 前置 thinking block。下游 engine MsgTypeThinkingDelta／accumulator thinking_delta／TUI renderThinkingBlock 皆既有，零改動。**控制層（--thinking 旗標→enable_thinking/reasoning_effort）刻意未做**（使用者裁示先做顯示層；thinking tokens 計費，啟用參數属成本決策）。守護測試 openai_thinking_test.go×5＋engine thinking_display_test.go。
- **2026-11-19 OpenAI 相容 usage 語意**：`Usage.InputTokens` 改為 `prompt_tokens − cached`（`openaiUsage.uncachedInputTokens`，下限 0），對齊 Anthropic「input_tokens 不含快取讀取」的語意。修正前同時回報含快取的 `prompt_tokens` 與 `CacheReadInputTokens`，壓縮門檻（engine 以 input+cache read+cache creation 計算 prompt 大小）和 TUI 狀態列的上下文用量都會重複計算快取部分。
- **2026-11-19 `ListModels`（/setup 驗證）**：獨立函式，**不加進 `api.Client` 介面**（避免改動所有測試用的假 client）。只打 `GET {base}/v1/models`，不呼叫 LLM、不花 token；路徑沿用既有 client 的 `normalizeBaseURL` + `/v1/...` 慣例。Anthropic 依 `has_more`/`last_id` 分頁（上限 10 頁）；OpenAI 相容讀 `context_length`。錯誤分類為 `ListModelsError`，金鑰在**截斷前**先遮蔽（避免金鑰跨過截斷點時部分外洩）；200 但沒有 `data` 欄位視為不支援。OpenRouter 的模型清單是公開的，所以另外打 `/api/v1/key` 驗證金鑰。
- **2026-11-20 帶其他版本號的 base URL（fix/versioned-base-url）**：OpenAI 相容的路徑（`/chat/completions`、`/models`、OpenRouter `/key`）改由 `openAIURL(base, path)` 組合。`normalizeBaseURL` 只去掉 `/v1`，原本之後一律接 `/v1`，所以 z.ai 文件給的 `https://api.z.ai/api/coding/paas/v4` 會變成 `…/v4/v1/chat/completions`（404）。現在 base 結尾是 `/v<數字>` 就直接接 path，否則照舊插入 `/v1`，`/v1`、無版本的 base 行為不變。只比對完整路徑段，`/v4beta` 這種不算。Anthropic 的 `/v1/messages` 不變。守護測試 TestOpenAIURL、TestOpenAIClient_V4BaseURL、TestListModels_BaseURLWithV4；另以實際 z.ai 端點驗證過取得模型與對話。
- **2026-11-20 finish_reason 與 usage 同一個 chunk（fix/openai-finish-with-usage）**：智譜 GLM（z.ai）串流把 `finish_reason` 和 `usage` 放在**同一個** chunk。原本 reader 只要看到 `usage` 且已 started 就提早 return usage delta，同 chunk 的 finish 沒被處理 → stop reason 為空 → engine 當成 `end_turn` 結束回合，**模型要求的工具從未執行**（TUI 只顯示 tool_use、沒有結果，回應看起來「很短就停」）。現在只有 choices 為空的獨立 usage chunk 才走提早 return；有 choices 時 fall through，由 finish 的 message_delta 帶出已記錄的 usage。守護測試 TestOpenAIStream_FinishAndUsageInSameChunk（chunk 形狀取自實測 z.ai 回應）；另以 glm-5.3-flash 實機對照修改前後驗證。
- **2026-11-20 圖片送達 OpenAI 相容 provider（feat/tool-result-images）**：`ContentBlock` 加 `Source *ImageSource`。`convertMessage` 把 user 訊息的 image block 轉成 `image_url` content part（base64 → data URL）；tool_result 裡的圖片因為 `role:"tool"` 只吃文字，改成在所有 tool 訊息之後補一則 user 訊息（`toolImagesNote` + image_url），tool 訊息本身只放文字或 `toolImagePlaceholder`。`openaiMessage.Parts` 用自訂 `MarshalJSON`，**只有帶圖的訊息**才輸出陣列 content，沒有圖片的請求位元組與以前完全相同（cache-first，守護測試 TestOpenAIConvert_NoImageBytesUnchanged）。`splitToolResultContent` 取代 `extractToolResultContent`，不再把含圖的陣列以原始 JSON（含 base64）當文字送出。`IsImageUnsupportedError`：400 且訊息符合 z.ai／OpenAI／DeepSeek 等「不接受圖片」措辭的啟發式判斷，呼叫端只在請求真的帶圖時才採信。實測 z.ai：glm-5.3-flash 接受圖片，glm-5.3／glm-4.5 回 400 `messages.content.type is invalid, allowed values: ['text']`。
- **2026-11-20 `IsImageUnsupportedError` 收緊**：只認「模型根本不收圖」的措辭（`allowed values: ['text']`、`only supported by certain models`、`unknown variant image_url`、`does not support … image`、`image … not supported`）。單一張圖的問題（media type 不符、超過 8000 px、超過 5 MB）不算，否則支援視覺的模型會因為一張壞圖被整個 session 停用圖片。守護案例在 TestIsImageUnsupportedError。
