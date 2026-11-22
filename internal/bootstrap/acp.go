package bootstrap

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/tunsuy/glaude/internal/acpclient"
	"github.com/tunsuy/glaude/internal/agenttype"
	"github.com/tunsuy/glaude/internal/commands"
	"github.com/tunsuy/glaude/internal/config"
	"github.com/tunsuy/glaude/internal/coordinator"
	"github.com/tunsuy/glaude/internal/permissions"
	"github.com/tunsuy/glaude/internal/state"
	"github.com/tunsuy/glaude/internal/tools"
	"github.com/tunsuy/glaude/internal/tools/agent"
)

// resolveACPSpecs merges settings onto the built-in ACP agent presets and
// drops names that would shadow a glaude agent type (worker, explore, ...).
func resolveACPSpecs(settings *config.LayeredSettings, profiles *agenttype.Registry, logf func(string, ...any)) map[string]acpclient.AgentSpec {
	var overrides map[string]config.ACPAgentConfig
	if settings != nil && settings.Merged != nil {
		overrides = settings.Merged.ACPAgents
	}
	specs := acpclient.Resolve(overrides)
	for name := range specs {
		if profiles != nil {
			if _, taken := profiles.Resolve(name); taken {
				if logf != nil {
					logf("acp: agent %q skipped: name is a glaude agent type", name)
				}
				delete(specs, name)
			}
		}
	}
	return specs
}

// buildACPPermissionFunc routes external agents' permission requests through
// glaude's permission checker: the request is shaped like the matching glaude
// tool call (Bash/Edit/Write/Read/...) so allow/deny rules, protected paths,
// Bash security checks and the permission mode all apply. Interactive mode
// asks the user for unmatched calls; headless mode (no one to ask) denies.
// Asks are serialized so concurrent agents never interleave dialogs.
func buildACPPermissionFunc(checker permissions.Checker, store *state.AppStateStore, headless bool) acpclient.PermissionFunc {
	var askMu sync.Mutex
	return func(ctx context.Context, q acpclient.PermissionQuery) (bool, error) {
		tctx := &tools.UseContext{Ctx: ctx, WorkingDir: store.GetState().WorkingDir}
		if headless {
			res, err := checker.CanUseTool(ctx, q.ToolName, tools.Input(q.Input), tctx)
			if err != nil {
				return false, err
			}
			return res.Behavior == tools.PermissionAllow, nil
		}
		askMu.Lock()
		defer askMu.Unlock()
		res, err := checker.RequestPermission(ctx, permissions.PermissionRequest{
			ToolName:  q.ToolName,
			ToolUseID: q.ToolCallID,
			Input:     tools.Input(q.Input),
			Note:      fmt.Sprintf("Requested by external agent %q: %s", q.Agent, q.Title),
		}, tctx)
		if err != nil {
			return false, err
		}
		return res.Behavior == tools.PermissionAllow, nil
	}
}

// buildACPManager wires the external-agent manager and advertises the agents
// in the Agent tool description. Returns nil when no agent is configured.
func buildACPManager(opts ContainerOptions, settings *config.LayeredSettings, profiles *agenttype.Registry, checker permissions.Checker, store *state.AppStateStore) *acpclient.Manager {
	logf := engineDebugLogf(opts)
	specs := resolveACPSpecs(settings, profiles, logf)
	infos := make([]agent.ExternalAgentInfo, 0, len(specs))
	for _, name := range acpclient.SortedNames(specs) {
		infos = append(infos, agent.ExternalAgentInfo{Name: name, Description: specs[name].Description})
	}
	agent.SetExternalAgents(infos)
	if len(specs) == 0 {
		return nil
	}
	return acpclient.NewManager(specs, acpclient.Options{
		Permission:    buildACPPermissionFunc(checker, store, opts.Headless),
		Logf:          logf,
		ClientVersion: appVersion,
	})
}

// withACPDispatch routes sub-agents whose type names an external ACP agent to
// that agent's long-lived session; every other type runs on base.
//
// Semantics: the first run of an agent ID sends req.Prompt; follow-ups
// delivered through SendMessage (inbox) are sent as further turns of the same
// ACP session — which is shared by every delegation to that agent in this
// glaude process, so the agent keeps what it already read.
func withACPDispatch(mgr *acpclient.Manager, store *state.AppStateStore, onProgress coordinator.OnProgressFn, base coordinator.RunAgentFn) coordinator.RunAgentFn {
	if mgr == nil {
		return base
	}
	var started sync.Map // AgentID → struct{}: initial prompt already sent
	return func(ctx context.Context, agentID coordinator.AgentID, req coordinator.SpawnRequest, inboxCh <-chan string) (string, error) {
		if !mgr.Has(req.SubagentType) {
			return base(ctx, agentID, req, inboxCh)
		}
		emit := func(activity, detail string) {
			if onProgress != nil {
				onProgress(coordinator.ProgressEvent{
					AgentID:     agentID,
					Description: req.Description,
					AgentName:   req.Name,
					AgentType:   req.SubagentType,
					Activity:    activity,
					Detail:      detail,
				})
			}
		}

		var turns []string
		if _, resumed := started.LoadOrStore(agentID, struct{}{}); !resumed {
			turns = append(turns, req.Prompt)
		}
		turns = append(turns, drainInbox(inboxCh)...)

		var last string
		var notes []string
		for len(turns) > 0 {
			text := strings.Join(turns, "\n\n")
			turns = nil
			cwd := store.GetState().WorkingDir
			res, err := mgr.PromptWithModel(ctx, req.SubagentType, cwd, req.Model, text, acpProgress(emit))
			if res.Restarted {
				notes = append(notes, fmt.Sprintf("[note: the %s process had exited; this turn ran in a new session without earlier context]", req.SubagentType))
			}
			if err != nil {
				return "", fmt.Errorf("%s: %w", req.SubagentType, err)
			}
			last = res.Text
			if res.StopReason != "" && res.StopReason != "end_turn" {
				notes = append(notes, fmt.Sprintf("[stop reason: %s]", res.StopReason))
			}
			turns = append(turns, drainInbox(inboxCh)...)
		}
		emit("Completed", "")
		if len(notes) > 0 {
			last = strings.TrimSpace(last + "\n\n" + strings.Join(notes, "\n"))
		}
		return last, nil
	}
}

func drainInbox(inboxCh <-chan string) []string {
	var out []string
	for {
		select {
		case m, ok := <-inboxCh:
			if !ok {
				return out
			}
			out = append(out, m)
		default:
			return out
		}
	}
}

// acpProgress converts ACP stream events into coordinator progress updates.
func acpProgress(emit func(activity, detail string)) func(acpclient.Event) {
	var line strings.Builder
	return func(e acpclient.Event) {
		switch e.Kind {
		case acpclient.EventText:
			line.WriteString(e.Text)
			s := line.String()
			if i := strings.LastIndexByte(strings.TrimRight(s, "\n"), '\n'); i >= 0 {
				s = s[i+1:]
			}
			emit("Streaming", clipRunes(strings.TrimSpace(s), 120))
		case acpclient.EventThought:
			emit("Thinking", "")
		case acpclient.EventToolCall:
			line.Reset()
			emit("Running "+clipRunes(e.Title, 60), "")
		case acpclient.EventToolUpdate:
			if e.Status == "completed" || e.Status == "failed" {
				emit(clipRunes(e.Title, 60)+" "+e.Status, "")
			}
		case acpclient.EventPlan:
			emit("Planning", clipRunes(strings.ReplaceAll(e.Text, "\n", " · "), 120))
		case acpclient.EventNotice:
			emit(clipRunes(e.Text, 80), "")
		}
	}
}

func clipRunes(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return string(r[:n]) + "…"
}

// externalAgentCommands registers /<agent> slash commands for the configured
// external ACP agents. The command spawns a background delegation through the
// coordinator, so it shares the Agent tool's session, progress panel and
// completion notification.
func externalAgentCommands(c *AppContainer) func(*commands.Registry) {
	if c == nil || c.ACPAgents == nil || c.AgentCoordinator == nil {
		return nil
	}
	mgr := c.ACPAgents
	store := c.AppStateStore
	names := mgr.Names()
	cmds := make([]commands.ExternalAgentCommand, 0, len(names))
	for _, n := range names {
		name := n
		spec, _ := mgr.Spec(name)
		cmds = append(cmds, commands.ExternalAgentCommand{
			Name:        name,
			Description: spec.Description,
			ListModels: func(cwd string) (string, error) {
				if cwd == "" && store != nil {
					cwd = store.GetState().WorkingDir
				}
				cat, err := mgr.Models(context.Background(), name, cwd)
				if err != nil {
					return "", err
				}
				return formatACPModelCatalog(name, cat), nil
			},
		})
	}
	coord := c.AgentCoordinator
	spawn := func(name, model, task string) (string, error) {
		return coord.SpawnAgent(context.Background(), tools.AgentSpawnRequest{
			Description: "/" + name + ": " + clipRunes(task, 80),
			Prompt:      task,
			AgentType:   name,
			Model:       model,
			Background:  true,
		})
	}
	return func(r *commands.Registry) { commands.RegisterExternalAgents(r, cmds, spawn) }
}

func formatACPModelCatalog(name string, cat acpclient.ModelCatalog) string {
	if len(cat.Models) == 0 && cat.Current == "" {
		return fmt.Sprintf("%s did not report selectable models.", name)
	}
	var b strings.Builder
	cur := cat.Current
	if cur == "" {
		cur = "(default)"
	}
	if cat.ThoughtLevel != "" {
		fmt.Fprintf(&b, "%s current model: %s (thinking: %s)\n", name, cur, cat.ThoughtLevel)
	} else {
		fmt.Fprintf(&b, "%s current model: %s\n", name, cur)
	}
	if len(cat.Models) > 0 {
		b.WriteString("Available models:\n")
		for _, m := range cat.Models {
			mark := "  "
			if m.ID == cat.Current {
				mark = "* "
			}
			if m.Name != "" && m.Name != m.ID {
				fmt.Fprintf(&b, "%s%-32s %s\n", mark, m.ID, m.Name)
			} else {
				fmt.Fprintf(&b, "%s%s\n", mark, m.ID)
			}
		}
	}
	if len(cat.ThoughtLevels) > 0 {
		fmt.Fprintf(&b, "Thinking levels (append -<level> to model ID, e.g. %s-%s): %s\n",
			cat.Current, cat.ThoughtLevels[0], strings.Join(cat.ThoughtLevels, ", "))
	}
	fmt.Fprintf(&b, "Use: /%s -m <model> <task>  or set acpAgents.%s.model in settings.json", name, name)
	return b.String()
}
