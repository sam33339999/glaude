---
package: config
import_path: internal/config
layer: infra
generated_at: 2026-11-25T15:40:49Z
source_files: [loader.go, mainmodel.go, mcpflag.go, mcpstore.go, orderedjson.go, pluginstore.go, settings.go, writer.go]
---

# internal/config

> Layer: **Infra** · Files: 8 · Interfaces: 1 · Structs: 20 · Functions: 24

## Interfaces

### ConfigLoader (1 methods)
> ConfigLoader is the interface for loading layered settings.

```go
type ConfigLoader interface {
    Load() (*LayeredSettings, error)
}
```

## Structs

- **ACPAgentConfig** — 6 fields: Command, Args, Env, Model, Description, Disabled
- **AttributionConfig** — 2 fields: Commit, PR
- **EnvPair** — 2 fields: Key, Value
- **InstalledPluginRecord** — 6 fields: Scope, InstallPath, Version, InstalledAt, LastUpdated, ProjectPath
- **InstalledPlugins** — 2 fields
- **KnownMarketplace** — 4 fields: SourceType, SourcePath, InstallLocation, LastUpdated
- **KnownMarketplaces** — 1 fields
- **LayeredSettings** — 5 fields: User, Project, Local, Policy, Merged
- **Loader** — 2 fields
- **MCPServerEntry** — 2 fields: Name, Raw
- **MCPStore** — 2 fields
- **MainModelSettings** — 4 fields: Provider, BaseURL, Model, APIKey
- **MarketplaceEntry** — 4 fields: Name, Source, Version, Description
- **MarketplaceManifest** — 6 fields: Name, Description, HasOwner, Plugins, RootDir, ManifestPath
- **OrderedMap** — 2 fields
- **PermissionsConfig** — 6 fields: Allow, Deny, Ask, DefaultMode, DisableBypass, AdditionalDirs
- **PluginManifest** — 4 fields: Name, Version, Description, HasAuthor
- **PluginStore** — 2 fields
- **SettingsJson** — 29 fields: Schema, APIKey, APIKeyHelper, BaseURL, Provider, AWSCredentialExport, AWSAuthRefresh, GCPAuthRefresh, ...
- **WorktreeConfig** — 2 fields: SymlinkDirectories, SparsePaths

## Functions

- `AddPermissionRule(projectDir string, list string, rule string) error`
- `ClaudeJsonPath(homeDir string) string`
- `DesktopConfigServers(homeDir string) ([]MCPServerEntry, error)`
- `LoadMarketplaceManifest(rootDir string) (*MarketplaceManifest, error)`
- `LoadMarketplaceManifestFile(path string) (*MarketplaceManifest, error)`
- `LoadOrderedJSON(path string) (*OrderedMap, error)`
- `LoadPluginManifest(dir string) (*PluginManifest, error)`
- `LocalSettingsPath(projectDir string) string`
- `McpJsonPath(projectDir string) string`
- `MergeACPAgentConfig(base ACPAgentConfig, over ACPAgentConfig) ACPAgentConfig`
- `NewLoader(homeDir string, projectDir string) *Loader`
- `NewMCPStore(homeDir string, projectDir string) *MCPStore`
- `NewOrderedMap() *OrderedMap`
- `NewPluginStore(homeDir string, projectDir string) *PluginStore`
- `OrphanCache(installPath string) error`
- `ParseMCPConfigArg(arg string) ([]MCPServerEntry, error)`
- `PluginNamePart(idOrName string) string`
- `PluginTimestamp(t time.Time) string`
- `ScopeLabel(scope string) string`
- `SettingsLocalPath(projectDir string) string`
- `UserSettingsPath(homeDir string) string`
- `ValidateMCPScope(scope string) error`
- `WriteOrderedJSONAtomic(path string, doc *OrderedMap) error`
- `WriteUserMainModel(homeDir string, s MainModelSettings) error`

## Constants

- `ClaudeDir`
- `ClaudeLocalDir`
- `MCPScopeLocal`
- `MCPScopeProject`
- `MCPScopeUser`
- `ManagedSettingsFile`
- `PermissionListAllow`
- `PermissionListAsk`
- `PermissionListDeny`
- `PluginScopeLocal`
- `PluginScopeProject`
- `PluginScopeUser`
- `SessionsDir`
- `SettingsFile`
- `SourceLocal`
- `SourcePolicy`
- `SourceProject`
- `SourceUser`
- `StatsFile`
- `TodosDir`

## Change Impact

**Exported type references (files that use types from this package):**
- `ACPAgentConfig` → `internal/acpclient/spec.go`, `internal/acpclient/spec_test.go` (test), `internal/bootstrap/acp.go`, `internal/bootstrap/acp_test.go` (test)
- `EnvPair` → `internal/bootstrap/mcp_health.go`, `internal/bootstrap/mcp_render.go`, `internal/bootstrap/mcp_render_test.go` (test), `internal/bootstrap/mcp_run.go`
- `InstalledPluginRecord` → `internal/bootstrap/plugin_install.go`, `internal/bootstrap/plugin_run.go`
- `InstalledPlugins` → `internal/bootstrap/plugin_run.go`
- `KnownMarketplace` → `internal/bootstrap/plugin_marketplace.go`
- `LayeredSettings` → `internal/bootstrap/acp.go`, `internal/bootstrap/acp_test.go` (test), `internal/bootstrap/bootstrap_test.go` (test), `internal/bootstrap/mcp_connect.go`, `internal/bootstrap/mcp_connect_test.go` (test) + 5 more
- `MCPServerEntry` → `internal/bootstrap/mcp_connect.go`, `internal/bootstrap/mcp_health.go`, `internal/bootstrap/mcp_health_test.go` (test), `internal/bootstrap/mcp_list.go`, `internal/bootstrap/mcp_list_test.go` (test) + 4 more
- `MCPStore` → `internal/bootstrap/mcp_cmd_test.go` (test), `internal/bootstrap/mcp_connect.go`, `internal/bootstrap/mcp_connect_test.go` (test), `internal/bootstrap/mcp_list.go`, `internal/bootstrap/mcp_list_test.go` (test) + 2 more
- `MainModelSettings` → `internal/bootstrap/setup.go`
- `MarketplaceEntry` → `internal/bootstrap/plugin_install.go`
- `MarketplaceManifest` → `internal/bootstrap/plugin_install.go`, `internal/bootstrap/plugin_marketplace.go`
- `OrderedMap` → `internal/bootstrap/mcp_health_test.go` (test), `internal/bootstrap/mcp_list.go`, `internal/bootstrap/mcp_list_test.go` (test), `internal/bootstrap/mcp_render_test.go` (test), `internal/bootstrap/mcp_run.go`
- `PermissionsConfig` → `internal/bootstrap/settings_permissions_test.go` (test)
- `PluginStore` → `internal/bootstrap/plugin_cmd_test.go` (test), `internal/bootstrap/plugin_run.go`
- `SettingsJson` → `internal/bootstrap/acp_test.go` (test), `internal/bootstrap/bootstrap_test.go` (test), `internal/bootstrap/mcp_connect_test.go` (test), `internal/bootstrap/settings_permissions_test.go` (test), `internal/bootstrap/setup.go` + 3 more

## Dependencies

**Imports:** `pkg/types`

**Imported by:** `internal/acpclient`, `internal/bootstrap`, `internal/permissions`, `internal/state`

<!-- AUTO-GENERATED ABOVE — DO NOT EDIT -->
<!-- MANUAL NOTES BELOW — preserved across regeneration -->

## Design Notes

- **2026-11-18 内核搬回（反转 PR #31）**：本包原于 2026-09-23 逐字提取至外部仓 github.com/tunsuy/agtkeel；因该仓停止维护，经人类决策整併回本仓原始路径。来源：agtkeel main 3a77085＋后续提交，零逻辑改动（仅 import 路径恢复＋gofmt）。内核改动自此直接在本仓进行。
- **2026-11-19 `WriteUserMainModel`（/setup）**：以 OrderedMap read-modify-write 全域 `settings.json`，只設定 `provider`/`baseUrl`/`model`/`apiKey`（空金鑰不動），其他鍵與順序保留；原檔無法解析就不寫。檔案含金鑰，rename 前就設成 `0600`。同時修正 `WriteOrderedJSONAtomic` 與 `writeSettingsAtomic` 寫死 `0644` 的問題：既有檔案保留原權限，避免 plugin 設定等其他寫入者把含金鑰的檔案改回 world-readable。`MainModelEnvVars` 是 `applyEnvOverrides` 讀取的主腦環境變數清單，由掃原始碼的測試確保兩邊一致。
