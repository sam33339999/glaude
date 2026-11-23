---
package: bootstrap
import_path: internal/bootstrap
layer: cli
generated_at: 2026-11-23T08:02:40Z
source_files: [acp.go, agents_cmd.go, auth.go, bootstrap.go, install_cmd.go, mcp.go, mcp_connect.go, mcp_health.go, mcp_help.go, mcp_list.go, mcp_parse.go, mcp_render.go, mcp_run.go, misc.go, modelswitch.go, plugin.go, plugin_help.go, plugin_install.go, plugin_marketplace.go, plugin_render.go, plugin_run.go, plugin_validate.go, provider.go, root.go, run.go, session.go, setup.go, tiers.go, tools.go, update_cmd.go, wire.go]
---

# internal/bootstrap

> Layer: **CLI** · Files: 31 · Interfaces: 0 · Structs: 2 · Functions: 8

## Structs

- **AppContainer** — 22 fields: QueryEngine, AppStateStore, ToolRegistry, MCPPool, Settings, PermAskCh, PermRespCh, QuestionAskCh, ...
- **ContainerOptions** — 10 fields: HomeDir, WorkingDir, ModelOverride, Verbose, Debug, DebugFile, Headless, ProviderClientFactory, ...

## Functions

- `BuildContainer(opts ContainerOptions) (*AppContainer, error)`
- `BuildContainerWithClient(opts ContainerOptions, client api.Client) (*AppContainer, error)`
- `BuildHeadlessContainer(opts ContainerOptions) (*AppContainer, error)`
- `Execute() error`
- `HandleFastPath(args []string) bool`
- `RegisterBuiltinTools(reg *tools.Registry, pool *agtmcp.Pool)`
- `Run(args []string) error`
- `RunHeadless(container *AppContainer, prompt string, outputFormat string, maxTurns int) error`

## Dependencies

**Imports:** `internal/acpclient`, `internal/agentctx`, `internal/agenttype`, `internal/api`, `internal/codemode`, `internal/commands`, `internal/compact`, `internal/config`, `internal/coordinator`, `internal/engine`, `internal/hooks`, `internal/mcp`, `internal/memdir`, `internal/modeltier`, `internal/msgqueue`, `internal/oauth`, `internal/permissions`, `internal/session`, `internal/soul`, `internal/state`, `internal/tools`, `internal/tools/agent`, `internal/tools/codemode`, `internal/tools/fileops`, `internal/tools/interact`, `internal/tools/mcp`, `internal/tools/memory`, `internal/tools/misc`, `internal/tools/shell`, `internal/tools/tasks`, `internal/tools/web`, `internal/transcript`, `internal/tui`, `pkg/types`, `pkg/utils/fs`

**Imported by:** `cmd/glaude`

<!-- AUTO-GENERATED ABOVE — DO NOT EDIT -->
<!-- MANUAL NOTES BELOW — preserved across regeneration -->

## Design Notes

- **版本输出格式对齐 oracle（2026-08-30，parity A1）**：`--version` 输出从
  `glaude 0.1.0` 改为 `0.1.0 (Claude Code)`。为什么：原版 TS 的格式是
  `<semver> (Claude Code)`（无二进制名前缀、带产品后缀），parity Tier-2
  对齐要求两版格式一致（版本号本身允许不同，由测试归一化处理）。
  `appVersionString()` 单独成函数而非内联，是因为 doctor / update 等
  子命令展示版本时未来可复用同一格式，且 ldflags 注入的 `appVersion`
  保持裸 semver 语义（供 User-Agent 等机器可读场景），展示格式只发生在
  这一个出口。

- **mcp/plugin 命令树共用 commander 语义解析器（2026-09-05，parity B2/B5）**：oracle 的
  CLI 错误与帮助格式来自 commander.js（`error: unknown command 'x'`、`(Did you mean …?)`、
  `error: option '-s, --scope <scope>' argument missing`），与 pflag/cobra 原生格式完全不同。
  因此两棵子树的每个节点都 `DisableFlagParsing: true`，RunE 收到原始 argv 后交给共享的
  parseMCPFlags（mcp_parse.go）按 commander 语义解析。mcp_ 前缀的错误类型
  （mcpUsageError/errMCPExit 等）是共享基础设施而非 mcp 专属——plugin 树直接复用；
  保留 mcp 前缀是因为 B2 先落地，重命名会无谓扰动已测代码。
- **cobra Use 必须是裸命令名，别名走 Aliases（2026-09-05，B5）**：cobra 的 Name() 取 Use
  的首个空白分隔 token，`Use: "install|i …"` 会让主名 install 永远无法路由到该子命令
  （别名机制也不覆盖裸主名），参数落回父命令被父级 flag 解析拦截。测试里专门有
  TestPluginCmdTreeShape 钉住子命令名集合，防止回归。
- **失败自打印 + ErrSilent 静默退出（2026-09-05，B2/B5）**：run 层失败自行向 stderr 输出
  （✘ 前缀、无 "Error:" 包装），随后返回 ErrSilent 让 main 静默 exit 1。oracle 的约定是
  错误文本自带全部上下文，cobra 默认再包一层前缀会破坏字节级对齐；errMCPExit 则表示
  诊断已打印、只关心退出码（plugin validate 的失败路径）。
- **已记录的分歧（B5，见 test/parity/cases.md）**：远程 marketplace 源（owner/repo、
  https://、npm:、--claudeai）与 `plugin details/eval/init/prune/tag` 未实现——前者给出
  诚实报错，后者从帮助行与建议名中整体移除；JS JSON 解析错误措辞与 Go 不同（外层
  `Invalid JSON syntax: JSON Parse error: …` 格式一致）；hooks 模块文件只做存在性检查，
  不按 JS 加载。
- **2026-11-18 BF-01 MCP 启动接线（桩回填）**：`RegisterBuiltinTools` 签名新增
  `pool *agtmcp.Pool` 参数（最小接口变更，可为 nil），MCP 资源工具构造期注入 pool。
  `BuildContainer`/`BuildContainerWithClient` 先建 pool 再注册工具，随后
  `connectConfiguredMCPServers`（mcp_connect.go）连接已配置服务器并经
  `mcptools.RegisterServerTools` 一次性注册各服务器工具（会话钉住，见
  internal/tools/mcp CONTEXT.md 的 cache-stability 决策）。服务器选择沿用
  `mcp list` 语义（ListMCPServers），另加 project 作用域批准门
  （enableAllProjectMcpServers / enabledMcpjsonServers）。连接失败非致命：
  状态记录在 pool（供 /mcp 展示），错误仅在 Debug 模式打印。
  **agtkeel 内核约束**：jsonRPCClient 的后台读协程绑定首次调用（initialize）
  的 ctx，故健康连接的 Connect 必须用长生命周期 context。启动等待用看门狗
  （connectWithWatchdog）约束：成功则保留该 ctx（其生命周期即连接生命周期）；
  超时/取消则 cancel 并**等待 Connect goroutine 完全收尾**后才返回——所有
  pool 写入在 boot 返回前终结，会话期间无后台 pool 变更（CR-BF01-1）。
  超时服务器本会话视为失败：钉住模型下迟到连接的工具本就不会注册进本会话，
  旧「迟到落地收益」注释已删除（其收益是虚幻的，且制造 data race）。

- **2026-11-18 BF-01-F1 有界且不遗弃的 boot 连接（CR-BF01-1 P0 / CR-BF01-2 P1 修复）**：
  connectWithWatchdog 改为 cancellable ctx（无 deadline）＋外部看门狗；超时分支
  cancel 后阻塞收 `<-done`，若握手恰好竞速成功则同步 Disconnect（仍在 boot 内），
  杜绝会话期 pool 写入与 /mcp、资源工具无锁读取的 data race（CR-BF03-2 升级裁决的
  触发路径）。`mcpConnectTimeout` 改为 var 供测试注入（生产 30s）。drain 有界性依据：
  agtkeel jsonrpc.call 与 transport 均 select ctx.Done，cancel 传导至握手使 Connect 返回。
  内核根因（reader 绑定 handshake ctx、HTTP Close 仅置旗标）登记跨仓债务 BF-19/CR-BF01-7。
  RegisterServerTools 的 boot ListTools 加每服务器 deadline（listToolsTimeout，生产 10s，
  可注入），hung server 不再无限阻塞启动；超时为非致命单服务器失败。
  回归测试：慢握手 bounded+drained、caller-cancel drain、慢 list 非致命（原超时分支 0% 覆盖已补）。
- **2026-11-18 BF-03（Agent-TUI 最小接线）**：`runInteractive` 的 `tui.New(...)` 调用追加传入 `container.MCPPool`，对应 TUI 层新增的 `mcpPool commands.MCPServerLister` 注入参数（/mcp 斜杠命令展示真实连接状态所需）。本包仅此一行变更，无其他逻辑改动；接口变更详情见 `docs/project/design/tui.md` §4.1。
- **2026-11-18 BF-18 持久化接线**：openTranscript（session.go）统一建立 recorder——resume/continue 以已加载历史为基线，否则新建；失败非致命（stderr 警告＋nil）。runInteractive 经 tui.New 注入并于退出 Close；runHeadless 于 headlessRun 返回后 Record 完整对话再 Close。loadSessionMessages/resumeSessionByID/continueMostRecentSession 签名改为返回 (sessionID, messages, error)（内部函数，测试已同步）。
- **2026-11-18 BF-06 CLI 三命令（oracle 形态对齐）**：agents 单一列表命令＋--setting-sources（User/Project/Built-in 分组＋shadow 标记，对齐 oracle main.tsx；local 源接受但无目录——分歧文档化）；update＋upgrade 别名经 GitHub releases 检查版本（endpoint vars 可注入，compareVersions 数值比较），v1 不自安装（提示 glaude install）；install [target] 下载 goreleaser archive 解压原子安装 ~/.local/bin（--force、PATH 提示、tar.gz/zip 双格式）。releaseSource vars 为测试注入点。misc.go 仅留 doctor。
- **2026-11-18 BF-07 TodoWrite 注册点变更**：TodoWrite 移出 RegisterBuiltinTools（余 33），wire.go 两条路径于 store 建立后 reg.Register(interact.NewTodoWriteTool(store))——工具需 live store，注册顺序约束文档化。
- **2026-11-18 BF-08 AskUserQuestion 接线**：ContainerOptions.Headless（runHeadless 设 true）；Phase 7 question channel 对（headless nil→tool 诚实报错）＋注册；AppContainer.QuestionAskCh/RespCh；RegisterBuiltinTools 余 32（TodoWrite/AskUserQuestion 均 wire.go 注册）。
- **2026-11-18 BF-09/BF-21**：plan-mode 工具 store 后注册（RegisterBuiltinTools 余 30）；CheckerConfig.ModeFn 接 store.GetState().ToolPermissionContext.Mode（两条路径）。
- **2026-11-18 BF-10**：worktree 工具 store 後註冊；RegisterBuiltinTools 餘 28（interact 全數移出至 wire.go）。
- **2026-11-18 UPD-01**：tea.NewProgram 移除 WithAltScreen/WithMouseCellMotion（v2 改在 AppModel.View 的 tea.View 上设置）。
- **2026-11-18 EV-01**：pool.SetNotificationHandler→mq.Enqueue(ModeExternalPrompt, PriorityNext)（两条路径），MCP 服务端推送事件驱动唤醒，零轮询。
- **2026-11-18 sub-agent cache 路徑（feat/coordinator-fork-cache）**：buildRunAgentFn 全 sub-agent NoWriteBack=true（修此前 sub-agent 結束覆蓋主對話 e.messages 的污染 bug）；fork 經 buildForkQuery 繼承父 CacheSafeParams 前綴＋尾部 FormatForkDirective，ExcludeTools=nil（完整工具列表＝cache bytes 一致），QuerySource=background（529 即放棄，避免容量級聯）。runStates sync.Map 以 AgentID 保留最終 QueryParams（Messages=收集歷史），SendMessage resume 時原樣重用（system/tools/model 不變＝cache 連續性）；collectSubAgentRun 收集 assistant/user 訊息。無快照時 fork 退化為 boilerplate-as-system-prompt 的 one-shot agent。
- **2026-11-18 WorkingDir 接線（feat/agent-cwd）**：buildRunAgentFn 改收 *state.AppStateStore（原 state.AppState 快照）——每次 spawn live 讀 WorkingDir＋MainLoopModel（/model、EnterWorktree 切換後生效），sub-agent uctx.WorkingDir 繼承會話目錄。headlessRun useCtx 亦注入 AppStateStore.WorkingDir。兩條建構路徑（生產＋test）同步。
- **2026-11-19 sub-agent model alias 修正接線（fix/subagent-model-alias）**：buildSubAgentQuery +provider 參數，call site 傳 st.Settings.Provider（live）；explore 的 haiku alias 在 openai provider 不再直送（404 根因）。fork 路徑不受影響（本就繼承父 model）。
- **2026-11-19 完成事件接線（feat/coordinator-completion-wake）**：buildOnStatusChangeFn 适配 OnStatusChangeFn 新簽名（+result/summary），填入 Event.Result/Summary 供 TUI 建唤醒通知。
- **2026-11-19 sub-agent result 空值修復（fix/subagent-result-layout）**：collectSubAgentRun 原僅累加 MsgTypeStreamText 為 result——tool-only 結尾/max_turns 截斷/stream delta 缺失時 result 空→DefaultSummaryGenerator(空)→summary 也空（使用者見双空）。現追踪 lastAssistantText（最近帶 text 的 assistant message，權威最終報告），result=lastAssistantText 非空否則 fallback streamText。副效：result 改取「最後一個 text assistant message」而非全 turn stream 累加（更贴近 final report，不含中間 narration）。+subAgentResult/assistantTextOf helper。守護測試 wire_result_test.go×3。
- **2026-11-19 sub-agent 等級路由（`tiers.go`）**：兩條建構路徑都經 `wireTiers` 載入 config.toml、為每個 provider 建 client（不連網），`main` 鍵放主腦 client；`ContainerOptions.ProviderClientFactory` 供測試注入。`AppContainer.Tiers` 交給 TUI 的 `/tier`。
  - `resolveSubAgentTarget` 候選順序不變（env > Agent 工具 model > profile > 繼承）。命中已綁定等級 → 帶 `Client`/`Model`/`ContextWindow`/`MaxOutputTokensOverride`；未綁定的 lite/pro/max → 轉回舊別名走原本規則（openai 下 explore 照舊繼承，`TestBuildSubAgentQuery_OpenAIExploreAliasFallback` 不變）；未綁定自訂等級 → 繼承；其他 → 原本的 `ResolveModelForProvider`。
  - provider 的金鑰環境變數沒設：派工直接失敗並點名變數，**不**退回主腦（反目標：請求默默走錯 provider）。
  - 續跑沿用 `runStates` 裡派工當下的 `Client`/`Model`/`CompactTracker`（快取連續性），不受之後 `/tier` 影響。fork 永遠不帶 `Client`。每個 sub-agent 都有自己的 `compact.Tracker`。
  - config.toml 警告一律印到 stderr（不只 `--debug`），因為設定錯誤需要使用者看到。
  - 段落審查後補修：`/tier` 重新讀檔後，可能綁到啟動後才加入的 provider（沒有 client）；派工時直接報錯要求重啟，絕不退回主腦 client。
- **2026-11-19 `setupBackend`（`setup.go`）**：實作 `commands.SetupBackend`。現值**每次從全域 `settings.json` 重新讀**（不用合併後的設定，避免把專案層、環境變數或 OAuth token 預填並寫進全域檔；同一會話跑第二次 /setup 也看得到上一次的結果）。沿用的金鑰在這裡解析，不經過 TUI。寫 `config.toml` 用另外從磁碟載入的 Store，不動執行中那一個：等級和主腦一樣等重新啟動才生效。`Overrides()` 只回報來源與欄位名，不回顯值。`NeedsSetup` 取自 `resolveAPIKey` 是否回空字串。
- **2026-11-20 /model 與派工凍結**：`modelSwitcher`（`modelswitch.go`）在 default（啟動時目標）與等級之間切換，存的是解析後的目標快照，之後 `/tier` 改綁定不影響主腦。不可用（未綁定、金鑰缺失、啟動後才加入的 provider）的目標不會被套用。派工時把當下主腦目標寫進 sub-agent params：inherit 完整凍結；別名或明確模型也凍結 client 與 max_tokens；fork 用快照的 client 與上限。`main:<模型>` 等級固定用啟動時的 client。主腦切到非啟動 provider 後，舊別名一律繼承主腦（不知道那裡有沒有 haiku 之類的模型）。
- **2026-11-20 /btw --tier**：`modelSwitcher.TargetFor` 用與 /model 相同的解析取得目標（不切換主腦），經 `btwTargetFn` 注入 TUI。
- **2026-11-22 ACP `agy` 內建與派工模型轉發**：`withACPDispatch` 改呼叫 `mgr.PromptWithModel(..., req.Model, ...)`，使 `Agent` 工具的 `model` 參數與 `/<agent> -m <model>` 都能切換外部 ACP session 的模型。
