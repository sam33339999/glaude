package permissions

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/tunsuy/glaude/internal/config"
	"github.com/tunsuy/glaude/internal/tools"
	"github.com/tunsuy/glaude/pkg/types"
)

// readProjectAllowRules reads permissions.allow from dir/.glaude/settings.json.
func readProjectAllowRules(t *testing.T, dir string) []string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, config.ClaudeDir, config.SettingsFile))
	if err != nil {
		t.Fatalf("read settings: %v", err)
	}
	var doc struct {
		Permissions struct {
			Allow []string `json:"allow"`
		} `json:"permissions"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("parse settings: %v", err)
	}
	return doc.Permissions.Allow
}

// newAlwaysAskChecker builds a checker that asks for WebFetch and persists
// always-allow rules into dir's project settings.
func newAlwaysAskChecker(t *testing.T, dir string) (Checker, chan AskRequest, chan AskResponse) {
	t.Helper()
	askCh := make(chan AskRequest, 1)
	respCh := make(chan AskResponse, 1)
	c := NewChecker(CheckerConfig{
		PermCtx: types.ToolPermissionContext{
			Mode: types.PermissionModeDefault,
			AlwaysAskRules: types.ToolPermissionRulesBySource{
				types.RuleSourceUser: {"WebFetch"},
			},
		},
		AskCh:  askCh,
		RespCh: respCh,
		Persist: &PersistConfig{
			ProjectDir: dir,
		},
	})
	return c, askCh, respCh
}

func TestRequestPermission_AlwaysAllowPersistsRule(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	c, _, respCh := newAlwaysAskChecker(t, dir)

	// Pre-feed the always-allow response (buffered channels).
	respCh <- AskResponse{ID: "u1", Decision: tools.PermissionAllow, AlwaysAllow: true}

	result, err := c.RequestPermission(context.Background(), PermissionRequest{
		ToolName:  "WebFetch",
		ToolUseID: "u1",
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Behavior != tools.PermissionAllow {
		t.Fatalf("expected Allow, got %q", result.Behavior)
	}

	got := readProjectAllowRules(t, dir)
	if len(got) != 1 || got[0] != "WebFetch" {
		t.Errorf("settings allow = %v, want [WebFetch]", got)
	}
}

func TestRequestPermission_AlwaysAllowSubsequentCallAllowedWithoutAsking(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	c, askCh, respCh := newAlwaysAskChecker(t, dir)
	respCh <- AskResponse{ID: "u1", Decision: tools.PermissionAllow, AlwaysAllow: true}
	if _, err := c.RequestPermission(context.Background(), PermissionRequest{
		ToolName:  "WebFetch",
		ToolUseID: "u1",
	}, nil); err != nil {
		t.Fatal(err)
	}

	// Second call: the in-memory rule must short-circuit to Allow before the
	// ask layer — CanUseTool alone should confirm it.
	result, err := c.CanUseTool(context.Background(), "WebFetch", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Behavior != tools.PermissionAllow {
		t.Errorf("expected Allow on repeat call, got %q", result.Behavior)
	}

	// Drain the first call's AskRequest from the buffered ask channel.
	select {
	case <-askCh:
	default:
		t.Fatal("expected an AskRequest from the first call")
	}

	// And the full flow must not emit a new AskRequest.
	done := make(chan tools.PermissionResult, 1)
	go func() {
		r, _ := c.RequestPermission(context.Background(), PermissionRequest{
			ToolName:  "WebFetch",
			ToolUseID: "u2",
		}, nil)
		done <- r
	}()
	select {
	case r := <-done:
		if r.Behavior != tools.PermissionAllow {
			t.Errorf("expected Allow without asking, got %q", r.Behavior)
		}
	case req := <-askCh:
		t.Errorf("unexpected ask request: %+v", req)
	}
}

func TestRequestPermission_AlwaysAllowIdempotent(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	c, _, respCh := newAlwaysAskChecker(t, dir)

	for _, id := range []string{"u1", "u2"} {
		respCh <- AskResponse{ID: id, Decision: tools.PermissionAllow, AlwaysAllow: true}
		if _, err := c.RequestPermission(context.Background(), PermissionRequest{
			ToolName:  "WebFetch",
			ToolUseID: id,
		}, nil); err != nil {
			t.Fatal(err)
		}
	}

	got := readProjectAllowRules(t, dir)
	if len(got) != 1 || got[0] != "WebFetch" {
		t.Errorf("settings allow = %v, want single [WebFetch] entry", got)
	}
}

func TestRequestPermission_PlainAllowDoesNotPersist(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	c, _, respCh := newAlwaysAskChecker(t, dir)

	respCh <- AskResponse{ID: "u1", Decision: tools.PermissionAllow}
	if _, err := c.RequestPermission(context.Background(), PermissionRequest{
		ToolName:  "WebFetch",
		ToolUseID: "u1",
	}, nil); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(filepath.Join(dir, config.ClaudeDir, config.SettingsFile)); !os.IsNotExist(err) {
		t.Error("plain allow should not write settings.json")
	}

	// The next call must still ask (in-memory rules untouched).
	result, err := c.CanUseTool(context.Background(), "WebFetch", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Behavior != tools.PermissionAsk {
		t.Errorf("expected Ask on repeat call after plain allow, got %q", result.Behavior)
	}
}

// stubPathTool is a PathTool stub so ruleForTool produces path rules.
type stubPathTool struct {
	stubPermTool
	path string
}

func (s *stubPathTool) GetPath(_ tools.Input) string { return s.path }

func TestRuleForTool_PathToolProducesPathRule(t *testing.T) {
	reg := newStubRegistry(&stubPathTool{
		stubPermTool: stubPermTool{name: "Read"},
		path:         "/tmp/project/docs/README.md",
	})
	// Inside the project: the rule the dialog promises, the whole project.
	if got := ruleForTool("Read", nil, reg, "/tmp/project"); got != "Read(/tmp/project/**)" {
		t.Errorf("ruleForTool = %q, want Read(/tmp/project/**)", got)
	}
	// Outside the project, or no project: only that path.
	if got := ruleForTool("Read", nil, reg, "/srv/other"); got != "Read(/tmp/project/docs/README.md)" {
		t.Errorf("ruleForTool outside project = %q, want the single file", got)
	}
	if got := ruleForTool("Read", nil, reg, ""); got != "Read(/tmp/project/docs/README.md)" {
		t.Errorf("ruleForTool without project = %q, want the single file", got)
	}
}

// TestPathRules_MatchFileToolCalls: file tools have no matcher of their own,
// so path rules (allow and deny) never matched and "always allow" on a Write
// asked again on the next Write. The checker now matches them by path.
func TestPathRules_MatchFileToolCalls(t *testing.T) {
	file := "/tmp/project/docs/02-architecture.md"
	cases := []struct {
		rule string
		want bool
	}{
		{"Write(/tmp/project/**)", true},
		{"Write(/tmp/project/docs/**)", true},
		{"Write(/tmp/project/docs/02-architecture.md)", true},
		{"Write(/tmp/project/docs/*.md)", true},
		{"Write(/tmp/project/docs/README.md/**)", false}, // a rule the old code wrote
		{"Write(/tmp/projectX/**)", false},
		{"Write(/tmp/project/src/**)", false},
		{"Edit(/tmp/project/**)", false},
	}
	for _, tc := range cases {
		reg := newStubRegistry(&stubPathTool{stubPermTool: stubPermTool{name: "Write"}, path: file})
		c := NewChecker(CheckerConfig{
			PermCtx: types.ToolPermissionContext{
				Mode:             types.PermissionModeDefault,
				AlwaysAllowRules: types.ToolPermissionRulesBySource{types.RuleSourceProject: {tc.rule}},
			},
			Registry: reg,
		})
		res, err := c.CanUseTool(context.Background(), "Write", nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		if got := res.Behavior == tools.PermissionAllow; got != tc.want {
			t.Errorf("rule %s on %s: allowed = %v, want %v", tc.rule, file, got, tc.want)
		}
	}

	// Deny rules on paths take effect too.
	reg := newStubRegistry(&stubPathTool{stubPermTool: stubPermTool{name: "Write"}, path: "/etc/passwd"})
	c := NewChecker(CheckerConfig{
		PermCtx: types.ToolPermissionContext{
			Mode:            types.PermissionModeDefault,
			AlwaysDenyRules: types.ToolPermissionRulesBySource{types.RuleSourceUser: {"Write(/etc/**)"}},
		},
		Registry: reg,
	})
	res, err := c.CanUseTool(context.Background(), "Write", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.Behavior != tools.PermissionDeny {
		t.Errorf("Write(/etc/**) deny rule on /etc/passwd: behavior = %q, want deny", res.Behavior)
	}
}

func TestRuleForTool_NonPathToolProducesBareName(t *testing.T) {
	reg := newStubRegistry(&stubPermTool{name: "WebFetch"})
	if got := ruleForTool("WebFetch", nil, reg, "/tmp/project"); got != "WebFetch" {
		t.Errorf("ruleForTool = %q, want WebFetch", got)
	}
}

func TestRuleForTool_NilRegistry(t *testing.T) {
	if got := ruleForTool("WebFetch", nil, nil, ""); got != "WebFetch" {
		t.Errorf("ruleForTool = %q, want WebFetch", got)
	}
}

func TestSetProjectDir(t *testing.T) {
	dir := t.TempDir()
	c, _, respCh := newAlwaysAskChecker(t, "")

	// Persistence was disabled at construction; enable it via SetProjectDir.
	impl := c.(*checker)
	impl.SetProjectDir(dir)

	respCh <- AskResponse{ID: "u1", Decision: tools.PermissionAllow, AlwaysAllow: true}
	if _, err := c.RequestPermission(context.Background(), PermissionRequest{
		ToolName:  "WebFetch",
		ToolUseID: "u1",
	}, nil); err != nil {
		t.Fatal(err)
	}

	got := readProjectAllowRules(t, dir)
	if len(got) != 1 || got[0] != "WebFetch" {
		t.Errorf("settings allow = %v, want [WebFetch]", got)
	}
}

func TestUniqueStrings(t *testing.T) {
	rules := uniqueStrings(nil, "a")
	rules = uniqueStrings(rules, "b")
	rules = uniqueStrings(rules, "a")
	if len(rules) != 2 || rules[0] != "a" || rules[1] != "b" {
		t.Errorf("uniqueStrings = %v, want [a b]", rules)
	}
}
