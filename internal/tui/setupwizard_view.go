package tui

import (
	"fmt"
	"strings"
)

// view renders the current wizard screen inside a dialog box.
func (w *setupWizard) view(width, maxHeight int, theme Theme) string {
	var body string
	switch w.step {
	case stepFormat:
		body = w.viewFormat(theme)
	case stepEndpoint:
		body = w.viewEndpoint(theme)
	case stepValidating:
		body = "Checking the endpoint (GET /models)…\n\n" + mutedStyle(theme).Render("Esc cancel")
	case stepModels:
		body = w.modelChooser.view(maxHeight, theme)
	case stepManualModels:
		body = w.viewManual(theme)
	case stepRoles:
		body = w.viewRoles(theme)
	case stepConfirm:
		body = w.viewConfirm(theme)
	case stepWriting:
		body = "Writing settings…"
	case stepDone:
		body = w.viewDone(theme)
	}
	if w.errMsg != "" && w.step != stepDone {
		body = errorStyle(theme).Render("Error: "+w.errMsg) + "\n\n" + body
	}
	title := primaryStyle(theme).Bold(true).Render("Setup · main model")
	return dialogBox(title+"\n\n"+body, width, theme)
}

func (w *setupWizard) viewFormat(theme Theme) string {
	var sb strings.Builder
	sb.WriteString("API format:\n")
	for i, f := range setupFormats {
		if i == w.formatIdx {
			sb.WriteString(primaryStyle(theme).Render("❯ "+f.label) + "\n")
		} else {
			sb.WriteString("  " + f.label + "\n")
		}
	}
	sb.WriteString("\n" + mutedStyle(theme).Render("↑/↓ choose · Enter next · Esc cancel"))
	return sb.String()
}

func (w *setupWizard) viewEndpoint(theme Theme) string {
	field := func(label, value string, focused bool) string {
		cursor := ""
		if focused {
			cursor = "█"
			return primaryStyle(theme).Render("❯ "+label+": ") + value + cursor
		}
		return "  " + label + ": " + value
	}
	key := strings.Repeat("•", len(w.apiKey))
	var sb strings.Builder
	sb.WriteString(field("Base URL", string(w.baseURL), w.focus == 0) + "\n")
	sb.WriteString(field("API key ", key, w.focus == 1) + "\n")
	if len(w.apiKey) == 0 && w.keyReusable() {
		src := "the key in settings.json"
		if env, ok := strings.CutPrefix(w.defaults.KeySources[w.format()], "env:"); ok {
			src = "environment variable " + env
		}
		sb.WriteString(mutedStyle(theme).Render("    (already set — leave empty to keep using "+src+")") + "\n")
	}
	sb.WriteString("\n" + mutedStyle(theme).Render("Tab switch field · Enter verify · Esc back"))
	return sb.String()
}

func (w *setupWizard) viewManual(theme Theme) string {
	return "This endpoint does not support listing models.\n" +
		"Enter model IDs (comma-separated):\n\n" +
		primaryStyle(theme).Render("❯ ") + string(w.manual) + "█\n\n" +
		mutedStyle(theme).Render("Enter next · Esc back")
}

// roleLabel describes a role's current choice.
func (w *setupWizard) roleLabel(i int) string {
	role := setupRoles[i]
	if cur, ok := w.fixed[role]; ok {
		return "keep " + cur + "  (other provider, unchanged)"
	}
	if c := w.roleChoice[i]; c >= 0 {
		return w.checkOrder[c]
	}
	if cur := w.defaults.Tiers[role]; strings.HasPrefix(cur, "main:") {
		return "(unbound)  — " + cur + " will be unbound"
	}
	return "(unbound)"
}

func (w *setupWizard) viewRoles(theme Theme) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "%d models checked. Assign roles:\n", len(w.checkOrder))
	for i, role := range setupRoles {
		line := fmt.Sprintf("%-5s %s", role, w.roleLabel(i))
		if i == w.roleCursor {
			sb.WriteString(primaryStyle(theme).Render("❯ "+line) + "\n")
		} else {
			sb.WriteString("  " + line + "\n")
		}
	}
	sb.WriteString("\n" + mutedStyle(theme).Render("↑/↓ role · ←/→ change model · Enter next · Esc back"))
	return sb.String()
}

func (w *setupWizard) keyDescription() string {
	if k := w.input().APIKey; k != "" {
		suffix := k
		if len(suffix) > 4 {
			suffix = suffix[len(suffix)-4:]
		}
		return "•••• " + suffix
	}
	if env, ok := strings.CutPrefix(w.defaults.KeySources[w.format()], "env:"); ok {
		return "from environment variable " + env + " (not written)"
	}
	return "keep the key in settings.json"
}

func (w *setupWizard) viewConfirm(theme Theme) string {
	p := w.plan()
	var sb strings.Builder
	sb.WriteString("Will write ~/.glaude/settings.json:\n")
	fmt.Fprintf(&sb, "  provider  %s\n  baseUrl   %s\n  model     %s\n  apiKey    %s\n", p.Input.Format, p.Input.BaseURL, p.MainModel, w.keyDescription())
	if len(p.Tiers) > 0 {
		sb.WriteString("\nWill update ~/.glaude/config.toml tiers:\n")
		for _, role := range setupRoles[1:] {
			model, ok := p.Tiers[role]
			switch {
			case !ok:
			case model == "":
				fmt.Fprintf(&sb, "  %-5s unbind (was %s)\n", role, w.defaults.Tiers[role])
			default:
				fmt.Fprintf(&sb, "  %-5s main:%s\n", role, model)
			}
		}
	}
	for _, o := range w.overrides {
		sb.WriteString("\n" + warningStyle(theme).Render("⚠ "+o))
	}
	sb.WriteString("\n\n" + mutedStyle(theme).Render("Enter write · Esc back"))
	return sb.String()
}

func (w *setupWizard) viewDone(theme Theme) string {
	var sb strings.Builder
	sb.WriteString(successStyle(theme).Render("Setup saved.") + "\n")
	for _, path := range []string{w.report.SettingsPath, w.report.ConfigPath} {
		if path != "" {
			sb.WriteString("  " + path + "\n")
		}
	}
	sb.WriteString("\nRestart glaude to use the new settings.\n")
	sb.WriteString(mutedStyle(theme).Render("Some endpoints list models without checking the key; if the first message fails with 401, run /setup again.") + "\n")
	warnings := append(append([]string(nil), w.overrides...), w.report.Warnings...)
	for _, o := range warnings {
		sb.WriteString(warningStyle(theme).Render("⚠ "+o) + "\n")
	}
	sb.WriteString("\n" + mutedStyle(theme).Render("Press any key to close"))
	return sb.String()
}
