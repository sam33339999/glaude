---
package: codemode
import_path: internal/tools/codemode
layer: tools
generated_at: 2026-11-23T08:02:40Z
source_files: [approvals.go, bridge.go, replay.go, snippets.go, state.go, tool.go]
---

# internal/tools/codemode

> Layer: **Tools** · Files: 6 · Interfaces: 0 · Structs: 0 · Functions: 1

## Functions

- `NewCodeModeTool(reg *tools.Registry, checker permissions.Checker, engine codemode.Runtime, dataDir string) tools.Tool`

## Dependencies

**Imports:** `internal/codemode`, `internal/permissions`, `internal/tools`

**Imported by:** `internal/bootstrap`

<!-- AUTO-GENERATED ABOVE — DO NOT EDIT -->
<!-- MANUAL NOTES BELOW — preserved across regeneration -->

## Design Notes

- **2026-11-18 CM-01 CodeMode 工具**：bridge 完整复用引擎权限流程（ValidateInput→CheckPermissions→RequestPermission HIL→Call），沙箱≠授权；内部拒绝以 JS Error 拒绝可 catch。结果 32KiB cap＋截断提示；字串 return 原样、其他 JSON。description 含紧凑 catalog（首行描述），完整 schema 经沙箱内 describe 按需取。wire.go 于 checker 后注册（非 RegisterBuiltinTools）。
- **2026-11-18 CM-02 v2**：NewCodeModeTool +dataDir 参数（history/spill/modules 根，wire 传 ~/.glaude/codemode）。批次批准：CanUseTool 分流——Ask→用户批准→execAllow[tool]（执行 scope，不持久化）；直接 Allow/Deny 不缓存（live 模式切换即时生效）。结果 >32KiB spill 到文件＋4KiB preview＋指针。history.jsonl 追加＋codemode.history(limit) 复用。受控 import：dataDir/modules/<flat>.js，穿越拒绝、256KiB cap。typescript 输入字段。
- **2026-11-18 CM-03 v3**：拆档 state.go（execState 共享执行状态）/bridge.go（authorize＋writeGate）/replay.go（findHistory＋runNested 深度≤3＋sandboxOptions）/snippets.go/approvals.go。input +replay/save_as/batch_writes（required 改运行时验证 code-或-replay）。持久批准仅 CanUseTool==Ask 时咨询（Deny 优先、live 模式即时生效）；AlwaysInCodeMode→approvals.json；write 闸门首个写单次 Ask（Note 声明执行级范围）并发写 channel 等待；写调用成功后记 side_effects 审计。巢状执行共享 execAllow/审计。
