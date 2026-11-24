package permissions

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/tunsuy/glaude/internal/tools"
	"github.com/tunsuy/glaude/pkg/types"
)

func newHookChecker(mode types.PermissionMode, allow, deny []string, withUI bool) (Checker, chan AskRequest, chan AskResponse) {
	var askCh chan AskRequest
	var respCh chan AskResponse
	if withUI {
		askCh = make(chan AskRequest, 1)
		respCh = make(chan AskResponse, 1)
	}
	c := NewChecker(CheckerConfig{
		PermCtx: types.ToolPermissionContext{
			Mode:             mode,
			AlwaysAllowRules: types.ToolPermissionRulesBySource{types.RuleSourceUser: allow},
			AlwaysDenyRules:  types.ToolPermissionRulesBySource{types.RuleSourceUser: deny},
		},
		AskCh:  askCh,
		RespCh: respCh,
	})
	return c, askCh, respCh
}

// A hook "ask" prompts even when an allow rule (or bypass mode) would allow.
func TestForceAsk_PromptsDespiteAllow(t *testing.T) {
	for _, mode := range []types.PermissionMode{types.PermissionModeDefault, types.PermissionModeBypassPermissions} {
		c, askCh, respCh := newHookChecker(mode, []string{"Bash"}, nil, true)
		go func() {
			req := <-askCh
			if !strings.Contains(req.Message, "hook wants you to look") {
				t.Errorf("dialog message = %q", req.Message)
			}
			respCh <- AskResponse{ID: req.ID, Decision: tools.PermissionDeny}
		}()
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		r, err := c.RequestPermission(ctx, PermissionRequest{ToolName: "Bash", ToolUseID: "t1", ForceAsk: true, HookReason: "hook wants you to look"}, nil)
		cancel()
		if err != nil || r.Behavior != tools.PermissionDeny || r.Reason != "user denied" {
			t.Errorf("mode %s: result = %+v err=%v, want the user's denial", mode, r, err)
		}
	}
}

// A deny rule wins over a hook "ask": no dialog.
func TestForceAsk_DenyRuleWins(t *testing.T) {
	c, askCh, _ := newHookChecker(types.PermissionModeDefault, nil, []string{"Bash"}, true)
	r, _ := c.RequestPermission(context.Background(), PermissionRequest{ToolName: "Bash", ToolUseID: "t1", ForceAsk: true}, nil)
	if r.Behavior != tools.PermissionDeny {
		t.Fatalf("result = %+v, want deny", r)
	}
	select {
	case <-askCh:
		t.Fatal("deny rule must not show a dialog")
	default:
	}
}

func TestForceAsk_NoUIDenies(t *testing.T) {
	c, _, _ := newHookChecker(types.PermissionModeDefault, []string{"Bash"}, nil, false)
	r, _ := c.RequestPermission(context.Background(), PermissionRequest{ToolName: "Bash", ToolUseID: "t1", ForceAsk: true}, nil)
	if r.Behavior != tools.PermissionDeny {
		t.Fatalf("result = %+v, want deny without a UI", r)
	}
}

func TestCanUseToolHookAllowed(t *testing.T) {
	reg := newStubRegistry(&stubPermTool{name: "Write"})
	mk := func(mode types.PermissionMode, allow, ask, deny []string) HookAwareChecker {
		return NewChecker(CheckerConfig{
			PermCtx: types.ToolPermissionContext{
				Mode:             mode,
				AlwaysAllowRules: types.ToolPermissionRulesBySource{types.RuleSourceUser: allow},
				AlwaysAskRules:   types.ToolPermissionRulesBySource{types.RuleSourceUser: ask},
				AlwaysDenyRules:  types.ToolPermissionRulesBySource{types.RuleSourceUser: deny},
			},
			Registry: reg,
		}).(HookAwareChecker)
	}
	ctx := context.Background()
	cases := []struct {
		name string
		c    HookAwareChecker
		want tools.PermissionBehavior
	}{
		{"default prompt skipped", mk(types.PermissionModeDefault, nil, nil, nil), tools.PermissionAllow},
		{"acceptEdits prompt skipped", mk(types.PermissionModeAcceptEdits, nil, nil, nil), tools.PermissionAllow},
		{"deny rule wins", mk(types.PermissionModeDefault, nil, nil, []string{"Write"}), tools.PermissionDeny},
		{"ask rule still asks", mk(types.PermissionModeDefault, nil, []string{"Write"}, nil), tools.PermissionAsk},
		{"plan mode blocks writes", mk(types.PermissionModePlan, nil, nil, nil), tools.PermissionDeny},
	}
	for _, tc := range cases {
		r, err := tc.c.CanUseToolHookAllowed(ctx, "Write", nil, nil)
		if err != nil || r.Behavior != tc.want {
			t.Errorf("%s: got %+v err=%v, want %s", tc.name, r, err, tc.want)
		}
	}
	// Without the hook, the default mode still asks.
	if r, _ := mk(types.PermissionModeDefault, nil, nil, nil).(Checker).CanUseTool(ctx, "Write", nil, nil); r.Behavior != tools.PermissionAsk {
		t.Errorf("CanUseTool without hook = %+v, want ask", r)
	}
}

func TestMatchRule(t *testing.T) {
	c := NewChecker(CheckerConfig{}).(HookAwareChecker)
	if !c.MatchRule("Write", "Write", nil) || c.MatchRule("Write", "Read", nil) {
		t.Error("tool-name rule mismatch")
	}
}
