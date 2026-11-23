package commands

import (
	"fmt"
	"os"
	"strings"

	"github.com/tunsuy/glaude/internal/memdir"
)

// RegisterBuiltins registers all built-in slash commands into r.
func RegisterBuiltins(r *Registry) {
	r.Register(cmdClear())
	r.Register(cmdHelp(r))
	r.Register(cmdExit())
	r.Register(cmdVim())
	r.Register(cmdTheme())
	r.Register(cmdModel())
	r.Register(cmdEffort())
	r.Register(cmdStatus())
	r.Register(cmdCost())
	r.Register(cmdSession())
	r.Register(cmdConfig())
	r.Register(cmdCompact())
	r.Register(cmdMemory())
	r.Register(cmdDream())
	r.Register(cmdMCP())
	r.Register(cmdTier())
	r.Register(cmdSetup())
	r.Register(cmdProvider())
	r.Register(cmdBTW())
	r.Register(cmdLoop())
	r.Register(cmdGoal())
	r.Register(cmdSetSoul())
	r.Register(cmdSoul())
	r.Register(cmdReview())
	r.Register(cmdCommit())
	r.Register(cmdDiff())
	r.Register(cmdInit())
	r.Register(cmdResume())
	r.Register(cmdTerminalSetup())
}

func cmdClear() *Command {
	return &Command{
		Name:        "clear",
		Description: "Clear the conversation history",
		Execute: func(ctx CommandContext, args string) Result {
			return Result{
				Text:         "Conversation cleared.",
				Display:      DisplayMessage,
				ClearHistory: true,
				NewMessages:  []interface{}{}, // signal to clear; tui layer type-asserts
			}
		},
	}
}

func cmdHelp(r *Registry) *Command {
	return &Command{
		Name:        "help",
		Description: "Show available slash commands",
		Execute: func(ctx CommandContext, args string) Result {
			cmds := r.All()
			var sb strings.Builder
			sb.WriteString("Available commands:\n\n")
			for _, cmd := range cmds {
				sb.WriteString(fmt.Sprintf("  /%-20s %s\n", cmd.Name, cmd.Description))
			}
			return Result{
				Text:    sb.String(),
				Display: DisplayMessage,
			}
		},
	}
}

func cmdExit() *Command {
	return &Command{
		Name:        "exit",
		Description: "Exit Glaude",
		Execute: func(ctx CommandContext, args string) Result {
			return Result{
				Text:       "Goodbye!",
				Display:    DisplayMessage,
				ShouldExit: true,
			}
		},
	}
}

func cmdVim() *Command {
	return &Command{
		Name:        "vim",
		Description: "Toggle Vim keybindings",
		Execute: func(ctx CommandContext, args string) Result {
			status := "enabled"
			if ctx.VimEnabled {
				status = "disabled"
			}
			return Result{
				Text:      fmt.Sprintf("Vim keybindings %s.", status),
				Display:   DisplayMessage,
				ToggleVim: true,
			}
		},
	}
}

func cmdTheme() *Command {
	return &Command{
		Name:        "theme",
		Description: "Switch color theme (e.g. /theme dark, /theme light, /theme tokyo-night)",
		Execute: func(ctx CommandContext, args string) Result {
			name := strings.TrimSpace(args)
			if name == "" {
				return Result{
					Text:    "Usage: /theme <name>  (dark | light | tokyo-night)",
					Display: DisplayMessage,
				}
			}
			return Result{
				Text:     fmt.Sprintf("Theme set to %q.", name),
				Display:  DisplayMessage,
				NewTheme: name,
			}
		},
	}
}

func cmdModel() *Command {
	return &Command{
		Name:        "model",
		Description: "Switch the main model for this session (menu, or /model <tier|provider:model|model-id>)",
		Execute: func(_ CommandContext, args string) Result {
			if ref := strings.TrimSpace(args); ref != "" {
				return Result{ModelRef: ref, Display: DisplayNone}
			}
			return Result{OpenDialog: "model", Display: DisplayNone}
		},
	}
}

func cmdEffort() *Command {
	return &Command{
		Name:        "effort",
		Description: "Set effort level (low, medium, high) to tune speed vs. intelligence",
		Execute: func(ctx CommandContext, args string) Result {
			level := strings.TrimSpace(strings.ToLower(args))
			validLevels := map[string]string{
				"low":    "low",
				"l":      "low",
				"medium": "medium",
				"med":    "medium",
				"m":      "medium",
				"high":   "high",
				"h":      "high",
			}

			if level == "" {
				current := ctx.Effort
				if current == "" {
					current = "medium"
				}
				return Result{
					Text: fmt.Sprintf("Current effort level: %s\n\n"+
						"Usage: /effort <level>\n"+
						"  low    - Fast responses, less thorough (good for simple tasks)\n"+
						"  medium - Balanced speed and intelligence (default)\n"+
						"  high   - Thorough thinking, slower responses (good for complex tasks)",
						current),
					Display: DisplayMessage,
				}
			}

			normalized, valid := validLevels[level]
			if !valid {
				return Result{
					Text:    fmt.Sprintf("Invalid effort level %q. Use: low, medium, or high", level),
					Display: DisplayError,
				}
			}

			return Result{
				Text:      fmt.Sprintf("Effort level set to %q.", normalized),
				Display:   DisplayMessage,
				NewEffort: normalized,
			}
		},
	}
}

func cmdStatus() *Command {
	return &Command{
		Name:        "status",
		Description: "Show session status (model, working dir, message count)",
		Execute: func(ctx CommandContext, args string) Result {
			text := fmt.Sprintf(
				"Session: %s\nModel:   %s\nCWD:     %s\nMessages: %d",
				ctx.SessionID,
				ctx.Model,
				ctx.WorkingDir,
				ctx.MessageCount,
			)
			return Result{
				Text:    text,
				Display: DisplayMessage,
			}
		},
	}
}

func cmdCost() *Command {
	return &Command{
		Name:        "cost",
		Description: "Show token usage for this session",
		Execute: func(ctx CommandContext, args string) Result {
			text := fmt.Sprintf(
				"Token usage — Input: %d  Output: %d",
				ctx.TokensInput,
				ctx.TokensOutput,
			)
			return Result{
				Text:    text,
				Display: DisplayMessage,
			}
		},
	}
}

func cmdSession() *Command {
	return &Command{
		Name:        "session",
		Description: "Show current session ID",
		Execute: func(ctx CommandContext, args string) Result {
			return Result{
				Text:    fmt.Sprintf("Session ID: %s", ctx.SessionID),
				Display: DisplayMessage,
			}
		},
	}
}

func cmdConfig() *Command {
	return &Command{
		Name:        "config",
		Description: "Open configuration settings",
		Execute: func(ctx CommandContext, args string) Result {
			// The TUI renders a read-only settings panel (dialogConfig);
			// editing happens via /model, /theme, /vim, /effort and settings files.
			return Result{OpenDialog: "config"}
		},
	}
}

func cmdCompact() *Command {
	return &Command{
		Name:        "compact",
		Description: "Compact the conversation history to save context",
		Execute: func(ctx CommandContext, args string) Result {
			// The actual compact logic is handled in the TUI update loop
			// because it needs access to the query engine.
			// OpenDialog tells the TUI to show the compact confirmation dialog.
			return Result{
				Display:    DisplayNone,
				OpenDialog: "compact",
			}
		},
	}
}

func cmdMemory() *Command {
	return &Command{
		Name:        "memory",
		Description: "Show loaded GLAUDE.md memory files and auto-memories",
		Execute: func(ctx CommandContext, args string) Result {
			store, _ := memdir.NewMemoryStore(ctx.WorkingDir)
			text := memdir.FormatMemoryStatus(ctx.WorkingDir, store)
			return Result{
				Text:    text,
				Display: DisplayMessage,
			}
		},
	}
}

func cmdDream() *Command {
	return &Command{
		Name:        "dream",
		Description: "Manually trigger memory consolidation (auto-dream)",
		Execute: func(_ CommandContext, _ string) Result {
			return Result{
				Display:      DisplayNone,
				TriggerDream: true,
			}
		},
	}
}

func cmdMCP() *Command {
	return &Command{
		Name:        "mcp",
		Description: "Show MCP server connection status",
		Execute: func(ctx CommandContext, args string) Result {
			// The status report may query connected servers (ListTools), so
			// the TUI fetches it asynchronously via tea.Cmd (see
			// commands.FormatMCPStatus) instead of blocking Update().
			return Result{
				Display:        DisplayNone,
				FetchMCPStatus: true,
			}
		},
	}
}

func cmdReview() *Command {
	return &Command{
		Name:        "review",
		Description: "Review the current git diff with Claude",
		Execute: func(ctx CommandContext, args string) Result {
			return Result{
				Text:    "Please review the current git diff and provide feedback.",
				Display: DisplayNone,
			}
		},
	}
}

func cmdCommit() *Command {
	return &Command{
		Name:        "commit",
		Description: "Ask Claude to write and create a git commit",
		Execute: func(ctx CommandContext, args string) Result {
			return Result{
				Text:    "Please create a git commit for the current staged changes.",
				Display: DisplayNone,
			}
		},
	}
}

func cmdDiff() *Command {
	return &Command{
		Name:        "diff",
		Description: "Show the current git diff",
		Execute: func(ctx CommandContext, args string) Result {
			return Result{
				Text:    "Please show the current git diff.",
				Display: DisplayNone,
			}
		},
	}
}

func cmdInit() *Command {
	return &Command{
		Name:        "init",
		Description: "Generate a GLAUDE.md for this project",
		Execute: func(ctx CommandContext, args string) Result {
			return Result{
				Text:    "Please generate a GLAUDE.md file for this project.",
				Display: DisplayNone,
			}
		},
	}
}

func cmdResume() *Command {
	return &Command{
		Name:        "resume",
		Description: "List previous sessions, or resume one (/resume <id>)",
		Execute: func(ctx CommandContext, args string) Result {
			sessionID := strings.TrimSpace(args)
			if sessionID == "" {
				sessions, err := listSessions(ctx.WorkingDir, resumeScanLimit)
				if err != nil {
					return Result{Text: err.Error(), Display: DisplayError}
				}
				if len(sessions) == 0 {
					return Result{
						Text:    "No previous sessions found for this project.",
						Display: DisplayMessage,
					}
				}
				listed := sessions
				if len(listed) > resumeListLimit {
					listed = listed[:resumeListLimit]
				}
				result := Result{
					Text:    formatSessionList(listed),
					Display: DisplayMessage,
				}
				// The interactive picker (TUI) gets the non-empty sessions.
				if picks := summaries(sessions, ctx.SessionID); len(picks) > 0 {
					result.OpenDialog = "resume"
					result.Sessions = picks
				}
				return result
			}

			msgs, err := loadSessionMessages(ctx.WorkingDir, sessionID)
			if err != nil {
				return Result{Text: err.Error(), Display: DisplayError}
			}
			return Result{
				Text:         fmt.Sprintf("Resumed session %s (%d messages).", sessionID, len(msgs)),
				Display:      DisplayMessage,
				NewMessages:  msgs,
				NewSessionID: sessionID,
			}
		},
	}
}

func cmdTerminalSetup() *Command {
	return &Command{
		Name:        "terminal-setup",
		Description: "Detect terminal environment and show setup guidance",
		Execute: func(ctx CommandContext, args string) Result {
			return Result{
				Text:    FormatTerminalSetup(DetectTerminal(os.Getenv)),
				Display: DisplayMessage,
			}
		},
	}
}

// cmdBTW is listed for /help and completion; the TUI intercepts "/btw" before
// command dispatch so it runs even while a query is in progress.
func cmdBTW() *Command {
	return &Command{
		Name:        "btw",
		Description: "Ask a side question in parallel (read-only; ctrl+y sends the answer to the main conversation)",
		Execute: func(_ CommandContext, _ string) Result {
			return Result{Text: "Usage: /btw [--tier <name>] <question>", Display: DisplayMessage}
		},
	}
}
