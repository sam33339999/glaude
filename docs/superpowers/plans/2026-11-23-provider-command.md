# `/provider` 服務商管理指令 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 在 TUI 用 `/provider` 新增、修改、刪除 `config.toml` 的服務商，金鑰存 `~/.glaude/credentials.json`，存檔後不必重開即可使用。

**Architecture:** `modeltier` 負責檔案（`config.toml` 的 provider 讀寫、`credentials.json`、統一的金鑰解析）；`bootstrap` 的 `tierRouter` 加鎖並可熱更新 client，`providerBackend` 串起寫檔順序與主腦自動套用；`tui` 新增純狀態機 `providerWizard`，模型勾選部分從 `/setup` 精靈抽出共用。

**Tech Stack:** Go、Bubble Tea v2（`charm.land/bubbletea/v2`）、`github.com/BurntSushi/toml`。

**Spec:** `docs/superpowers/specs/2026-11-23-provider-command-design.md`

## Global Constraints

- 跑 go 指令前不需要切 Node；驗證一律 `make all`（vet + test + build）、`make docs-check`、`make debt-check`。
- `modeltier` 不得 import `internal/api` 或 `internal/config`（client 建構留在 `bootstrap`）。
- `commands` 不得 import `bootstrap`；TUI 透過 `commands` 的介面取得後端。
- 金鑰不得出現在 `config.toml`、錯誤字串、警告、log、TUI 畫面（確認頁只顯示末 4 碼）。
- `config.toml` 仍拒絕 `[providers.<id>] api_key`（`config.go:141` 的行為不變）。
- `credentials.json` 權限 0600、原子寫入、保留不認識的鍵；檔案無法解析時讀為空並報錯、寫入拒絕。
- 新 provider id：`^[a-z0-9_-]+$`、不可為 `main`、不可與既有 id 重複；既有手寫 id 不驗證。
- 金鑰解析順序：`api_key_env` → `api_key_source = "stored"`（缺金鑰＝錯誤）→ 無金鑰。
- 已派出／續跑的 sub-agent 沿用派工當下的 client；任何失敗不得 fallback 到主腦。
- 不新增 `TODO(dep)` 或 `not yet implemented`。
- commit message 依專案慣例（`feat(scope): 中文描述`），結尾加 `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`。

## Review Focus

1. **`credentials.json` 有這一筆、但 `config.toml` 沒有 `api_key_source`**（使用者手動刪了標記）→ 應視為無金鑰，不讀 `credentials.json`。測試加在 Task 2 `TestResolveKey`。
2. **修改時只改 Base URL、金鑰留空** → 沿用原金鑰（不論 env 或 stored），`api_key_env` 不被移除。測試加在 Task 5 `TestApplyEditKeepsKeySource`。
3. **主腦正在用的 provider 被修改、但等級綁定改到別處** → 主腦仍以「目前的模型 ID」套用新 client，不跟著等級走。測試加在 Task 5 `TestApplyEditSwitchesMainKeepsModel`。
4. **`/provider` 開著時另一個寫入者改了 `config.toml`**（例如另一個 glaude 視窗）→ `commitLocked` 讀當下檔案再改，不覆蓋對方新增的段落。測試加在 Task 3 `TestUpsertProviderPreservesConcurrentEdits`。
5. **貼上的金鑰前後帶空白或換行** → 存檔前 trim，驗證與寫入用同一個值。測試加在 Task 7 `TestProviderWizardPasteKeyTrimmed`。

---

## File Structure

| 檔案 | 責任 |
|------|------|
| `internal/modeltier/providerid.go`（新） | `ValidProviderID` |
| `internal/modeltier/credentials.go`（新） | `credentials.json` 讀寫、`ResolveKey` |
| `internal/modeltier/config.go`（改） | `Provider.APIKeySource` 欄位與驗證 |
| `internal/modeltier/store.go`（改） | `ProviderStatus.KeySource`、`UpsertProvider`、`RemoveProvider`、`writeAtomic` 加 mode |
| `internal/bootstrap/tiers.go`（改） | `tierRouter` 加鎖、`upsert`/`remove`/`client`、改用 `ResolveKey` |
| `internal/bootstrap/modelswitch.go`（改） | 改走 router 帶鎖方法 |
| `internal/engine/engine.go`（改） | `MainTarget.StripThinking` |
| `internal/commands/provider.go`（新） | `ProviderBackend` 介面與型別、`cmdProvider` |
| `internal/bootstrap/provider.go`（新） | `providerBackend` 實作 |
| `internal/tui/modelchooser.go`（新） | 從 `setupWizard` 抽出的模型過濾＋勾選 |
| `internal/tui/providerwizard.go`、`providerwizard_view.go`（新） | `/provider` 狀態機與畫面 |
| `internal/tui/provider_dialog.go`（新） | tea.Cmd、訊息、開啟對話框 |
| `docs/provider.md`（新）、`docs/config-toml.md`、`docs/setup.md`（改） | 使用說明 |

---

### Task 1: `api_key_source` 欄位與 provider id 驗證

**Files:**
- Create: `internal/modeltier/providerid.go`
- Modify: `internal/modeltier/config.go`（`Provider` struct、`validate`）
- Test: `internal/modeltier/config_test.go`、`internal/modeltier/providerid_test.go`

**Interfaces:**
- Produces: `const KeySourceStored = "stored"`；`Provider.APIKeySource string` (toml `api_key_source,omitempty`)；`func ValidProviderID(id string) error`

- [ ] **Step 1: Write the failing tests**

```go
func TestValidProviderID(t *testing.T) {
	for _, ok := range []string{"zai", "open-router", "qwen_2", "a1"} {
		if err := ValidProviderID(ok); err != nil { t.Errorf("%q: %v", ok, err) }
	}
	for _, bad := range []string{"", "main", "Zai", "z:ai", "z.ai", "z ai", "zai/"} {
		if err := ValidProviderID(bad); err == nil { t.Errorf("%q accepted", bad) }
	}
}

func TestParseAPIKeySource(t *testing.T) {
	// stored → kept
	cfg, w, _ := parse([]byte("[providers.a]\nformat=\"openai\"\nbase_url=\"x\"\napi_key_source=\"stored\"\n"))
	// assert cfg.Providers["a"].APIKeySource == "stored", len(w) == 0
	// other value → warning containing "api_key_source", provider "a" dropped
	// both api_key_env and api_key_source → warning, APIKeySource cleared, APIKeyEnv kept
}
```

- [ ] **Step 2: Run** `go test ./internal/modeltier/ -run 'TestValidProviderID|TestParseAPIKeySource'` — Expected: FAIL（undefined）
- [ ] **Step 3: Implement** — `ValidProviderID` 用 `regexp.MustCompile("^[a-z0-9_-]+$")` 並擋 `MainProviderID`，錯誤訊息說明允許字元；`validate` 在 format 檢查後加兩條規則（值非空且非 `stored` → `warn(... ignoring provider)` 並 drop；同時有 env → warn 並清空 `APIKeySource`）。警告不印值。
- [ ] **Step 4: Run** 同上 — Expected: PASS；`go test ./internal/modeltier/` 全綠
- [ ] **Step 5: Commit** `feat(modeltier): provider 支援 api_key_source 與 id 驗證`

---

### Task 2: `credentials.json` 與統一金鑰解析

**Files:**
- Create: `internal/modeltier/credentials.go`
- Modify: `internal/modeltier/store.go`（`writeAtomic` 加 `mode os.FileMode` 與暫存檔 pattern 參數；`ProviderStatus` 加 `KeySource`，`KeyPresent` 改由 `ResolveKey` 計算）
- Test: `internal/modeltier/credentials_test.go`、`internal/modeltier/store_test.go`

**Interfaces:**
- Consumes: Task 1 `KeySourceStored`
- Produces:
  - `func DefaultCredentialsPath(homeDir string) string` → `~/.glaude/credentials.json`
  - `func ReadCredential(path, id string) (string, error)`；缺檔或缺筆回 `ErrCredentialMissing`（`errors.Is` 可判斷）；無法解析回包含路徑、不含內容的錯誤
  - `func WriteCredential(path, id, key string) error`；`key == ""` 刪除該筆；檔案無法解析時拒絕
  - `const KeySourceEnv = "env"`, `KeySourceNone = "none"`
  - `func ResolveKey(id string, p Provider, credPath string, getenv func(string) string) (key, source string, err error)`
  - `func (s *Store) CredentialsPath() string`（= `filepath.Join(filepath.Dir(s.path), "credentials.json")`）
  - `ProviderStatus.KeySource string`

格式：`{"providers": {"<id>": {"api_key": "..."}}}`，以 `map[string]any` 讀寫保留其他鍵。

- [ ] **Step 1: Write the failing tests**

```go
func TestCredentialRoundTripAndMode(t *testing.T)      // Write → Read 相同；檔案 perm == 0o600；temp 檔不殘留
func TestWriteCredentialPreservesUnknownKeys(t *testing.T) // 先寫 {"other":1,...}，Write 後 "other" 仍在
func TestWriteCredentialEmptyDeletes(t *testing.T)     // Write(id,"") 後 Read → ErrCredentialMissing
func TestCredentialCorruptFile(t *testing.T)           // 檔案內容 "{" → Read 錯誤不含 "{"、Write 回錯且檔案不變
func TestResolveKey(t *testing.T) {
	// env 有值 → (v, "env", nil)；env 沒值 → err 含變數名
	// stored + 有筆 → (k, "stored", nil)；stored + 缺筆/壞檔 → errors.Is(err, ErrCredentialMissing) 或解析錯誤，訊息含 "/provider"
	// 兩者皆無，且 credentials.json 有這個 id → ("", "none", nil)   ← Review Focus 1
}
func TestProvidersKeySource(t *testing.T)              // Store.Providers() 的 KeySource/KeyPresent 三種情況
```

- [ ] **Step 2: Run** `go test ./internal/modeltier/` — Expected: FAIL
- [ ] **Step 3: Implement** — `writeAtomic(path string, content []byte, mode os.FileMode, pattern string)`，`config.toml` 呼叫端傳 `0o644, ".config.toml.*"`，credentials 傳 `0o600, ".credentials.json.*"`；mode 在 rename 前套用。stored 缺金鑰的錯誤訊息：`credentials.json has no key for provider <id>; re-enter it with /provider`。
- [ ] **Step 4: Run** `go test -race ./internal/modeltier/` — Expected: PASS
- [ ] **Step 5: Commit** `feat(modeltier): credentials.json 金鑰儲存與統一解析`

---

### Task 3: Store 寫入 provider

**Files:**
- Modify: `internal/modeltier/store.go`
- Test: `internal/modeltier/store_test.go`

**Interfaces:**
- Consumes: Task 1 `ValidProviderID`
- Produces:
  - `func (s *Store) UpsertProvider(id string, p Provider, isNew bool) error` — 整個替換 `[providers.<id>]`（含 `models`）；`isNew` 時跑 `ValidProviderID` 且 id 不得已存在；非新增時 id 必須已存在
  - `func (s *Store) RemoveProvider(id string) (unbound []string, err error)` — 同一次 `commitLocked` 內移除 provider，並把 `provider == id` 的所有等級設成空表（`setTier(name, map[string]any{})`）；回傳被解除的等級名（排序）
  - `func (s *Store) TiersOn(id string) []string` — 綁在 id 上的等級名（排序）

- [ ] **Step 1: Write the failing tests**

```go
func TestUpsertProviderNewAndEdit(t *testing.T)   // 新增寫入 format/base_url/api_key_source/models；重複新增回錯；編輯不存在的 id 回錯；寫回後 Load 無警告
func TestUpsertProviderRejectsBadID(t *testing.T) // isNew && "Z:ai" → 錯誤，檔案未變
func TestUpsertProviderPreservesConcurrentEdits(t *testing.T) // Load 後外部 append [providers.other]，Upsert a → other 仍在   ← Review Focus 4
func TestRemoveProviderUnbindsTiers(t *testing.T) // lite、自訂 cheap 綁 a → 回 ["cheap","lite"]；兩者 Resolve 為 Unbound（不是 NotTier）；pro 綁 b 不變
func TestUpsertProviderRefusedWhenLoadFailed(t *testing.T) // loadErr != nil → 錯誤
```

- [ ] **Step 2: Run** `go test ./internal/modeltier/ -run 'Upsert|RemoveProvider'` — Expected: FAIL
- [ ] **Step 3: Implement** — 用 `commitLocked` 的 raw map 寫 `providers.<id>`；`Provider` 轉 map 時省略零值欄位，`models` 每筆省略零值（空表保留）。
- [ ] **Step 4: Run** `go test -race ./internal/modeltier/` — Expected: PASS
- [ ] **Step 5: Commit** `feat(modeltier): Store 新增、修改、刪除 provider`

---

### Task 4: router 加鎖與熱更新

**Files:**
- Modify: `internal/bootstrap/tiers.go`、`internal/bootstrap/modelswitch.go`、`internal/engine/engine.go`
- Test: `internal/bootstrap/tiers_test.go`、`internal/bootstrap/modelswitch_test.go`、`internal/engine/engine_test.go`（或現有對應測試檔）

**Interfaces:**
- Consumes: Task 2 `ResolveKey`、`Store.CredentialsPath`
- Produces:
  - `tierRouter` 新欄位 `mu sync.RWMutex`、`factory providerClientFactory`、`getenv func(string) string`
  - `func (r *tierRouter) client(id string) (api.Client, error)` — `main` 回主腦 startup client；`keyErr` 有值回該錯；不存在回 `provider %q has no client`
  - `func (r *tierRouter) upsert(id string) error` — 從 store 取 provider → `ResolveKey` → factory → 替換 `clients[id]`／設定 `keyErr[id]`；回傳的 error 同時記進 `keyErr`
  - `func (r *tierRouter) remove(id string)`
  - `engine.MainTarget.StripThinking bool` — `SetMainTarget` 在 `providerChanged || t.StripThinking` 時 strip；存進 `e.main` 前清成 false
- `loadTierRouter` 改為逐一呼叫 `upsert`；`resolveSubAgentTarget`、`modelSwitcher.fromBinding`、`modelSwitcher.clientFor`、`tierManager.ListProviderModels` 改用 `client()`／`ResolveKey`，不再直接讀 map。`clientFor` 的「目前主腦就是這家」捷徑移除（一律查 router；`main` 查 startup）。「provider added after startup; restart」兩處文案刪除（`upsert` 之後不會再出現此狀態；store 有 provider 而 router 沒有時回 `provider %q has no client`）。

- [ ] **Step 1: Write the failing tests**

```go
func TestRouterUpsertMakesProviderUsable(t *testing.T)   // store 寫入新 provider（stored 金鑰）→ upsert → resolveSubAgentTarget 綁該 provider 的等級拿到 factory 建的 client
func TestRouterUpsertStoredKeyMissing(t *testing.T)      // stored 但無 credentials → resolveSubAgentTarget 回錯、Client 不是主腦
func TestRouterRemove(t *testing.T)                      // remove 後 client() 回錯
func TestRouterConcurrentUpsertAndResolve(t *testing.T)  // 多 goroutine 同時 upsert/remove 與 resolveSubAgentTarget，-race 無警告；先取得的 subAgentTarget.Client 在 upsert 後仍是舊 client
func TestClientForUsesRouterAfterUpsert(t *testing.T)    // 主腦切到 a 後 upsert(a) 換 client → clientFor("a") 回新 client
func TestSetMainTargetStripThinkingFlag(t *testing.T)    // 同 ProviderID、StripThinking=true → 歷史 thinking 被移除；MainTarget().StripThinking == false
```

- [ ] **Step 2: Run** `go test ./internal/bootstrap/ ./internal/engine/ -run 'Router|ClientFor|StripThinking'` — Expected: FAIL
- [ ] **Step 3: Implement** 依 Interfaces。
- [ ] **Step 4: Run** `go test -race ./internal/bootstrap/ ./internal/engine/ ./internal/modeltier/` — Expected: PASS（含既有 tier / modelswitch 測試）
- [ ] **Step 5: Commit** `feat(bootstrap): tier router 加鎖並支援執行中更新 provider`

**段落審查點**：Task 1–4 完成後，開一個 reviewer subagent 審 `git diff dev...HEAD -- internal/modeltier internal/bootstrap internal/engine`（依使用者 CLAUDE.md 的混合做法）。

---

### Task 5: `ProviderBackend` 介面與 bootstrap 實作

**Files:**
- Create: `internal/commands/provider.go`、`internal/bootstrap/provider.go`
- Modify: `internal/commands/builtins.go`（註冊 `cmdProvider()`）、`internal/bootstrap/wire.go`（`Container.Providers commands.ProviderBackend`，兩個 container 建構處都填）
- Test: `internal/commands/provider_test.go`、`internal/bootstrap/provider_test.go`

**Interfaces:**
- Consumes: Task 2–4
- Produces（`commands` 套件）：

```go
type ProviderInput struct {
	ID      string
	Format  string // SetupFormatAnthropic | SetupFormatOpenAI
	BaseURL string
	APIKey  string // "" = 沿用現有金鑰（編輯時）
	NoKey   bool   // 不需要金鑰
}
type ProviderInfo struct {
	ID, Format, BaseURL string
	KeySource  string   // "env" | "stored" | "none"
	KeyEnv     string   // api_key_env（顯示用）
	KeyPresent bool
	Models     []string // models 表，排序
	Tiers      []string // 綁在這家的等級，排序
}
type ProviderPlan struct {
	Input  ProviderInput
	IsNew  bool
	Models []SetupModel       // 勾選的模型（含回報的 ContextWindow）
	Tiers  map[string]string  // lite/pro/max → 模型 ID；不在 map 中＝維持現狀
}
type ProviderReport struct {
	MainSwitched bool     // 主腦已改用新設定
	Unbound      []string // Delete 解除的等級
	Warnings     []string
}
type ProviderBackend interface {
	List() []ProviderInfo
	Warnings() []string
	MainProvider() string // 主腦目前的 ProviderID
	Writable() error      // config.toml 啟動時無法解析 → 非 nil（Store.loadErr）；清單仍可看，新增/修改/刪除拒絕
	ListModels(ctx context.Context, in ProviderInput) ([]SetupModel, error)
	Apply(ctx context.Context, plan ProviderPlan) (ProviderReport, error)
	Delete(id string) (ProviderReport, error)
}
func cmdProvider() *Command // Name "provider"；回 Result{OpenDialog: "provider", Display: DisplayNone}；有參數時回用法錯誤
```

- `bootstrap.providerBackend{store *modeltier.Store; router *tierRouter; eng engine.QueryEngine; app *state.AppStateStore; httpClient *http.Client; getenv func(string) string}`
- `ListModels`：`APIKey == "" && !NoKey && ID 已存在` → 用 `ResolveKey` 取現有金鑰；其餘照輸入；呼叫既有 `listSetupModels`。
- `Apply` 順序（任一步失敗即回錯，錯誤訊息說明哪一步）：
  1. 組 `modeltier.Provider`：format 由 `SetupFormat*` 對應 `Format*`；金鑰：`NoKey` → 清 env 與 source；`APIKey != ""` → 清 env、`APIKeySource = stored`；留空 → 沿用原欄位。`Models`：勾選的模型；原本已有的模型保留其 `MaxTokens`/`ContextWindow`，回報的 `ContextWindow > 0` 覆蓋。
  2. 金鑰：`APIKey != ""` → `WriteCredential`；`NoKey` → `WriteCredential(id, "")`。
  3. `store.UpsertProvider(id, p, plan.IsNew)`。
  4. `plan.Tiers` 每項 `store.Bind(tier, id, model)`。
  5. `router.upsert(id)`；錯誤放進 `Warnings`（例如 stored 金鑰剛寫入卻讀不到），不中斷。
  6. 若 `eng.MainTarget().ProviderID == id`：取 `router.client(id)`，以原 `Model`、依新 provider models 表解析的 `ContextWindow`/`MaxTokens`（解析不到沿用原值）呼叫 `SetMainTarget`，`StripThinking = format 或 base_url 有變`；更新 `AppState.MainLoopModel.Provider` 為新格式；`MainSwitched = true`。
- `Delete`：`MainProvider() == id` → 錯誤 `the main loop is using provider <id>; switch away with /model first`；否則 `RemoveProvider` → `WriteCredential(id, "")`（錯誤進 Warnings）→ `router.remove(id)`。

- [ ] **Step 1: Write the failing tests**（用 fake factory、`t.TempDir()` 當 home、假的 `getenv`；`ListModels` 用 `httptest.Server`）

```go
func TestCmdProviderOpensDialog(t *testing.T)               // "" → OpenDialog "provider"；"add x" → DisplayError
func TestApplyNewStoresKeyAndIsUsable(t *testing.T)         // credentials 有金鑰、config.toml 有 api_key_source=stored 且無金鑰字串；resolveSubAgentTarget 綁新等級成功
func TestApplyNewBindsTiers(t *testing.T)                   // Tiers{"lite":"m1"} → lite 綁 id:m1；pro 未動
func TestApplyEditKeepsKeySource(t *testing.T)              // 原本 api_key_env=X，只改 BaseURL、APIKey="" → 仍 api_key_env=X，credentials 未建立   ← Review Focus 2
func TestApplyEditNoKeyClearsCredential(t *testing.T)       // NoKey → credentials 無此筆、config 無 env/source
func TestApplyEditKeepsModelLimits(t *testing.T)            // 原 models.m1.max_tokens=4096，再勾 m1 → 保留 4096
func TestApplyEditSwitchesMainKeepsModel(t *testing.T)      // 主腦在 a:m1，修改 a 且把 lite 綁到 m2 → MainTarget().Model == "m1"、Client 是新 client、MainSwitched   ← Review Focus 3
func TestApplyEditBaseURLStripsThinking(t *testing.T)       // 改 base_url → SetMainTarget 收到 StripThinking=true；只改金鑰 → false
func TestDeleteRefusedForMainProvider(t *testing.T)
func TestDeleteUnbindsAndRemovesKey(t *testing.T)           // Unbound 列出等級、credentials 無此筆、client() 回錯
func TestListModelsReusesStoredKey(t *testing.T)            // 編輯時 APIKey="" → httptest 收到 Authorization 為已存金鑰
func TestApplyErrorsNeverContainKey(t *testing.T)           // 各錯誤路徑的 err.Error() 不含金鑰字串
```

- [ ] **Step 2: Run** `go test ./internal/commands/ ./internal/bootstrap/ -run 'Provider|Apply|Delete|ListModelsReuses'` — Expected: FAIL
- [ ] **Step 3: Implement** 依 Interfaces。
- [ ] **Step 4: Run** `go test -race ./internal/commands/ ./internal/bootstrap/` — Expected: PASS
- [ ] **Step 5: Commit** `feat(bootstrap): /provider 後端（寫檔順序、熱更新、主腦自動套用）`

**段落審查點**：後端（Task 1–5）完成，開 reviewer subagent 審該段 diff。

---

### Task 6: 從 `/setup` 抽出 `modelChooser`

**Files:**
- Create: `internal/tui/modelchooser.go`
- Modify: `internal/tui/setupwizard.go`、`internal/tui/setupwizard_view.go`
- Test: `internal/tui/modelchooser_test.go`；既有 `setupwizard_test.go` 必須不改斷言而通過

**Interfaces:**
- Produces:

```go
type modelChooser struct {
	models     []commands.SetupModel
	filter     []rune
	cursor     int
	checked    map[string]bool
	checkOrder []string
}
type chooserResult int // chooserNone | chooserBack | chooserDone
func newModelChooser(models []commands.SetupModel, prechecked []string) *modelChooser
func (c *modelChooser) filtered() []commands.SetupModel
func (c *modelChooser) toggle(id string)
func (c *modelChooser) handleKey(key tea.KeyPressMsg) chooserResult // ↑↓ 空白 Backspace 打字 Esc(back) Enter(done；沒勾時勾游標所在)
func (c *modelChooser) paste(text string)
func (c *modelChooser) view(width int) string                       // 從 setupwizard_view.go 搬出的清單繪製
```

`setupWizard` 的 `models/filter/cursor/checked/checkOrder` 改為一個 `chooser *modelChooser`；手打模型（`stepManualModels`）建立 `newModelChooser(models, ids)`。

- [ ] **Step 1: Write the failing test** `TestModelChooserPrechecked`（預勾的 ID 進 `checkOrder`、順序依輸入）、`TestModelChooserEnterWithoutCheck`（Enter 勾游標所在並回 `chooserDone`；空清單 Enter 回 `chooserNone`）
- [ ] **Step 2: Run** `go test ./internal/tui/ -run ModelChooser` — Expected: FAIL
- [ ] **Step 3: Implement**（純搬移＋預勾參數，不改 `/setup` 行為）
- [ ] **Step 4: Run** `go test ./internal/tui/` — Expected: PASS（`setupwizard_test.go` 全綠）
- [ ] **Step 5: Commit** `refactor(tui): 抽出 /setup 的模型勾選元件`

---

### Task 7: `providerWizard` 狀態機與畫面

**Files:**
- Create: `internal/tui/providerwizard.go`、`internal/tui/providerwizard_view.go`
- Test: `internal/tui/providerwizard_test.go`

**Interfaces:**
- Consumes: Task 5 型別、Task 6 `modelChooser`
- Produces:

```go
type providerStep int // pvList, pvConfirmDelete, pvID, pvFormat, pvEndpoint, pvValidating, pvModels, pvManualModels, pvTiers, pvConfirm, pvWriting, pvDone
type providerAction int // pvActNone, pvActClose, pvActValidate, pvActApply, pvActDelete
func newProviderWizard(list []commands.ProviderInfo, warnings []string, mainProvider string, writable error) *providerWizard
func (w *providerWizard) handleKey(key tea.KeyPressMsg, busy bool) providerAction // busy = 查詢進行中
func (w *providerWizard) handlePaste(text string)
func (w *providerWizard) input() commands.ProviderInput   // 欄位一律 TrimSpace
func (w *providerWizard) plan() commands.ProviderPlan
func (w *providerWizard) deleteID() string
func (w *providerWizard) onModels(models []commands.SetupModel, err error)
func (w *providerWizard) onApplied(r commands.ProviderReport, err error)
func (w *providerWizard) onDeleted(r commands.ProviderReport, err error)
func (w *providerWizard) view(width int) string
```

行為（依 spec §5.1–5.4）：
- `pvList`：列 `main`（不可選）、各 provider、`+ Add provider…`；`enter` 進編輯（`pvFormat`，帶入現值）或新增（`pvID`）；`d` 在 provider 上進 `pvConfirmDelete`；主腦正用的那家按 `d` → `errMsg` 提示先 `/model` 切走、不進確認；`esc` → `pvActClose`。
- `Writable() != nil` 時清單上方顯示原因，`enter`（新增/編輯）與 `d` 只設 `errMsg`、不進下一步；啟動警告（`Warnings()`）顯示在清單上方。
- `pvID`：`ValidProviderID` 不過或 id 已存在 → `errMsg`。
- `pvEndpoint`：欄位 Base URL、API key（遮蔽）、`No key needed` 切換（focus 在第三列時空白切換）；新增時未勾 NoKey 且 key 空 → `API key is required.`；編輯時 key 空顯示 `leave empty to keep using <stored key|$ENV>`。
- `pvValidating`／`pvModels`／`pvManualModels`：同 `/setup`；編輯時 `newModelChooser(models, info.Models)` 預勾。
- `pvTiers`：lite/pro/max，←/→ 在「keep (<現值>)」與勾選模型間循環，預設 keep。
- `pvConfirm`：列出 id、格式、Base URL、金鑰（末 4 碼／env 名／none）、模型、等級變更；編輯的是主腦正用的那家時加一行 `The main loop will switch to the new settings; the prompt cache is invalidated and the next request resends the whole conversation.`；格式或 Base URL 改了且有等級綁在這家時加一行提醒該等級的模型可能不存在。`enter` 時若 `busy && 編輯的是主腦那家` → `errMsg = "Cannot change the provider the main loop is using while a query is running."`，不送出。
- `pvDone`：新增成功顯示 `Ready. Try /model <id>:<first model>`；`MainSwitched` 時加一行說明；任意鍵 `pvActClose`。
- Esc 一律回上一步；`pvID` 與編輯流程的 `pvFormat` Esc 回 `pvList`。

- [ ] **Step 1: Write the failing tests**

```go
func TestProviderWizardAddFlow(t *testing.T)            // id→format→endpoint→validate→models→tiers→confirm；plan() 的 IsNew、Input、Models、Tiers 正確
func TestProviderWizardRejectsBadAndDuplicateID(t *testing.T)
func TestProviderWizardEditPrefills(t *testing.T)        // 帶入 format/base URL、預勾 models、key 空可送出驗證
func TestProviderWizardNoKeyToggle(t *testing.T)         // NoKey 時 key 空可送出；input().NoKey
func TestProviderWizardDeleteMainRefused(t *testing.T)
func TestProviderWizardBusyBlocksMainEdit(t *testing.T)  // busy=true 編輯主腦那家 → 不回 pvActApply
func TestProviderWizardPasteKeyTrimmed(t *testing.T)     // 貼上 "  sk-abc\n" → input().APIKey == "sk-abc"   ← Review Focus 5
func TestProviderWizardConfirmMasksKey(t *testing.T)     // view() 不含完整金鑰、含末 4 碼
func TestProviderWizardEscNavigation(t *testing.T)
```

- [ ] **Step 2: Run** `go test ./internal/tui/ -run ProviderWizard` — Expected: FAIL
- [ ] **Step 3: Implement**（純狀態機，不做 I/O；畫面風格沿用 `setupwizard_view.go` 與 `tierpicker.go`）
- [ ] **Step 4: Run** `go test ./internal/tui/ -run ProviderWizard` — Expected: PASS
- [ ] **Step 5: Commit** `feat(tui): /provider 精靈狀態機與畫面`

---

### Task 8: TUI 接線

**Files:**
- Create: `internal/tui/provider_dialog.go`
- Modify: `internal/tui/tierpicker.go`（provider 列改顯示 `ProviderStatus.KeySource`：`key: stored` / `key: env X (missing)` / `key: none`，spec §5.5）
- Modify: `internal/tui/model.go`（`dialogProvider`、`providerBackend`、`providerWizard`、`providerSeq` 欄位；`New` 加 `providers commands.ProviderBackend` 參數放在 `setup` 之後）、`internal/tui/update.go`（`OpenDialog "provider"`、paste 路由、三種結果訊息）、`internal/tui/keys.go`、`internal/tui/view.go`、`internal/bootstrap/root.go`、`internal/tui/tui_test.go`（`New` 呼叫處）
- Test: `internal/tui/provider_dialog_test.go`

**Interfaces:**
- Consumes: Task 5、Task 7
- Produces: `providerModelsMsg{gen int; models []commands.SetupModel; err error}`、`providerAppliedMsg{gen int; report commands.ProviderReport; err error}`、`providerDeletedMsg{gen int; report commands.ProviderReport; err error}`；`func (m AppModel) openProviderDialog() AppModel`；`func (m AppModel) handleProviderKey(key tea.KeyPressMsg) (tea.Model, tea.Cmd)`

做法照抄 `setup_dialog.go` 的 gen/seq 模式（逾時 15s / 30s）。Apply 或 Delete 成功後：若 `report.MainSwitched`，更新 `m.statusBar`（provider 格式）；呼叫 `m.tiers`/`m.modelSwitcher` 不需要額外刷新（它們每次開啟都即時讀）。`busy` 傳 `m.isLoading`。

- [ ] **Step 1: Write the failing tests**（fake `ProviderBackend`）

```go
func TestProviderDialogOpensFromCommand(t *testing.T)    // 執行 /provider → activeDialog == dialogProvider
func TestProviderDialogUnavailable(t *testing.T)         // backend nil → 系統錯誤訊息
func TestProviderDialogDropsStaleResults(t *testing.T)   // 關閉後才到的 providerModelsMsg 被忽略
func TestProviderDialogApplyCallsBackend(t *testing.T)   // 走完新增 → fake 收到 plan；完成畫面出現
func TestTierPickerShowsKeySource(t *testing.T)          // stored / env missing / none 三種顯示
```

- [ ] **Step 2: Run** `go test ./internal/tui/ -run ProviderDialog` — Expected: FAIL
- [ ] **Step 3: Implement**
- [ ] **Step 4: Run** `make all` — Expected: PASS
- [ ] **Step 5: 手動驗證**：`make build`，以 `HOME=$(mktemp -d)` 啟動 `bin/glaude`，`/provider` 新增一家（可用 `httptest` 以外的本機假端點，或 OpenRouter 公開模型清單＋假金鑰）→ `/tier` 列表看到該 provider 與 `key: stored` → `/model <id>:<model>` 可選。確認 `~/.glaude/credentials.json` 權限為 `-rw-------`、`config.toml` 不含金鑰。
- [ ] **Step 6: Commit** `feat(tui): 接上 /provider 對話框`

---

### Task 9: 文件與收尾

**Files:**
- Create: `docs/provider.md`
- Modify: `docs/config-toml.md`、`docs/setup.md`、`docs/config.example.toml`（註解提到 `/provider` 與 `api_key_source`）、`docs/superpowers/specs/2026-11-23-provider-command-design.md`（狀態改為已實作）、各套件 `CONTEXT.md`（`make docs` 產生）

- [ ] **Step 1:** 寫 `docs/provider.md`（風格同 `docs/setup.md`：流程、寫入哪些東西、金鑰解析順序、主腦自動套用、已知限制）；`config-toml.md` 加 `api_key_source` 欄位列、金鑰解析順序、「手動編輯要重啟，用 `/provider` 立即生效」；`setup.md` 的「不支援的部分」改指向 `/provider`。
- [ ] **Step 2: Run** `go test ./internal/modeltier/ -run TestExampleConfigParses` — Expected: PASS
- [ ] **Step 3: Run** `make docs && make all && make docs-check && make debt-check` — Expected: 全部成功，debt 數字不升
- [ ] **Step 4: Commit** `docs: /provider 使用說明與 config.toml 金鑰來源`

**整個分支審查**：合併前開 reviewer subagent 審 `git diff dev...HEAD`。
