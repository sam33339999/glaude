package tui

import (
	"fmt"
	"strings"

	"github.com/tunsuy/glaude/internal/commands"
	"github.com/tunsuy/glaude/internal/modeltier"
)

// view renders the current wizard screen inside a dialog box.
func (w *providerWizard) view(width, maxHeight int, theme Theme) string {
	var body string
	switch w.step {
	case pvList:
		body = w.viewList(theme)
	case pvConfirmDelete:
		body = w.viewConfirmDelete(theme)
	case pvID:
		body = "Provider id (lower-case letters, digits, '-' and '_'):\n\n" +
			primaryStyle(theme).Render("❯ ") + string(w.id) + "█\n\n" +
			mutedStyle(theme).Render("Enter next · Esc back")
	case pvFormat:
		body = w.viewFormat(theme)
	case pvEndpoint:
		body = w.viewEndpoint(theme)
	case pvValidating:
		body = "Checking the endpoint (GET /models)…\n\n" + mutedStyle(theme).Render("Esc cancel")
	case pvModels:
		body = w.modelChooser.view(maxHeight, theme)
	case pvManualModels:
		body = "This endpoint does not support listing models.\n" +
			"Enter model IDs (comma-separated):\n\n" +
			primaryStyle(theme).Render("❯ ") + string(w.manual) + "█\n\n" +
			mutedStyle(theme).Render("Enter next · Esc back")
	case pvTiers:
		body = w.viewTiers(theme)
	case pvConfirm:
		body = w.viewConfirm(theme)
	case pvWriting:
		body = "Writing…"
	case pvDone:
		body = w.viewDone(theme)
	}
	if w.errMsg != "" && w.step != pvDone {
		body = errorStyle(theme).Render("Error: "+w.errMsg) + "\n\n" + body
	}
	title := "Providers"
	if id := w.providerID(); w.step != pvList && w.step != pvConfirmDelete && id != "" {
		title += " · " + id
	}
	return dialogBox(primaryStyle(theme).Bold(true).Render(title)+"\n\n"+body, width, theme)
}

// keyStatus describes where a listed provider's key comes from.
func keyStatus(p commands.ProviderInfo) string {
	var s string
	switch p.KeySource {
	case modeltier.KeySourceEnv:
		s = "key: env " + p.KeyEnv
	case modeltier.KeySourceStored:
		s = "key: stored"
	default:
		return "key: none"
	}
	if !p.KeyPresent {
		s += " (missing)"
	}
	return s
}

func (w *providerWizard) viewList(theme Theme) string {
	var sb strings.Builder
	for _, warn := range w.warnings {
		sb.WriteString(errorStyle(theme).Render("! "+warn) + "\n")
	}
	if w.writable != nil {
		sb.WriteString(errorStyle(theme).Render("! read-only: "+w.writable.Error()) + "\n")
	}
	if sb.Len() > 0 {
		sb.WriteString("\n")
	}
	sb.WriteString(mutedStyle(theme).Render("  main        (main loop — configure with /setup)") + "\n")
	for i, p := range w.list {
		line := fmt.Sprintf("%-11s %-6s %s   %s", p.ID, formatName(p.Format), p.BaseURL, keyStatus(p))
		if len(p.Tiers) > 0 {
			line += "   " + strings.Join(p.Tiers, ",")
		}
		if p.ID == w.mainProvider {
			line += "   (main loop)"
		}
		sb.WriteString(cursorLine(i == w.listCursor, line, theme))
	}
	sb.WriteString(cursorLine(w.listCursor == len(w.list), "+ Add provider…", theme))
	sb.WriteString("\n" + mutedStyle(theme).Render("Enter edit/add · d delete · Esc close"))
	return sb.String()
}

func cursorLine(selected bool, line string, theme Theme) string {
	if selected {
		return primaryStyle(theme).Render("❯ ") + line + "\n"
	}
	return "  " + line + "\n"
}

func formatName(f string) string {
	if f == commands.SetupFormatAnthropic {
		return "anthropic"
	}
	return "openai"
}

func (w *providerWizard) viewConfirmDelete(theme Theme) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "Delete provider %s?\n", w.deleting)
	for _, p := range w.list {
		if p.ID == w.deleting && len(p.Tiers) > 0 {
			fmt.Fprintf(&sb, "Tiers bound to it will be unbound (they inherit the main model): %s\n", strings.Join(p.Tiers, ", "))
		}
	}
	sb.WriteString("Its stored API key is removed too.\n\n")
	sb.WriteString(mutedStyle(theme).Render("Enter delete · Esc back"))
	return sb.String()
}

func (w *providerWizard) viewFormat(theme Theme) string {
	var sb strings.Builder
	sb.WriteString("API format:\n")
	for i, f := range setupFormats {
		sb.WriteString(cursorLine(i == w.formatIdx, f.label, theme))
	}
	sb.WriteString("\n" + mutedStyle(theme).Render("↑/↓ choose · Enter next · Esc back"))
	return sb.String()
}

// currentKeyHint describes the key an empty field keeps (edit only).
func (w *providerWizard) currentKeyHint() string {
	if w.editing == nil {
		return ""
	}
	switch w.editing.KeySource {
	case modeltier.KeySourceEnv:
		return "environment variable " + w.editing.KeyEnv
	case modeltier.KeySourceStored:
		return "the stored key"
	}
	return ""
}

func (w *providerWizard) viewEndpoint(theme Theme) string {
	field := func(label, value string, focused bool) string {
		if focused {
			return primaryStyle(theme).Render("❯ "+label+": ") + value + "█"
		}
		return "  " + label + ": " + value
	}
	var sb strings.Builder
	sb.WriteString(field("Base URL", string(w.baseURL), w.focus == 0) + "\n")
	if !w.noKey {
		sb.WriteString(field("API key ", strings.Repeat("•", len(w.apiKey)), w.focus == 1) + "\n")
		if hint := w.currentKeyHint(); len(w.apiKey) == 0 && hint != "" {
			sb.WriteString(mutedStyle(theme).Render("    (leave empty to keep using "+hint+")") + "\n")
		}
	} else {
		sb.WriteString(mutedStyle(theme).Render("  API key : (not needed)") + "\n")
	}
	box := "[ ]"
	if w.noKey {
		box = "[x]"
	}
	toggle := box + " No key needed (local endpoints such as Ollama)"
	if w.focus == 2 {
		sb.WriteString(primaryStyle(theme).Render("❯ ") + toggle + "\n")
	} else {
		sb.WriteString("  " + toggle + "\n")
	}
	sb.WriteString("\n" + mutedStyle(theme).Render("Tab switch field · Space toggle · Enter verify · Esc back"))
	return sb.String()
}

func (w *providerWizard) tierLabel(i int) string {
	if c := w.tierChoice[i]; c >= 0 {
		return w.checkOrder[c]
	}
	if w.editing != nil {
		for _, t := range w.editing.Tiers {
			if t == providerTiers[i] {
				return "keep (bound to this provider)"
			}
		}
	}
	return "keep current binding"
}

func (w *providerWizard) viewTiers(theme Theme) string {
	var sb strings.Builder
	sb.WriteString("Bind tiers to this provider (optional):\n")
	for i, tier := range providerTiers {
		sb.WriteString(cursorLine(i == w.tierCursor, fmt.Sprintf("%-5s ← %s →", tier, w.tierLabel(i)), theme))
	}
	sb.WriteString("\n" + mutedStyle(theme).Render("↑/↓ tier · ←/→ model · Enter next · Esc back"))
	return sb.String()
}

// keyDescription says what key will be used, never showing more than the last 4 characters.
func (w *providerWizard) keyDescription() string {
	in := w.input()
	switch {
	case in.NoKey:
		return "none (no key needed)"
	case in.APIKey != "":
		k := []rune(in.APIKey)
		return "new key ••••" + string(k[max(len(k)-4, 0):]) + " (stored in credentials.json)"
	case w.currentKeyHint() != "":
		return "unchanged (" + w.currentKeyHint() + ")"
	}
	return "none"
}

func (w *providerWizard) viewConfirm(theme Theme) string {
	in := w.input()
	var sb strings.Builder
	verb := "Update"
	if w.isNew() {
		verb = "Add"
	}
	fmt.Fprintf(&sb, "%s provider %s\n", verb, in.ID)
	fmt.Fprintf(&sb, "  format   %s\n", formatName(in.Format))
	fmt.Fprintf(&sb, "  base URL %s\n", in.BaseURL)
	fmt.Fprintf(&sb, "  API key  %s\n", w.keyDescription())
	fmt.Fprintf(&sb, "  models   %s\n", strings.Join(w.checkOrder, ", "))
	for tier, model := range w.plan().Tiers {
		fmt.Fprintf(&sb, "  bind     %s → %s:%s\n", tier, in.ID, model)
	}
	if w.editsMain() {
		sb.WriteString("\n" + errorStyle(theme).Render("The main loop will switch to the new settings; the prompt cache is invalidated and the next request resends the whole conversation.") + "\n")
	}
	if e := w.editing; e != nil && len(e.Tiers) > 0 && (e.Format != in.Format || e.BaseURL != in.BaseURL) {
		sb.WriteString("\n" + mutedStyle(theme).Render(fmt.Sprintf("Tiers %s stay bound to this provider; the new endpoint may not serve their models.", strings.Join(e.Tiers, ", "))) + "\n")
	}
	sb.WriteString("\n" + mutedStyle(theme).Render("Enter save · Esc back"))
	return sb.String()
}

func (w *providerWizard) viewDone(theme Theme) string {
	var sb strings.Builder
	switch {
	case w.deleted:
		fmt.Fprintf(&sb, "Provider %s removed.\n", w.deleting)
		if len(w.report.Unbound) > 0 {
			fmt.Fprintf(&sb, "Unbound tiers (now inherit the main model): %s\n", strings.Join(w.report.Unbound, ", "))
		}
	default:
		fmt.Fprintf(&sb, "Saved provider %s. It is ready to use.\n", w.providerID())
		if len(w.checkOrder) > 0 {
			fmt.Fprintf(&sb, "Try /model %s:%s\n", w.providerID(), w.checkOrder[0])
		}
		if w.report.MainSwitched {
			sb.WriteString("The main loop now uses the new settings.\n")
		}
	}
	for _, warn := range w.report.Warnings {
		sb.WriteString(errorStyle(theme).Render("! "+warn) + "\n")
	}
	sb.WriteString("\n" + mutedStyle(theme).Render("Press any key to close"))
	return sb.String()
}
