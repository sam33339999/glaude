---
package: compact
import_path: internal/compact
layer: services
generated_at: 2026-11-25T16:47:52Z
source_files: [auto.go, compact.go, images.go, micro.go, snip.go, target.go, window.go]
---

# internal/compact

> Layer: **Services** · Files: 7 · Interfaces: 1 · Structs: 13 · Functions: 9

## Interfaces

### Compressor (2 methods)
> Compressor is the unified interface for context compaction.

```go
type Compressor interface {
    NeedsCompaction(messages []types.Message, model string, extra CompactionExtra) bool
    Compact(ctx context.Context, messages []types.Message, params CompactionParams) (*CompactionResult, error)
}
```

## Structs

- **AutoCompactTrackingState** — 4 fields: Compacted, TurnCounter, TurnID, ConsecutiveFailures
- **AutoCompactor**
- **CacheEdit** — 2 fields: ToolUseID, Summary
- **CompactionExtra** — 1 fields: SnipTokensFreed
- **CompactionParams** — 5 fields: SystemPromptParts, UserContext, SystemContext, QuerySource, ForkMessages
- **CompactionResult** — 7 fields: SummaryMessages, Attachments, HookResults, PreCompactTokenCount, PostCompactTokenCount, TruePostCompactTokenCount, CompactionUsage
- **MicroCompactResult** — 2 fields: Messages, CompactionInfo
- **MicroCompactionInfo** — 1 fields: PendingCacheEdits
- **MicroCompactor**
- **SnipResult** — 3 fields: Messages, TokensFreed, Snipped
- **Target** — 4 fields: Client, Model, ContextWindow, MaxOutputTokens
- **TokenUsage** — 4 fields: InputTokens, OutputTokens, CacheReadInputTokens, CacheCreationInputTokens
- **Tracker** — 3 fields

## Functions

- `AutoCompactThreshold(t Target) int`
- `ContextWindow(model string) int`
- `EstimateTokens(messages []types.Message) int`
- `KnownContextWindow(model string) (int, bool)`
- `NewAutoCompactor() *AutoCompactor`
- `NewMicroCompactor() *MicroCompactor`
- `NewTracker() *Tracker`
- `ResolveContextWindow(model string, configured int) int`
- `SnipCompactIfNeeded(messages []types.Message, estTokens int, t Target) SnipResult`

## Constants

- `AutoCompactBufferTokens`
- `AutoCompactWarningBufferTokens`
- `MaxConsecutiveFailures`
- `MaxOutputTokensForSummary`
- `SnipTriggerFraction`

## Change Impact

**Exported type references (files that use types from this package):**
- `AutoCompactor` → `internal/engine/engine.go`
- `CompactionExtra` → `internal/engine/compaction.go`
- `CompactionParams` → `internal/engine/compaction.go`
- `MicroCompactor` → `internal/engine/engine.go`
- `Target` → `internal/engine/compaction.go`, `internal/tui/btw.go`, `internal/tui/modelpicker.go`
- `Tracker` → `internal/engine/compaction.go`, `internal/engine/engine.go`

## Dependencies

**Imports:** `internal/api`, `pkg/types`

**Imported by:** `internal/bootstrap`, `internal/engine`, `internal/tui`

<!-- AUTO-GENERATED ABOVE — DO NOT EDIT -->
<!-- MANUAL NOTES BELOW — preserved across regeneration -->

## Design Notes

- **2026-11-18 内核搬回（反转 PR #31）**：本包原于 2026-09-23 逐字提取至外部仓 github.com/tunsuy/agtkeel；因该仓停止维护，经人类决策整併回本仓原始路径。来源：agtkeel main 3a77085＋后续提交，零逻辑改动（仅 import 路径恢复＋gofmt）。内核改动自此直接在本仓进行。
- **2026-11-19 壓縮全修（provider 等級，C1/C3/C4/C5/C7/C8/C9/C12）**：
  - `AutoCompactor` 改為無狀態；門檻與摘要依查詢的 `Target`（client、model、context window、max_tokens）計算，摘要打到**這次查詢的** client 與模型（修正前一律用主腦啟動時的 client/model）。狀態移到 `Tracker`（mutex 保護），主腦一份（整個會話累計）、每個 sub-agent 各一份——修正前全 engine 共用一份 tracking、無鎖、sub-agent 失敗會熔斷主腦。
  - `ResolveContextWindow`：env > 設定值 > 內建表（`claude-2*`=100k、其他 `claude-*`=200k）> 200k。非 Claude 模型的真實視窗由 config.toml 提供。
  - `fitForSummary`（C7）：對話超過摘要模型的上限時，從最舊端丟到符合預算，且切點必須是不含 tool_result 的 user 訊息（避免孤兒 tool 對）。摘要輸出上限 `min(20000, max_tokens)`。
  - **snip（C9/C12）**：證據見 `snip_prefix_test.go` 的歷史版本——修正前每輪都會把一則剛越過保護區的訊息改成 placeholder（前綴中段被改寫、快取每輪失效），並每輪追加一次 boundary 訊息（緊接 tool_result 的連續 user 訊息）。現在只在估算值 ≥ `SnipTriggerFraction`(0.8)×自動壓縮門檻時，一次剪完保護區之前的全部內容；Cache-first 下這等同一次壓縮（快取讀取 10% 價格、重寫 125%，提早 snip 通常賠錢）。說明文字併入 placeholder，不再有 boundary 訊息。
  - `CompactionExtra.SnipTokensFreed` 保留欄位但不再相減：訊息已是 snip 後的，修正前會重複扣除。
  - 段落審查後補修：snip 每個壓縮週期最多一次（歷史已有被剪過的區塊就不再 snip，交給自動壓縮），否則估算值停在 [0.8T, T) 時會逐輪改寫前綴、重現 C9。`fitForSummary` 保留第一則 plain user（任務 prompt，附截斷說明），之後從 assistant 邊界切，保留最近的工具往返；修正前 sub-agent 的摘要會只剩任務 prompt。
  - 最終審查後補修：`AutoCompactThreshold` 的保留量按窗口比例設上限——輸出保留 `min(max_tokens, 20000, Window/4)`、buffer `min(13000, Window/10)`。200k 窗口數值不變；32k 窗口的門檻從 3k～11k（低於工具 schema 等固定開銷，會每輪壓縮）變成約 21k。另外 `Tracker.RecordPostCompact` 記下壓縮後的估算值，之後要成長 Window/10 才會再壓縮，避免固定開銷本身就超過門檻時每輪重複摘要。
- **2026-11-19 `KnownContextWindow`**：讓 /setup 判斷主腦模型是否在內建表（`claude-*`），不在才寫 `[main] context_window`。
- **2026-11-20 圖片不當文字估算、摘要請求不帶圖（feat/tool-result-images）**：engine 改成以 image block 送出工具回傳的圖片後，`EstimateTokens`（JSON 位元組 ÷ 4）會把 base64 當文字算——一張 1 MB 截圖約被估成 33 萬 tokens，下一輪立刻觸發 auto-compact，`fitForSummary` 還會依膨脹的數字砍歷史。現在估算前以 `withoutImages` 把圖片換成佔位文字，每張圖另計固定 `imageTokenEstimate`（1600，對應 provider 依像素計費、Anthropic 縮圖後約 1.6k 上限）。摘要請求是文字進文字出，且摘要模型可能不收圖（例如已被 engine 標記的 glm-5.3），所以送出前一律以 `withoutImages` 換成 `[image]`，避免 400 累積失敗打開熔斷器。`withoutImages` 只複製有圖的訊息，不改呼叫端的 history。守護測試 images_test.go。
