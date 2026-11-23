package interact

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/tunsuy/glaude/internal/state"
	"github.com/tunsuy/glaude/internal/tools"
	"github.com/tunsuy/glaude/pkg/types"
	"github.com/tunsuy/glaude/pkg/utils/ids"
)

// ── TodoWrite ─────────────────────────────────────────────────────────────────

// TodoWriteInput is the input schema for TodoWrite.
type TodoWriteInput struct {
	// Todos is the complete list of TODO items to write (replaces existing list).
	Todos []types.TodoItem `json:"todos"`
}

// NewTodoWriteTool constructs the TodoWrite tool backed by the shared app
// state store (BF-07). The list lives in AppState.Todos, rendered by the TUI
// panel; bootstrap registers this after the store is built. A nil store
// makes every call report an honest error.
//
// Oracle parity (TodoWriteTool.ts): no permission checks beyond the standard
// pipeline; when every item is completed the list is cleared; the result is
// the fixed confirmation text. Divergences (documented in tools.md): single
// main-session list (no per-agent keying), no disk persistence, no
// feature-flagged verification nudge.
func NewTodoWriteTool(store *state.AppStateStore) tools.Tool {
	return &todoWriteTool{store: store}
}

type todoWriteTool struct {
	tools.BaseTool
	store *state.AppStateStore
}

func (t *todoWriteTool) Name() string { return "TodoWrite" }

func (t *todoWriteTool) Description(_ tools.Input, _ tools.PermissionContext) string {
	return `Writes the complete TODO list for the current session, replacing the existing list.

Usage notes:
- Provide the full list on every call; partial updates are not supported
- Status must be one of: pending, in_progress, completed`
}

func (t *todoWriteTool) InputSchema() tools.InputSchema {
	return tools.NewInputSchema(
		map[string]json.RawMessage{
			"todos": tools.PropSchema(map[string]any{
				"type":        "array",
				"description": "The complete list of TODO items",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"id":       map[string]any{"type": "string"},
						"content":  map[string]any{"type": "string"},
						"status":   map[string]any{"type": "string", "enum": []string{"pending", "in_progress", "completed"}},
						"priority": map[string]any{"type": "integer"},
					},
					"required": []string{"id", "content", "status"},
				},
			}),
		},
		[]string{"todos"},
	)
}

func (t *todoWriteTool) IsConcurrencySafe(_ tools.Input) bool { return false }
func (t *todoWriteTool) IsReadOnly(_ tools.Input) bool        { return false }
func (t *todoWriteTool) UserFacingName(_ tools.Input) string  { return "TodoWrite" }

// todoWriteResultText is the oracle's fixed confirmation (TodoWriteTool.ts).
const todoWriteResultText = "Todos have been modified successfully. Ensure that you continue to use the todo list to track your progress. Please proceed with the current tasks if applicable"

func (t *todoWriteTool) Call(input tools.Input, uctx *tools.UseContext, _ tools.OnProgressFn) (*tools.Result, error) {
	if t.store == nil {
		return &tools.Result{IsError: true, Content: "TodoWrite: no app state store configured"}, nil
	}
	var in TodoWriteInput
	if err := json.Unmarshal(input, &in); err != nil {
		return &tools.Result{IsError: true, Content: fmt.Sprintf("TodoWrite: invalid input: %v", err)}, nil
	}
	allDone := len(in.Todos) > 0
	for i, item := range in.Todos {
		if item.ID == "" {
			return &tools.Result{IsError: true, Content: fmt.Sprintf("TodoWrite: todos[%d].id is required", i)}, nil
		}
		if item.Content == "" {
			return &tools.Result{IsError: true, Content: fmt.Sprintf("TodoWrite: todos[%d].content is required", i)}, nil
		}
		switch item.Status {
		case types.TodoPending, types.TodoInProgress, types.TodoCompleted:
		default:
			return &tools.Result{IsError: true, Content: fmt.Sprintf("TodoWrite: todos[%d].status must be pending, in_progress or completed, got %q", i, item.Status)}, nil
		}
		if item.Status != types.TodoCompleted {
			allDone = false
		}
	}

	if uctx != nil && uctx.TodoAppendOnly {
		t.store.SetState(func(prev state.AppState) state.AppState {
			prev.Todos = appendTodos(prev.Todos, in.Todos)
			return prev
		})
		return &tools.Result{Content: todoWriteResultText}, nil
	}

	// Oracle semantics: an all-completed list clears the panel.
	newTodos := in.Todos
	if allDone {
		newTodos = nil
	}
	t.store.SetState(func(prev state.AppState) state.AppState {
		prev.Todos = newTodos
		return prev
	})
	return &tools.Result{Content: todoWriteResultText}, nil
}

// btwTodoPrefix marks todo items appended by /btw.
const btwTodoPrefix = "[btw] "

// appendTodos returns existing with items appended (append-only mode, used by
// /btw): existing items and their status are never changed, contents are
// prefixed with btwTodoPrefix, duplicates are skipped and colliding IDs get a
// fresh one. The result is a new slice (the store's state is not aliased).
func appendTodos(existing, items []types.TodoItem) []types.TodoItem {
	out := append([]types.TodoItem(nil), existing...)
	ids := map[string]bool{}
	contents := map[string]bool{}
	for _, it := range out {
		ids[it.ID] = true
		contents[it.Content] = true
		contents[btwTodoPrefix+strings.TrimPrefix(it.Content, btwTodoPrefix)] = true // main items count too
	}
	for _, it := range items {
		if !strings.HasPrefix(it.Content, btwTodoPrefix) {
			it.Content = btwTodoPrefix + it.Content
		}
		if contents[it.Content] {
			continue
		}
		for base, n := it.ID, 1; ids[it.ID]; n++ {
			it.ID = fmt.Sprintf("%s-btw%d", base, n)
		}
		ids[it.ID] = true
		contents[it.Content] = true
		out = append(out, it)
	}
	return out
}

// ── AskUserQuestion ───────────────────────────────────────────────────────────

// AskUserQuestionInput is the input schema for AskUserQuestion.
type AskUserQuestionInput struct {
	// Question is the question to ask the user (required).
	Question string `json:"question"`
	// Options is an optional list of suggested reply options.
	Options []string `json:"options,omitempty"`
}

// UnmarshalJSON accepts each option either as a plain string or as a
// {label, description} object — the shape Claude Code's AskUserQuestion uses,
// which models frequently emit regardless of our string schema. Objects are
// flattened to "label — description" so the TUI keeps a []string.
func (in *AskUserQuestionInput) UnmarshalJSON(data []byte) error {
	var raw struct {
		Question string            `json:"question"`
		Options  []json.RawMessage `json:"options,omitempty"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	in.Question = raw.Question
	in.Options = nil
	for i, opt := range raw.Options {
		var s string
		if err := json.Unmarshal(opt, &s); err == nil {
			in.Options = append(in.Options, s)
			continue
		}
		var obj struct {
			Label       string `json:"label"`
			Description string `json:"description"`
		}
		if err := json.Unmarshal(opt, &obj); err != nil {
			return fmt.Errorf("options[%d]: want a string or {label, description} object", i)
		}
		switch {
		case obj.Label != "" && obj.Description != "":
			in.Options = append(in.Options, obj.Label+" — "+obj.Description)
		case obj.Label != "":
			in.Options = append(in.Options, obj.Label)
		case obj.Description != "":
			in.Options = append(in.Options, obj.Description)
		}
	}
	return nil
}

// NewAskUserQuestionTool constructs the AskUserQuestion tool backed by the
// HIL channel pair (BF-08), mirroring the permission dialog pipeline: the
// tool sends a QuestionRequest and blocks until the TUI returns a response.
// Nil channels (headless mode) make every call report an honest error instead
// of blocking forever.
func NewAskUserQuestionTool(askCh chan<- types.QuestionRequest, respCh <-chan types.QuestionResponse) tools.Tool {
	return &askUserQuestionTool{askCh: askCh, respCh: respCh}
}

type askUserQuestionTool struct {
	tools.BaseTool
	askCh  chan<- types.QuestionRequest
	respCh <-chan types.QuestionResponse
}

func (t *askUserQuestionTool) Name() string { return "AskUserQuestion" }

func (t *askUserQuestionTool) Description(_ tools.Input, _ tools.PermissionContext) string {
	return `Pauses execution and asks the user a clarifying question, waiting for their response.

Usage notes:
- Use only when genuinely blocked and cannot proceed without user input
- Prefer inference over asking when possible
- Optionally provide response options to guide the user`
}

func (t *askUserQuestionTool) InputSchema() tools.InputSchema {
	return tools.NewInputSchema(
		map[string]json.RawMessage{
			"question": tools.PropSchema(map[string]any{
				"type":        "string",
				"description": "The question to ask the user",
			}),
			"options": tools.PropSchema(map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "string"},
				"description": "Optional suggested response options",
			}),
		},
		[]string{"question"},
	)
}

func (t *askUserQuestionTool) IsConcurrencySafe(_ tools.Input) bool { return false }
func (t *askUserQuestionTool) IsReadOnly(_ tools.Input) bool        { return true }
func (t *askUserQuestionTool) InterruptBehavior() tools.InterruptBehavior {
	return tools.InterruptBehaviorBlock
}
func (t *askUserQuestionTool) UserFacingName(_ tools.Input) string { return "AskUserQuestion" }

func (t *askUserQuestionTool) Call(input tools.Input, uctx *tools.UseContext, _ tools.OnProgressFn) (*tools.Result, error) {
	if t.askCh == nil || t.respCh == nil {
		return &tools.Result{IsError: true, Content: "AskUserQuestion: not available in non-interactive mode"}, nil
	}
	var in AskUserQuestionInput
	if err := json.Unmarshal(input, &in); err != nil {
		return &tools.Result{IsError: true, Content: fmt.Sprintf("AskUserQuestion: invalid input: %v", err)}, nil
	}
	if in.Question == "" {
		return &tools.Result{IsError: true, Content: "AskUserQuestion: question is required"}, nil
	}

	ctx := context.Background()
	var abortCh <-chan struct{}
	if uctx != nil {
		if uctx.Ctx != nil {
			ctx = uctx.Ctx
		}
		abortCh = uctx.AbortCh
	}

	req := types.QuestionRequest{ID: string(ids.NewSessionId()), Question: in.Question, Options: in.Options}
	select {
	case t.askCh <- req:
	case <-ctx.Done():
		return &tools.Result{IsError: true, Content: "AskUserQuestion: cancelled before the question was delivered"}, nil
	case <-abortCh:
		return &tools.Result{IsError: true, Content: "AskUserQuestion: aborted"}, nil
	}

	select {
	case resp := <-t.respCh:
		if resp.Dismissed {
			return &tools.Result{Content: "The user dismissed the question without answering. Proceed with your best judgment or ask again later if still blocked."}, nil
		}
		return &tools.Result{Content: "User answered: " + resp.Answer}, nil
	case <-ctx.Done():
		return &tools.Result{IsError: true, Content: "AskUserQuestion: cancelled while waiting for the user's answer"}, nil
	case <-abortCh:
		return &tools.Result{IsError: true, Content: "AskUserQuestion: aborted while waiting for the user's answer"}, nil
	}
}
