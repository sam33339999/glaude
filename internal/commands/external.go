package commands

import (
	"fmt"
	"strings"
)

// ExternalAgentCommand describes an external ACP agent exposed as /<name>.
type ExternalAgentCommand struct {
	Name        string
	Description string
	// ListModels, when non-nil, returns a formatted list of available models
	// for `/<name> --models`.
	ListModels func(cwd string) (string, error)
}

// ExternalAgentSpawner starts a background delegation to the named external
// agent (optionally overriding its model when model is non-empty) and returns
// the agent ID.
type ExternalAgentSpawner func(name, model, task string) (string, error)

// RegisterExternalAgents registers one /<name> command per external agent.
// "/grok [-m <model>] <task>" hands the task to that agent's long-lived
// session; "/grok --models" lists available models; progress streams in the
// agent panel and the result returns to the conversation. Names already taken
// by another command are skipped.
func RegisterExternalAgents(r *Registry, agents []ExternalAgentCommand, spawn ExternalAgentSpawner) {
	if spawn == nil {
		return
	}
	for _, a := range agents {
		if a.Name == "" || r.Lookup(a.Name) != nil {
			continue
		}
		name := a.Name
		listModels := a.ListModels
		r.Register(&Command{
			Name:        name,
			Description: fmt.Sprintf("Delegate a task to the external %s agent (one long-lived session): /%s [-m <model>|--models] <task>", name, name),
			Execute: func(ctx CommandContext, args string) Result {
				trimmed := strings.TrimSpace(args)
				if trimmed == "--models" || trimmed == "models" {
					if listModels == nil {
						return Result{Text: fmt.Sprintf("/%s: model listing is not available", name), Display: DisplayError}
					}
					out, err := listModels(ctx.WorkingDir)
					if err != nil {
						return Result{Text: fmt.Sprintf("/%s: %v", name, err), Display: DisplayError}
					}
					return Result{Text: out, Display: DisplayMessage}
				}
				model, task, ok := parseExternalAgentArgs(args)
				if !ok || task == "" {
					return Result{Text: fmt.Sprintf("Usage: /%s [-m <model>] <task>  (or /%s --models)", name, name), Display: DisplayError}
				}
				id, err := spawn(name, model, task)
				if err != nil {
					return Result{Text: fmt.Sprintf("/%s: %v", name, err), Display: DisplayError}
				}
				return Result{
					Text:    fmt.Sprintf("Delegated to %s (%s). Progress shows in the agent panel; the result returns to this conversation when it finishes.", name, id),
					Display: DisplayMessage,
				}
			},
		})
	}
}

// parseExternalAgentArgs extracts an optional leading -m/--model flag from args.
func parseExternalAgentArgs(args string) (model, task string, ok bool) {
	s := strings.TrimSpace(args)
	for _, flag := range []string{"--model=", "-m="} {
		if strings.HasPrefix(s, flag) {
			rest := strings.TrimPrefix(s, flag)
			val, rem := splitFirstToken(rest)
			if val == "" {
				return "", "", false
			}
			return val, strings.TrimSpace(rem), true
		}
	}
	if strings.HasPrefix(s, "-m ") || strings.HasPrefix(s, "-m\t") || strings.HasPrefix(s, "--model ") || strings.HasPrefix(s, "--model\t") {
		_, rest := splitFirstToken(s)
		val, rem := splitFirstToken(strings.TrimSpace(rest))
		if val == "" {
			return "", "", false
		}
		return val, strings.TrimSpace(rem), true
	}
	if s == "-m" || s == "--model" {
		return "", "", false
	}
	return "", s, true
}

func splitFirstToken(s string) (token, rest string) {
	for i, r := range s {
		if r == ' ' || r == '\t' || r == '\n' {
			return s[:i], s[i:]
		}
	}
	return s, ""
}
