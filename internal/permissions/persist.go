package permissions

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/tunsuy/glaude/internal/config"
	"github.com/tunsuy/glaude/internal/tools"
	"github.com/tunsuy/glaude/pkg/types"
)

// PersistConfig wires the checker to the on-disk settings that hold its rules.
// ProjectDir is the directory whose .glaude/settings.json receives persisted
// rules; if empty, persistence falls back to in-memory updates only.
type PersistConfig struct {
	ProjectDir string
}

// ruleForTool builds the settings.json permission rule for a tool call.
// For file-oriented tools the rule is what the dialog offers — "always allow
// access to <project>/" — so a path inside projectDir yields
// "Write(<projectDir>/**)"; a path outside it (or with no project dir) yields
// a rule for that one path. Everything else gets a bare tool-name rule
// ("WebFetch"). It used to be "<path>/**" for a file path, which matched
// nothing — not even that file — so the user was asked again every time.
func ruleForTool(toolName string, input tools.Input, registry interface {
	Get(name string) (tools.Tool, bool)
}, projectDir string) string {
	if registry != nil {
		if t, ok := registry.Get(toolName); ok {
			if pt, ok := t.(tools.PathTool); ok {
				if p := pt.GetPath(input); p != "" {
					if projectDir != "" && pathUnder(p, projectDir) {
						return fmt.Sprintf("%s(%s/**)", toolName, filepath.Clean(projectDir))
					}
					return fmt.Sprintf("%s(%s)", toolName, filepath.Clean(p))
				}
			}
		}
	}
	return toolName
}

// pathUnder reports whether path is dir or lies inside it.
func pathUnder(path, dir string) bool {
	path, dir = filepath.Clean(path), filepath.Clean(dir)
	return path == dir || strings.HasPrefix(path, strings.TrimSuffix(dir, string(filepath.Separator))+string(filepath.Separator))
}

// pathRuleMatcher returns the content matcher for a file-oriented tool call
// on path, used when the tool has no matcher of its own (file tools use the
// BaseTool default, nil, so their path rules — allow and deny alike — never
// matched). Pattern forms:
//   - "<dir>/**": path is dir or anything below it
//   - containing *, ? or [: filepath.Match glob
//   - otherwise: that exact path
func pathRuleMatcher(path string) func(string) bool {
	path = filepath.Clean(path)
	return func(pattern string) bool {
		if pattern == "" {
			return false
		}
		if dir, ok := strings.CutSuffix(pattern, "/**"); ok {
			return pathUnder(path, dir)
		}
		if strings.ContainsAny(pattern, "*?[") {
			ok, err := filepath.Match(filepath.Clean(pattern), path)
			return err == nil && ok
		}
		return filepath.Clean(pattern) == path
	}
}

// persistAllowRule writes rule to the project settings' permissions.allow
// (atomic write-back, idempotent) and registers it in the checker's in-memory
// allow rules so subsequent identical calls short-circuit to Allow without
// asking. Returns the rule that was registered.
func (c *checker) persistAllowRule(rule string) string {
	c.mu.Lock()
	if c.permCtx.AlwaysAllowRules == nil {
		c.permCtx.AlwaysAllowRules = make(types.ToolPermissionRulesBySource)
	}
	c.permCtx.AlwaysAllowRules[types.RuleSourceProject] = uniqueStrings(
		c.permCtx.AlwaysAllowRules[types.RuleSourceProject], rule)
	c.mu.Unlock()

	if c.getProjectDir() != "" {
		// Persistence failure must not block the already-approved tool call —
		// the rule still applies for this session via the in-memory update.
		_ = config.AddPermissionRule(c.getProjectDir(), config.PermissionListAllow, rule)
	}
	return rule
}

// SetProjectDir sets the persistence target directory (project root whose
// .glaude/settings.json receives always-allow rules).
func (c *checker) SetProjectDir(dir string) {
	c.mu.Lock()
	c.persist.ProjectDir = dir
	c.mu.Unlock()
}

// getProjectDir returns the current persistence target directory.
func (c *checker) getProjectDir() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.persist.ProjectDir
}

// uniqueStrings appends s to rules if not already present (order-preserving).
func uniqueStrings(rules []string, s string) []string {
	for _, r := range rules {
		if r == s {
			return rules
		}
	}
	return append(rules, s)
}
