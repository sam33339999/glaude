package commands

import (
	"fmt"
	"os"
	"strings"

	"github.com/tunsuy/glaude/internal/soul"
)

const setSoulUsage = "Usage: /set-soul [--global] <text>   save the soul (project by default)\n" +
	"       /soul                          show the active soul\n" +
	"       /soul clear [--global]         remove it"

// soulHome is the home directory soul commands use (swapped in tests).
var soulHome = func() string { h, _ := os.UserHomeDir(); return h }

// splitGlobal strips a leading --global flag.
func splitGlobal(args string) (global bool, rest string) {
	args = strings.TrimSpace(args)
	if args == "--global" || strings.HasPrefix(args, "--global ") || strings.HasPrefix(args, "--global\n") {
		return true, strings.TrimSpace(strings.TrimPrefix(args, "--global"))
	}
	return false, args
}

func soulPath(ctx CommandContext, global bool) string {
	if global {
		return soul.GlobalPath(soulHome())
	}
	return soul.ProjectPath(ctx.WorkingDir)
}

func cmdSetSoul() *Command {
	return &Command{
		Name:        "set-soul",
		Description: "Save a standing identity/rules for the agent (system prompt; survives auto-compact; sub-agents get it too)",
		Execute: func(ctx CommandContext, args string) Result {
			global, text := splitGlobal(args)
			if text == "" {
				return Result{Text: setSoulUsage, Display: DisplayMessage}
			}
			path := soulPath(ctx, global)
			if err := soul.Save(path, text); err != nil {
				return Result{Text: "/set-soul: " + err.Error(), Display: DisplayError}
			}
			return Result{Text: fmt.Sprintf(
				"Soul saved to %s (%d chars).\nIt is part of the system prompt from the next request on — for the main agent and every sub-agent — so auto-compact cannot drop it. Changing it invalidates the prompt cache once.",
				path, len([]rune(strings.TrimSpace(text)))), Display: DisplayMessage}
		},
	}
}

func cmdSoul() *Command {
	return &Command{
		Name:        "soul",
		Description: "Show the active soul (/soul clear [--global] to remove it)",
		Execute: func(ctx CommandContext, args string) Result {
			sub, rest, _ := strings.Cut(strings.TrimSpace(args), " ")
			switch sub {
			case "":
				srcs := soul.Sources(soulHome(), ctx.WorkingDir)
				if len(srcs) == 0 {
					return Result{Text: "No soul set.\n" + setSoulUsage, Display: DisplayMessage}
				}
				var sb strings.Builder
				for i, s := range srcs {
					if i > 0 {
						sb.WriteString("\n\n")
					}
					fmt.Fprintf(&sb, "── %s soul (%s) ──\n%s", s.Scope, s.Path, s.Text)
				}
				return Result{Text: sb.String(), Display: DisplayMessage}
			case "clear":
				global, _ := splitGlobal(rest)
				path := soulPath(ctx, global)
				ok, err := soul.Clear(path)
				switch {
				case err != nil:
					return Result{Text: "/soul clear: " + err.Error(), Display: DisplayError}
				case !ok:
					return Result{Text: "No soul at " + path, Display: DisplayMessage}
				}
				return Result{Text: "Soul removed: " + path, Display: DisplayMessage}
			}
			return Result{Text: setSoulUsage, Display: DisplayMessage}
		},
	}
}
