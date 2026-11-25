package memdir

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/tunsuy/glaude/internal/engine"
	"github.com/tunsuy/glaude/pkg/types"
)

const (
	autoDreamQuerySource = "auto_dream"
	// manualDreamQuerySource tags /dream runs apart from the automatic ones.
	manualDreamQuerySource = "manual_dream"
	consolidateLockName    = ".consolidate-lock"
	minHoursBetweenDream   = 24
	autoDreamMaxTurns      = 8
)

// AutoDreamConfig configures background memory consolidation.
type AutoDreamConfig struct {
	Store   *MemoryStore
	Enabled bool
	// Logf receives background failures (nil = discard); see
	// ExtractMemoriesConfig.Logf.
	Logf func(format string, args ...any)
	// OnUsage receives each dream fork's API usage (nil = discard); see
	// ExtractMemoriesConfig.OnUsage.
	OnUsage func(engine.ForkUsage)
}

// DefaultAutoDreamConfig returns the default auto-dream configuration.
func DefaultAutoDreamConfig() AutoDreamConfig {
	return AutoDreamConfig{Enabled: true}
}

// ExecuteAutoDream runs memory consolidation as a StopHook when gates pass.
func ExecuteAutoDream(ctx context.Context, hookCtx *engine.StopHookContext, cfg AutoDreamConfig) {
	if !cfg.Enabled || cfg.Store == nil || hookCtx == nil {
		return
	}
	if hookCtx.IsBareMode || hookCtx.QuerySource != foregroundQuerySource {
		return
	}
	if hookCtx.Engine == nil || hookCtx.CacheParams == nil {
		return
	}
	if !shouldRunAutoDream(cfg.Store.Dir()) {
		return
	}

	lockPath := filepath.Join(cfg.Store.Dir(), consolidateLockName)
	if err := touchConsolidateLock(lockPath); err != nil {
		logf(cfg.Logf, "memdir: auto dream lock: %v", err)
		return
	}

	prompt := buildDreamPrompt(cfg.Store)
	forkedCfg := engine.ForkedAgentConfig{
		PromptMessages: []types.Message{{
			Role: types.RoleUser,
			Content: []types.ContentBlock{{
				Type: types.ContentTypeText,
				Text: strPtr(prompt),
			}},
		}},
		CacheSafeParams: hookCtx.CacheParams,
		QuerySource:     autoDreamQuerySource,
		MaxTurns:        autoDreamMaxTurns,
		AllowedTools:    memoryToolNames,
		OnUsage:         cfg.OnUsage,
	}

	refused, err := runMemoryFork(ctx, hookCtx.Engine, forkedCfg)
	if err != nil {
		logf(cfg.Logf, "memdir: auto dream: %v", err)
	}
	if refused != nil {
		logf(cfg.Logf, "memdir: auto dream: %v", refused)
	}
}

// RunManualDream triggers consolidation immediately (used by /dream).
func RunManualDream(ctx context.Context, eng engine.QueryEngine, cache *engine.CacheSafeParams, store *MemoryStore) error {
	if eng == nil || cache == nil || store == nil {
		return fmt.Errorf("memdir: manual dream: engine, cache params, and store are required")
	}
	lockPath := filepath.Join(store.Dir(), consolidateLockName)
	if err := touchConsolidateLock(lockPath); err != nil {
		return err
	}

	prompt := buildDreamPrompt(store)
	refused, err := runMemoryFork(ctx, eng, engine.ForkedAgentConfig{
		PromptMessages: []types.Message{{
			Role: types.RoleUser,
			Content: []types.ContentBlock{{
				Type: types.ContentTypeText,
				Text: strPtr(prompt),
			}},
		}},
		CacheSafeParams: cache,
		QuerySource:     manualDreamQuerySource,
		MaxTurns:        autoDreamMaxTurns,
		AllowedTools:    memoryToolNames,
	})
	if err != nil {
		return err
	}
	// /dream reports success otherwise, though nothing was written.
	return refused
}

func shouldRunAutoDream(memoryDir string) bool {
	lockPath := filepath.Join(memoryDir, consolidateLockName)
	info, err := os.Stat(lockPath)
	if err != nil {
		return true
	}
	return time.Since(info.ModTime()) >= minHoursBetweenDream*time.Hour
}

func touchConsolidateLock(lockPath string) error {
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o755); err != nil {
		return fmt.Errorf("ensure memory dir: %w", err)
	}
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("create consolidate lock: %w", err)
	}
	if err := f.Close(); err != nil {
		return err
	}
	// Opening an existing file leaves its mtime alone, and the mtime is the
	// "last dream" time shouldRunAutoDream reads: set it explicitly, or once
	// the lock is a day old dream runs after every turn.
	now := time.Now()
	if err := os.Chtimes(lockPath, now, now); err != nil {
		return fmt.Errorf("touch consolidate lock: %w", err)
	}
	return nil
}

func buildDreamPrompt(store *MemoryStore) string {
	idx, _ := store.LoadMemoryIndex()
	if strings.TrimSpace(idx) == "" {
		idx = "(empty)"
	}
	return fmt.Sprintf(`You are a memory consolidation assistant. Review the project's auto-memory index and memory files.

Goals:
1. Merge duplicate or overlapping memories
2. Remove outdated or contradictory entries
3. Keep MEMORY.md index under %d lines

Current MEMORY.md index:
%s

Use MemoryRead to inspect files, MemoryWrite/MemoryDelete to update. Use ONLY these three tools: others may be listed but any call to them is refused. Be conservative — only change when clearly beneficial.`, MaxMemoryIndexLines, idx)
}

// logf reports a background failure through fn, dropping it when fn is nil.
func logf(fn func(string, ...any), format string, args ...any) {
	if fn != nil {
		fn(format, args...)
	}
}
