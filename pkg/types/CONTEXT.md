---
package: types
import_path: pkg/types
layer: types
generated_at: 2026-11-24T06:00:34Z
source_files: [command.go, hooks.go, ids.go, logs.go, message.go, permissions.go, plugin.go, question.go, todo.go, types.go]
---

# pkg/types

> Layer: **Types (zero-dep)** · Files: 10 · Interfaces: 2 · Structs: 30 · Functions: 3

## Interfaces

### AppStateReader (3 methods)
> AppStateReader is the read-only AppState access interface used by commands

```go
type AppStateReader interface {
    GetPermissionContext() ToolPermissionContext
    GetModel() string
    GetVerbose() bool
}
```

### MCPConnection (2 methods)
> MCPConnection is a forward-declaration interface for an MCP server connection.

```go
type MCPConnection interface {
    ID() string
    IsConnected() bool
}
```

## Structs

- **AdditionalWorkingDirectory** — 2 fields: Path, Source
- **CacheControl** — 1 fields: Type
- **CommandBase** — 12 fields: Name, Description, Type, Source, Aliases, IsHidden, IsMCP, ArgumentHint, ...
- **ContentBlock** — 13 fields: Type, CacheControl, Text, ID, Name, Input, ToolUseID, Content, ...
- **EntryEnvelope** — 2 fields: Type, Raw
- **HookCommand** — 11 fields: Type, Command, Args, Shell, If, OnFailure, Async, AsyncRewake, ...
- **HookMatcher** — 2 fields: Matcher, Hooks
- **ImageSource** — 4 fields: Type, MediaType, Data, URL
- **LoadedPlugin** — 3 fields: Config, Commands, Hooks
- **LocalCommand** — 3 fields: SupportsNonInteractive, Call (embeds CommandBase)
- **LocalCommandContext** — 3 fields: AppState, SessionId, WorkingDir
- **LocalCommandResult** — 2 fields: Type, Value
- **LogOption** — 7 fields: SessionId, Date, FirstPrompt, IsSidechain, Messages, Title, GitBranch
- **Message** — 6 fields: ID, Role, Content, UUID, Timestamp, SessionId
- **PermissionDecision** — 5 fields: Behavior, Message, UserModified, Suggestions, BlockedPath
- **PermissionRule** — 3 fields: Source, RuleBehavior, RuleValue
- **PermissionRuleValue** — 2 fields: ToolName, RuleContent
- **PermissionUpdate** — 6 fields: Type, Destination, Rules, Behavior, Mode, Directories
- **PluginConfig** — 3 fields: Name, Enabled, Options
- **PluginError** — 3 fields: PluginName, Message, Fatal
- **PromptCommand** — 8 fields: ContentLength, ArgNames, AllowedTools, Model, Context, Agent, GetPrompt (embeds CommandBase)
- **QuestionRequest** — 3 fields: ID, Question, Options
- **QuestionResponse** — 3 fields: ID, Answer, Dismissed
- **SerializedMessage** — 8 fields: Message, CWD, UserType, SessionId, Timestamp, Version, GitBranch, Slug
- **SummaryEntry** — 3 fields: Type, Summary, LeafId
- **TodoItem** — 4 fields: ID, Content, Status, Priority
- **ToolCall** — 3 fields: ID, Name, Input
- **ToolPermissionContext** — 6 fields: Mode, AdditionalWorkingDirectories, AlwaysAllowRules, AlwaysDenyRules, AlwaysAskRules, IsBypassPermissionsModeAvailable
- **ToolResult** — 3 fields: ToolUseId, Content, IsError
- **TranscriptMessage** — 4 fields: SerializedMessage, ParentUUID, IsSidechain, AgentId

## Functions

- `AsAgentId(s string) (AgentId, error)`
- `AsSessionId(s string) SessionId`
- `WireContent(blocks []ContentBlock) []ContentBlock`

## Constants

- `BehaviorAllow`
- `BehaviorAsk`
- `BehaviorDeny`
- `CommandSourceBuiltin`
- `CommandSourceBundled`
- `CommandSourceMCP`
- `CommandSourcePlugin`
- `CommandSourceSkills`
- `CommandTypeLocal`
- `CommandTypeLocalJSX`
- `CommandTypePrompt`
- `ContentTypeImage`
- `ContentTypeText`
- `ContentTypeThinking`
- `ContentTypeToolResult`
- `ContentTypeToolUse`
- `EntryTypeAssistant`
- `EntryTypeBash`
- `EntryTypeBranchSwitch`
- `EntryTypeCompletion`
- `EntryTypeContextCompact`
- `EntryTypeDebug`
- `EntryTypeError`
- `EntryTypeImage`
- `EntryTypeInfo`
- `EntryTypePRLink`
- `EntryTypeProgress`
- `EntryTypeResult`
- `EntryTypeSessionEnd`
- `EntryTypeSessionStart`
- `EntryTypeSummary`
- `EntryTypeSystem`
- `EntryTypeTag`
- `EntryTypeThinking`
- `EntryTypeToolResult`
- `EntryTypeToolUse`
- `EntryTypeTranscript`
- `EntryTypeUser`
- `EntryTypeWorktree`
- `HookNotification`
- `HookPostSampling`
- `HookPostToolUse`
- `HookPostToolUseFailure`
- `HookPreSampling`
- `HookPreToolUse`
- `HookSessionEnd`
- `HookSessionStart`
- `HookStop`
- `HookSubagentStop`
- `PermissionModeAcceptEdits`
- `PermissionModeAuto`
- `PermissionModeBubble`
- `PermissionModeBypassPermissions`
- `PermissionModeDefault`
- `PermissionModeDontAsk`
- `PermissionModePlan`
- `PermissionUpdateAddDirectories`
- `PermissionUpdateAddRules`
- `PermissionUpdateRemoveDirectories`
- `PermissionUpdateRemoveRules`
- `PermissionUpdateReplaceRules`
- `PermissionUpdateSetMode`
- `RoleAssistant`
- `RoleUser`
- `RuleSourceCLI`
- `RuleSourceCommand`
- `RuleSourceFlag`
- `RuleSourceLocal`
- `RuleSourcePolicy`
- `RuleSourceProject`
- `RuleSourceSession`
- `RuleSourceUser`
- `TodoCompleted`
- `TodoInProgress`
- `TodoPending`

## Change Impact

**Exported type references (files that use types from this package):**
- `AdditionalWorkingDirectory` → `internal/bootstrap/wire.go`, `internal/state/store.go`
- `AppStateReader` → `internal/state/store.go`
- `CacheControl` → `internal/engine/query.go`
- `ContentBlock` → `internal/bootstrap/acp_integration_test.go` (test), `internal/bootstrap/run.go`, `internal/bootstrap/session_test.go` (test), `internal/bootstrap/wire.go`, `internal/bootstrap/wire_fork_test.go` (test) + 51 more
- `EntryEnvelope` → `internal/commands/resume.go`, `internal/session/store.go`, `internal/session/store_test.go` (test)
- `HookCommand` → `internal/config/loader_test.go` (test), `internal/engine/tool_hooks_test.go` (test), `internal/hooks/hooks.go`, `internal/hooks/hooks_test.go` (test), `internal/tools/codemode/hooks_test.go` (test)
- `HookMatcher` → `internal/config/loader.go`, `internal/config/loader_test.go` (test), `internal/engine/tool_hooks_test.go` (test), `internal/hooks/hooks.go`, `internal/hooks/hooks_test.go` (test) + 1 more
- `ImageSource` → `internal/compact/images_test.go` (test), `internal/engine/btw_test.go` (test), `internal/engine/tool_images_test.go` (test), `internal/tui/clipboard_paste.go`, `internal/tui/clipboard_paste_test.go` (test) + 4 more
- `LoadedPlugin` → `internal/plugin/plugin.go`, `internal/state/store.go`
- `MCPConnection` → `internal/state/store.go`
- `Message` → `internal/bootstrap/acp_integration_test.go` (test), `internal/bootstrap/run.go`, `internal/bootstrap/session.go`, `internal/bootstrap/session_test.go` (test), `internal/bootstrap/wire.go` + 69 more
- `PermissionRule` → `internal/bootstrap/wire.go`
- `PluginConfig` → `internal/plugin/plugin.go`
- `PluginError` → `internal/plugin/plugin.go`
- `QuestionRequest` → `internal/bootstrap/wire.go`, `internal/tools/interact/interact.go`, `internal/tools/interact/interact_test.go` (test), `internal/tui/init.go`, `internal/tui/model.go` + 1 more
- `QuestionResponse` → `internal/bootstrap/wire.go`, `internal/tools/interact/interact.go`, `internal/tools/interact/interact_test.go` (test), `internal/tui/init.go`, `internal/tui/model.go` + 1 more
- `SerializedMessage` → `internal/bootstrap/session.go`, `internal/bootstrap/session_test.go` (test), `internal/commands/resume.go`, `internal/commands/resume_test.go` (test), `internal/transcript/recorder.go` + 2 more
- `TodoItem` → `internal/state/store.go`, `internal/tools/interact/interact.go`, `internal/tools/interact/interact_test.go` (test), `internal/tools/interact/todo_append_test.go` (test), `internal/tui/coordinator_layout_test.go` (test) + 3 more
- `ToolPermissionContext` → `internal/bootstrap/acp_test.go` (test), `internal/bootstrap/wire.go`, `internal/permissions/ask_concurrency_test.go` (test), `internal/permissions/checker.go`, `internal/permissions/checker_test.go` (test) + 6 more

## Dependencies

**Imports:** *(none — zero-dependency)*

**Imported by:** `internal/bootstrap`, `internal/commands`, `internal/compact`, `internal/config`, `internal/engine`, `internal/hooks`, `internal/memdir`, `internal/msgqueue`, `internal/permissions`, `internal/plugin`, `internal/session`, `internal/state`, `internal/tools/codemode`, `internal/tools/interact`, `internal/transcript`, `internal/tui`, `pkg/utils/ids`

<!-- AUTO-GENERATED ABOVE — DO NOT EDIT -->
<!-- MANUAL NOTES BELOW — preserved across regeneration -->

## Design Notes

- **2026-11-18 内核搬回（反转 PR #31）**：本包原于 2026-09-23 逐字提取至外部仓 github.com/tunsuy/agtkeel；因该仓停止维护，经人类决策整併回本仓原始路径。来源：agtkeel main 3a77085＋后续提交，零逻辑改动（仅 import 路径恢复＋gofmt）。内核改动自此直接在本仓进行。
- **2026-11-18 CacheControl（feat/coordinator-fork-cache）**：ContentBlock +CacheControl *CacheControl（json:"cache_control,omitempty"）——Anthropic prompt-cache 斷點標記，**request-side only**：engine 在序列化前 clone 最後 user message 的 content 再掛標記，歷史快照（GetMessages、CacheSafeParams.ContextMessages）绝不帶標記，避免滾動斷點污染前綴 bytes。零第三方依賴維持。
