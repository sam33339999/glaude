---
package: tui
import_path: internal/tui
layer: tui
generated_at: 2026-11-25T16:47:52Z
source_files: [agentcolors.go, autorun.go, bashmode.go, btw.go, clipboard_paste.go, cmds.go, codeblock.go, completion.go, coordinator.go, init.go, input.go, keys.go, memory_events.go, memstat.go, memstat_darwin.go, memstat_linux.go, memstat_other.go, memstat_windows.go, messagelist.go, messages.go, model.go, modelchooser.go, modelpicker.go, paste.go, paste_input.go, permissions.go, provider_dialog.go, providerwizard.go, providerwizard_view.go, rendercache.go, resumepicker.go, scrollview.go, selection.go, setup_dialog.go, setupwizard.go, setupwizard_view.go, spinner.go, statusbar.go, styles.go, textwidth.go, theme.go, tier_dialog.go, tierpicker.go, toolview.go, turntiming.go, update.go, view.go, welcome.go]
---

# internal/tui

> Layer: **TUI** · Files: 48 · Interfaces: 0 · Structs: 39 · Functions: 6

## Structs

- **AgentColorManager** — 3 fields
- **AgentProgressMsg** — 3 fields: TaskID, Activity, Detail
- **AgentStatusMsg** — 5 fields: TaskID, Status, Description, Result, Summary
- **AgentTaskState** — 12 fields: ID, Name, AgentType, Description, Status, StartTime, ElapsedMs, OutputTokens, ...
- **AppModel** — 105 fields
- **CommandResultMsg** — 2 fields: Text, IsError
- **CompactDoneMsg** — 1 fields: Summary
- **CoordinatorPanel** — 3 fields: Tasks, SelectedIndex, TaskOrder
- **DreamDoneMsg** — 1 fields: Err
- **InputChangedMsg** — 1 fields: Text
- **InputModel** — 4 fields
- **InputSubmittedMsg** — 1 fields: Text
- **MCPStatusMsg** — 1 fields: Text
- **MemdirLoadedMsg** — 2 fields: Paths, ScopedFiles
- **MemoryEventMsg** — 1 fields: Event
- **MessageLookups** — 4 fields: ToolUseToResult, ResolvedToolUseIDs, ErroredToolUseIDs, InProgressToolUseIDs
- **PermissionDialog** — 8 fields
- **PermissionRequestMsg** — 8 fields: RequestID, ToolName, ToolUseID, Message, Input, ProjectPath, FromCodeMode, RespFn
- **QuestionRequestMsg** — 4 fields: RequestID, Question, Options, RespFn
- **SlashCommandMsg** — 2 fields: Name, Args
- **SpinnerModel** — 4 fields
- **StatusBar** — 13 fields
- **StreamAssistantTurnMsg** — 2 fields: FinalMessage, Usage
- **StreamDoneMsg** — 2 fields: FinalMessage, Usage
- **StreamErrorMsg** — 1 fields: Err
- **StreamThinkingMsg** — 1 fields: Delta
- **StreamTokenMsg** — 1 fields: Delta
- **StreamToolResultMsg** — 3 fields: ToolUseID, Content, IsError
- **StreamToolUseCompleteMsg** — 2 fields: ToolUseID, ToolInput
- **StreamToolUseInputDeltaMsg** — 3 fields: ToolUseID, ToolName, InputDelta
- **StreamToolUseStartMsg** — 3 fields: ToolUseID, ToolName, InputDelta
- **StreamUserTurnMsg** — 1 fields: FinalMessage
- **SystemTextMsg** — 1 fields: Text
- **TermResizedMsg** — 2 fields: Width, Height
- **Theme** — 13 fields: Name, Primary, Secondary, Accent, Muted, Error, Warning, Success, ...
- **TickMsg** — 1 fields: Time
- **TokenUsage** — 4 fields: Input, Output, CacheRead, CacheCreated
- **TranscriptErrMsg** — 1 fields: Err
- **WelcomeHeader** — 4 fields

## Functions

- `IsSlashCommand(text string) bool`
- `MessageListView(messages []types.Message, width int, darkMode bool, theme Theme, mdRenderer *glamour.TermRenderer, expandedToolResults map[string]bool) string`
- `New(qe engine.QueryEngine, appStore *state.AppStateStore, vimEnabled bool, dark bool, permAskCh <-chan permissions.AskRequest, permRespCh chan<- permissions.AskResponse, agentCoord tools.AgentCoordinator, agentEventCh <-chan coordinator.Event, mq *msgqueue.MessageQueue, qg *msgqueue.QueryGuard, memoryStore *memdir.MemoryStore, mcpPool commands.MCPServerLister, tiers commands.TierManager, setup commands.SetupBackend, models commands.ModelSwitcher, btwTarget func(ref string) (engine.MainTarget, error), rec *transcript.Recorder, questionAskCh <-chan types.QuestionRequest, questionRespCh chan<- types.QuestionResponse, extraCommands ...func(*commands.Registry)) tea.Model`
- `NewAgentColorManager() *AgentColorManager`
- `NewInput(vimEnabled bool) InputModel`
- `NewWelcomeHeader(model string, cwd string) WelcomeHeader`

## Constants

- `AgentCompleted`
- `AgentFailed`
- `AgentPaused`
- `AgentRunning`
- `PermissionChoiceAlwaysAllow`
- `PermissionChoiceNo`
- `PermissionChoiceYes`
- `SpinnerModeBrief`
- `SpinnerModeNormal`
- `SpinnerModeTeammate`
- `ToolStatusError`
- `ToolStatusInProgress`
- `ToolStatusQueued`
- `ToolStatusResolved`
- `VimModeInsert`
- `VimModeNormal`
- `VimModeVisual`

## Change Impact

**Exported type references (files that use types from this package):**
- `AppModel` → `internal/bootstrap/root.go`

## Dependencies

**Imports:** `internal/agenttype`, `internal/api`, `internal/clipboard`, `internal/commands`, `internal/compact`, `internal/coordinator`, `internal/engine`, `internal/memdir`, `internal/modeltier`, `internal/msgqueue`, `internal/permissions`, `internal/soul`, `internal/state`, `internal/textutil`, `internal/tools`, `internal/tools/shell`, `internal/transcript`, `internal/turnlog`, `pkg/types`

**Imported by:** `internal/bootstrap`

<!-- AUTO-GENERATED ABOVE — DO NOT EDIT -->
<!-- MANUAL NOTES BELOW — preserved across regeneration -->

## Design Notes

- **2026-09-23 基座换库（agtkeel import-swap）**：可复用 agent 内核已逐字提取到 `github.com/tunsuy/agtkeel`（MIT），本包 import 随之改写：`internal/{api,compact,msgqueue,engine,session,permissions,hooks,mcp,config}` → `agtkeel/*`、`internal/tools`（接口契约+注册表）→ `agtkeel/tools`、`pkg/types` → `agtkeel/types`、`pkg/utils/{fs,ids}` → `agtkeel/utils/*`。本包代码**仅 import 路径重写，零逻辑改动**，`go build`/`go vet`/`go test -race ./...` 全绿。已知表现局限：docgen 依赖图按本模块前缀分类，agtkeel 视作第三方库，故上方 Imports 栏不再展示内核依赖（与 cobra 等一致）。
- **2026-11-18 BF-03（/mcp、/resume 接线）**：`New()`/`newAppModel` 末尾新增 `mcpPool commands.MCPServerLister` 注入参数（bootstrap `runInteractive` 传入 `container.MCPPool`，与 memoryStore 同一注入模式）；`AppModel` 新增 `mcpPool` 字段。新增 `MCPStatusMsg` 与 `fetchMCPStatusCmd`（/mcp 报告在 Update 循环外生成，整体 15s 超时，MVU 合规）。`applyCommandResult` 新增：`FetchMCPStatus` → 异步 Cmd；`NewMessages`（`[]types.Message` 断言）→ 替换 `m.messages` + `queryEngine.SetMessages` + 采纳 `NewSessionID`，查询进行中拒绝恢复；/clear 的遗留 `[]interface{}{}` 断言失败被忽略，行为不变。未新增引擎契约（复用既有 `SetMessages`）。
- **2026-11-18 BF-03-F1 评审修复（CR-BF03-1）**：`applyCommandResult` 提取 `applyResumeResult`（NewMessages 消费＋isLoading 拒绝，返回 stop 标志）与 `applyDisplayResult`（Display 渲染）两个私有 helper，主函数 107→72 行（≤80 规范），逐路径行为不变。/mcp 显示-only 守护补强：TestSlashMCP_FetchesStatusViaCmd 追加引擎历史 DeepEqual 不变断言（cache gate）。
- **2026-11-18 BF-05 /config 面板**：view.go 新增 renderConfigDialog（dialogConfig overlay case），渲染 AppState 当前设置（Model/Theme/Vim/Effort/WorkingDir/Session），Esc 关闭沿用既有 keys.go。纯展示不触引擎历史（DeepEqual 守护测试）。只读面板，交互编辑为文档化后续项。
- **2026-11-18 BF-18 持久化接线**：AppModel.transcript 字段＋New/newAppModel 末位 rec 参数（可 nil）；StreamDone/StreamError 返回 persistTranscriptCmd（创建时快照拷贝 msgs 防引擎后续突变竞态）；/resume 采纳 NewSessionID 时经 switchTranscriptCmd 换文件（与 display/dream/mcp Cmd 用 tea.Batch 组合）；TranscriptErrMsg 降级为系统警告。sessionID 启动时取自 recorder。
- **2026-11-18 BF-07 Todo 面板**：renderTodoPanel 于 View 输入区上方渲染 AppState.Todos（[x] completed／[>] in_progress／[ ] pending），空列表不渲染。读 mutex 保护 store，纯展示（cache gate 三问全否）。
- **2026-11-18 BF-08 问答对话框**：questionAskCh/questionRespCh 字段＋New/newAppModel 参数；listenForQuestionRequest（RespFn callback 回传，权限 listener 同构）；dialogQuestion 状态机 questionState（选项 selected＋自由文本 answer）；handleQuestionKey（1-9/ runes/backspace/enter/esc）；renderQuestionDialog。
- **2026-11-18 BF-21 Shift+Tab 模式循环**：nextPermissionMode（default→acceptEdits→plan→bypassPermissions→default，空 Mode 视为 default；oracle 顺序，YOLO 纳入循环为人类明确要求之分歧）；cyclePermissionMode 更新 AppState＋系统訊息（YOLO 警告）；StatusBar.mode 指示器（⏵⏵ accept edits／⏸ plan／⚠ YOLO warning bold），View 每次读 live mode。
- **2026-11-18 UPD-01 Bubble Tea v2 迁移**：charm.land/bubbletea/v2 v2.1.1＋bubbles/v2＋lipgloss/v2＋glamour v1.0.0（Go 1.25）。View()→tea.View（screenView 设 AltScreen/MouseModeCellMotion）；KeyPressMsg（Code/Text/Mod 匹配；ctrl=字母+ModCtrl、shift+tab=Tab+ModShift、printable=Text!=""）；MouseWheelMsg 独立类型；viewport New(WithWidth/WithHeight)/SetWidth/ScrollUp/ScrollDown/HalfPageUp/Down；Theme 欄位 image/color.Color（lipgloss.Color 为函数）；textarea New() 零参兼容。自研 spinner 零影响。v2 守护测试×3。
- **2026-11-18 对话框可见性修复**：View 先渲染 renderActiveDialog＋todo/coordinator 面板并计行（renderedLines），viewport 以副本 SetHeight(原高−extras) 收缩，保证总渲染高度≤termHeight——修复批准对话框被挤出终端底部不可见的 bug。回归测试 dialog_visibility_test.go×4。
- **2026-11-18 EV-01**：processQueueCmd ModeExternalPrompt 分支——附加 display 訊息＋pinnedToBottom＋startQueryCmd，MCP 通知唤醒代理处理。
- **2026-11-18 CM-03**：PermissionRequestMsg +FromCodeMode；newPermissionDialog 按来源切换 always-allow 选项文案（CodeMode-scoped vs 项目级）；listenForPermissionRequest RespFn 映射 AlwaysInCodeMode/AlwaysAllow 互斥。
- **2026-11-18 coordinator 提示注入（feat/coordinator-fork-cache）**：startQueryCmd 於 coordinatorMode 時注入 GetCoordinatorSystemPrompt(false) 至穩定 system 前綴（GLAUDE.md 之後）；此前該 prompt 僅測試使用、未接生產。cache 佈局：穩定段（GLAUDE.md＋coordinator prompt）末尾掛單一 ephemeral 斷點，動態記憶 reminder 追加於斷點之後（turn 尾部、不進快取前綴）。守護測試 coordinator_prompt_test.go×2。
- **2026-11-18 WorkingDir 注入（feat/agent-cwd）**：startQueryCmd uctx.WorkingDir = appState.GetState().WorkingDir（live）——主會話工具（含 EnterWorktree 後）在會話目錄執行。守護測試 TestStartQueryCmd_WorkingDirFromAppState。
- **2026-11-19 協調者完成唤醒＋面板高度約束（feat/coordinator-completion-wake）**：(1) AgentStatusMsg completed/failed 時注入 coordinator.FormatTaskNotification 至 msgQueue（ModeExternalPrompt，复用 EV-01 唤醒路徑）——idle 立即 processQueueCmd 起 query；running 則排队待 StreamDone between-turn drain（不中斷 in-flight turn，同步 agent 经 WaitForAgent 取結果故不需 wake）。修「背景 agent 完成後協調者不繼續跑／派 2+ agent 後停擺」。AgentStatusMsg +Result/Summary；init.go +coordinatorStatus 反向映射。(2) CoordinatorPanel.View 上限 maxPanelTaskRows=5（running 優先排序＋"+N more"），View viewport 下限 minViewportLines=5——修「subagent 面板擠掉輸出」。守護測試 coordinator_wake_test.go×5。
- **2026-11-19 View 高度保證_fit（fix/subagent-result-layout）**：重寫高度分配防底部遮罩——topAvailable=termHeight-reservedLines-todoLines-dialogLines；coordPanel  truncateToLines(panelBudget=topAvailable-minViewportLines)；vpHeight=topAvailable-panelLines（≥1）。todo 亦可截斷（todoBudget=termHeight-reservedLines-dialogLines-1，display-only 譲位 interactive dialog）。優先序：dialog＞todo＞viewport(軟底5)＞panel（最先截）。修此前 minViewportLines 硬底反而總高溢出遮底部。守護測試 coordinator_layout_test.go×2（24/12/10 行總高≤termHeight；viewport 与 panel 共存）＋既有 dialog_visibility 全綠。
- **2026-11-19 等級注入**：`New`/`newAppModel` 在 `mcpPool` 後新增 `tiers commands.TierManager`；建 `CommandContext` 時帶入。狀態列的上下文長度改用 `compact.ResolveContextWindow(model, [main] context_window)`，啟動與 `/model` 切換都一樣。
- 最終審查後補修：啟動時把 `tiers.Warnings()` 逐則插入成 TUI 本地的 system 訊息（不進 engine 歷史，不影響前綴）。
- **2026-11-19 /setup 設定引導**：`setupWizard`（`setupwizard.go`）是純狀態機，`handleKey` 回傳動作，網路與寫檔由 `setup_dialog.go` 以 `tea.Cmd` 執行（Update 不做 I/O）。每次發出或放棄請求都會遞增 `gen`，回來的 `setupModelsMsg`/`setupAppliedMsg` gen 不符就丟棄（例如驗證中按了 Esc）。`tea.PasteMsg` 在 `dialogSetup` 時交給 wizard，讓 API key 可以貼上。`tui.New`/`newAppModel` 在 `tiers` 後新增 `setup commands.SetupBackend`；`NeedsSetup()` 為真時啟動訊息提示輸入 /setup（TUI 本地訊息，不進 engine 歷史）。
- **2026-11-22 /tier 互動選單**：不帶參數的 `/tier` 開 `dialogTier`。`tierPicker`（`tierpicker.go`）同樣是純狀態機，I/O 在 `tier_dialog.go` 的 `tea.Cmd`：開啟時對每個有金鑰的 provider 呼叫 `commands.TierModelLister.ListProviderModels`（`m.tiers` 的選用介面，bootstrap 的 `tierManager` 實作），選定後 `BindWithLimits`/`Unbind` 寫回 config.toml。候選＝endpoint 列出的模型 ∪ `[providers.<id>.models]` 宣告的模型；搜尋字串為 `已知provider:模型` 且不在清單時提供自訂列。`tierSeq` 遞增使已關閉選單的結果被丟棄。
- **2026-11-20 /model 選單**：`modelPicker` 列出 default 與等級（不可用的列顯示原因、游標跳過）；對話已有內容時先開 `dialogModelConfirm` 說明快取失效（≈token 數、窗口不足時警告），y 才 `Apply`。查詢中拒絕切換；確認框開著時不再疊對話框。`ModelSwitcher` 為 nil 時維持舊的 `SetModel` 行為。
- **2026-11-20 /btw**：`/btw` 在送出時先於「查詢中排隊」被攔下並立即開始（`btw.go`）。主腦在跑時讀 `mainSnap`（engine 寫入的快照），閒置時用 `GetMessages()` 與 `buildMainSystemPrompt`（從 `startQueryCmd` 抽出），讓前綴與主腦一致。btw 的串流以 `listenBTW` 逐段讀進 TUI 本地區塊（`btwMessageID`，不進 engine 歷史與 transcript）。Esc 先停 btw、再按才中斷主腦；ctrl+y 交給主腦：執行中入列（`Origin` 標記），閒置或沒被接走時存為 `btwPending`，附加到下一則使用者訊息。`tui.New`/`newAppModel` 在 `models` 後新增 `btwTarget` 參數。
- **2026-11-20 每輪耗時（turntiming.go）**：所有查詢起點（直送、queue 的 Prompt/ExternalPrompt、slash `DisplayNone`）統一走 `beginQuery()`，記 `turnStart` 並歸零 `turnStats`；`StreamDoneMsg`／`StreamErrorMsg`／`doAbort` 呼叫 `finishTurn(done|error|interrupted)`。用實際時鐘（可注入 `now`）而非 spinner tick 累加，因為 tick 會漂移。done/interrupted 附加 TUI 本地系統訊息（`✻ Worked for …`／`✻ Interrupted after …`），不進 engine 歷史與 transcript，不動前綴。每輪一筆 JSONL 以 `tea.Cmd` 附加到 `~/.glaude/turn-timings.jsonl`（全域檔，`cwd` 欄位區分專案；不記 prompt 內容；寫檔失敗靜默）。`turnLogPath` 只在 `tui.New` 設定，`newAppModel`（測試路徑）為空即不寫檔。`finishTurn` 在沒有 `turnStart` 時是 no-op，所以中斷後、下一輪開始前抵達的過期結束事件不會重複記錄；若過期事件在新一輪已開始後才到，會提早結束新一輪（`StreamDoneMsg` 沒有 queryGen 比對，屬既有問題）。`input_tokens` 不含快取命中，另記 `cache_read_tokens`／`cache_creation_tokens`。
- **2026-11-20 `!` bash 模式（bashmode.go）**：`handleSubmit` 在 `/btw` 之後、排隊判斷之前攔下 `!`，查詢進行中也立即執行（不排隊）。執行用 `shell.Run` 在 `appState` 的 live WorkingDir，不走權限鏈。結果顯示為 TUI 本地訊息（code fence 長度自動大於輸出中的反引號串），同時把 `<bash-input>/<bash-stdout>/<bash-stderr>` 存進 `bashPending`，由 `startQueryCmd` 接在下一則使用者文字前面（所有查詢起點都經過它，含佇列送出）。**不**立刻寫進 engine 歷史：注入在 turn 尾部不動快取前綴、不產生連續 user 訊息、不多打 API；代價是送出下一句前就退出或 resume，這段輸出會遺失。每段輸出各截到 30,000 rune。只有一個 `bashCancel` 槽：Esc 先停 `!`、再按才中斷主腦；閒置時 Esc 連同 `btwPending` 一起丟棄。關閉補全選單或離開 vim insert 模式的 Esc 屬於輸入框，不取消 `!`、不丟 pending（`forInput`）。`bashGen` 在每次啟動、`/clear` 與 `/resume` 遞增，過期結果丟棄；`/resume` 也清掉 pending，避免舊對話的輸出流進新對話。顯示端同樣截到 30,000 rune，避免大量輸出拖慢渲染。輸入以 `!` 開頭時提示符號改成 `!`、邊框改用 `theme.Warning`。守護測試 bashmode_test.go。
- **2026-11-20 ctx 用量條字元**：`contextMeter` 由 `▰/▱` 改成 `█/░`。前者在部分終端字型缺字，會被替代字型畫成寬度不一的方框，狀態列看起來像亂碼；`█/░` 幾乎所有等寬字型都有。守護測試 TestStatusBar_ContextMeterUsesBlockGlyphs。
- **2026-11-20 狀態列模型來源**：`StatusBar` 加 `defaultModel`（啟動時的 `MainLoopModel.ModelID`）與 `restartModel`（`/setup` 寫入的主模型，經 `setupAppliedMsg.mainModel` 帶回，只要 `SettingsWritten` 就設定，config.toml 寫入失敗也一樣，因為主模型在 settings.json）。`/model` 切換不寫回設定檔、`/setup` 要重開才生效，兩者都會讓「實際在用的模型」與設定檔不一致，所以模型名後標示 `(session · default X)`／`(restart → Y)`。寬度不夠放「完整右側＋8 欄 cwd」時縮成 `model*`。純顯示，不動前綴。守護測試 statusbar_source_test.go×9。
- **2026-11-21 Shift+Enter 換行**：輸入框的 Enter 仍送出。`promptNewline` 把 Alt+Enter 與 Shift+Enter 都插成換行；補全選單開著時也一樣，不選指令、不送出。Shift+Enter 要終端回報修飾鍵才分得出來（Bubble Tea v2 預設會要求 Kitty keyboard protocol／modifyOtherKeys）；不支援的終端會把 Shift+Enter 當成 Enter。提示文字改為 shift+enter newline。純按鍵分派，不動 system 前綴、不加大窗口、不新增 API 呼叫。守護測試 TestShiftEnter_InsertsNewline、TestShiftEnter_WithCompletionInsertsNewline。
- **2026-11-20 貼上收合（送出時展開）**：`pasteStore` 跟著整個會話活著（`newAppModel` 建立，`/clear` 也不換）。標記可能先留在輸入框、佇列、`!` 或 `/btw`，要等真正送進模型才對得回原文；中途換 store，那些還沒送出的標記就變成無法展開的字面。展開只發生在使用者自己的輸入：主迴圈直送、`Origin == ""` 的佇列訊息，以及 `/btw` 的問題。btw 筆記、`!` 的輸出、外部事件（`ModeExternalPrompt`）與 slash `DisplayNone` 的結果都不展開——`takeBTWPending`／`takeBashPending` 接在展開之後，避免把模型或工具塞回來的字樣再當成貼上。圖片只附在一般訊息與 `/btw`；`!`、`# ` 與 slash 只展開文字，並在既有 hint 列提示。顯示與歷史仍留標記。單趟 `ReplaceAllStringFunc`，剛代入的原文不再掃。展開改的是這一則使用者訊息（turn 尾部），不移動 system 前綴、不加大穩定窗口、不新增 API 呼叫。
- **2026-11-20 工具邊界的佇列貼上**：查詢進行中送出的使用者訊息，在入隊時把 `expand` 的結果寫進 `QueuedCommand.Content`，Value 維持標記。回合之間的 `processQueueCmd` 仍從 Value 展開，不用這份快照。`!` 的氣泡顯示未展開的那一行；行程與 `<bash-input>` 用展開後的文字，圖片標記留字面。slash 先依未展開字串解析名稱與參數，只對參數做 `expandTextOnly`；原始名稱不是已註冊命令時，不執行從展開本文變出來的命令。
- **2026-11-20 Ctrl+V**：停用 textarea 內建貼上（`KeyMap.Paste`）。讀取是 `tea.Cmd`，不在 `Update` 裡同步執行。20 張上限在結果回到時才算：engine 歷史的 image block（含 `tool_result` 的一層 `Content`）加上輸入框裡不重複的有效圖片標記。對話框開著時，晚到的結果丟棄。標記插在當下游標，不寫進已送出的訊息。圖片只屬於那一則使用者訊息（不移動 system 前綴、不新增 API 呼叫）。
- **2026-11-20 中止後排隊圖片仍計入上限**：閒置時的上限另計 `unsentUsers` 裡尚未寫回的標記（Esc 或串流錯誤不清佇列，含 `idx >= inflightBase`），不掃一般歷史文字；載入中的公式不變。直接送出的 in-flight 氣泡在 `releaseWrittenUser` 丟掉 `inflightUser` 之後、engine defer 寫回 `GetMessages` 之前的短窗可能沒被算到。
- **2026-11-22 滑鼠選取與複製**：滑鼠模式維持 CellMotion（滾輪捲動照舊）。左鍵在訊息 viewport 內拖曳＝選取，放開即複製；雙擊（≤400ms 同格）選一個字（英數字＋`_`，或連續 CJK）；`Esc`（無對話框時）清選取後照原本流程繼續（中斷回應、停 `!`、離開 vim insert 不必多按一次）；寬度改變清選取。選取存內容座標（`contentLines` 的行、cell 欄），所以捲動與串流追加不會錯位；`viewportGeometry` 是 View 與命中判定共用的唯一 offset 與「實際畫出列數」來源（尾端空白列不畫、下方面板上移），兩邊不可各算各的。取字去 ANSI、去行尾空白、去共同縮排，並跳過程式碼標題列。assistant text block 的每個程式碼區塊上方插 `── <lang> ── [copy] ──`（goldmark 抽原文、對 glamour 輸出比對前 3 個非空行（連續、完全相等，折行可前綴）定位，找不到就略過），位置 `codeBlockRef` 隨 render cache entry 存；點 `[copy]` 複製原文。系統提示（※）不插標題列。複製同時送 `tea.SetClipboard`（OSC 52）與 `clipboard.WriteText`（原生；失敗忽略），提示走 `pasteNotice`。純 TUI 顯示與本機剪貼簿，不動 prompt、工具、請求。
- **2026-11-23 權限回應不阻塞 Update（fix/repetitive-replies）**：`listenForPermissionRequest` 的 RespFn 在 Update 內執行，原本直接寫入容量 1 的 respCh。若詢問的呼叫者已放棄（查詢取消），沒人讀 respCh，第二個過期回答會讓 **UI 卡死**。現在先嘗試非阻塞送出，滿了就交給 goroutine；checker 端會丟棄 ID 不符的回應（見 permissions Design Notes）。
