package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/tunsuy/glaude/internal/commands"
)

// modelChooser is the type-to-filter, space-to-check model list shared by
// the /setup and /provider wizards. It does no I/O.
type modelChooser struct {
	models     []commands.SetupModel
	filter     []rune
	cursor     int // index into filtered()
	checked    map[string]bool
	checkOrder []string
}

// chooserResult tells the owning wizard what a key press finished.
type chooserResult int

const (
	chooserNone chooserResult = iota
	chooserBack               // Esc
	chooserDone               // Enter with at least one model checked
)

// newModelChooser lists models with prechecked IDs checked in order. A
// prechecked ID that is not listed stays checked.
func newModelChooser(models []commands.SetupModel, prechecked []string) modelChooser {
	c := modelChooser{models: models, checked: map[string]bool{}}
	for _, id := range prechecked {
		if !c.checked[id] {
			c.toggle(id)
		}
	}
	return c
}

// filtered returns the models matching the filter (case-insensitive substring).
func (c *modelChooser) filtered() []commands.SetupModel {
	q := strings.ToLower(string(c.filter))
	if q == "" {
		return c.models
	}
	var out []commands.SetupModel
	for _, m := range c.models {
		if strings.Contains(strings.ToLower(m.ID), q) {
			out = append(out, m)
		}
	}
	return out
}

func (c *modelChooser) toggle(id string) {
	if c.checked == nil {
		c.checked = map[string]bool{}
	}
	if c.checked[id] {
		delete(c.checked, id)
		for i, v := range c.checkOrder {
			if v == id {
				c.checkOrder = append(c.checkOrder[:i], c.checkOrder[i+1:]...)
				break
			}
		}
		return
	}
	c.checked[id] = true
	c.checkOrder = append(c.checkOrder, id)
}

// handleKey moves, filters and checks. Enter with nothing checked checks the
// cursor row first.
func (c *modelChooser) handleKey(key tea.KeyPressMsg) chooserResult {
	list := c.filtered()
	switch key.Code {
	case tea.KeyUp:
		if c.cursor > 0 {
			c.cursor--
		}
	case tea.KeyDown:
		if c.cursor < len(list)-1 {
			c.cursor++
		}
	case tea.KeySpace:
		if c.cursor < len(list) {
			c.toggle(list[c.cursor].ID)
		}
	case tea.KeyBackspace:
		c.filter = dropLast(c.filter)
		c.cursor = 0
	case tea.KeyEsc:
		return chooserBack
	case tea.KeyEnter:
		if len(c.checkOrder) == 0 && c.cursor < len(list) {
			c.toggle(list[c.cursor].ID)
		}
		if len(c.checkOrder) > 0 {
			return chooserDone
		}
	default:
		if key.Text != "" {
			c.filter = append(c.filter, []rune(key.Text)...)
			c.cursor = 0
		}
	}
	return chooserNone
}

// paste appends text to the filter.
func (c *modelChooser) paste(text string) {
	c.filter = append(c.filter, []rune(text)...)
	c.cursor = 0
}

// view renders the search line and a scrolling window of the list.
func (c *modelChooser) view(maxHeight int, theme Theme) string {
	list := c.filtered()
	var sb strings.Builder
	fmt.Fprintf(&sb, "Search: %s█   %s\n", string(c.filter),
		mutedStyle(theme).Render(fmt.Sprintf("(%d models, %d match, %d checked)", len(c.models), len(list), len(c.checkOrder))))
	rows := max(maxHeight-10, 3)
	start := 0
	if c.cursor >= rows {
		start = c.cursor - rows + 1
	}
	end := min(start+rows, len(list))
	if start > 0 {
		sb.WriteString(mutedStyle(theme).Render(fmt.Sprintf("  ↑ %d more", start)) + "\n")
	}
	for i := start; i < end; i++ {
		m := list[i]
		box := "[ ]"
		if c.checked[m.ID] {
			box = "[x]"
		}
		line := box + " " + m.ID
		if m.ContextWindow > 0 {
			line += "  " + mutedStyle(theme).Render(compactCount(m.ContextWindow))
		}
		if i == c.cursor {
			sb.WriteString(primaryStyle(theme).Render("❯ ") + line + "\n")
		} else {
			sb.WriteString("  " + line + "\n")
		}
	}
	if end < len(list) {
		sb.WriteString(mutedStyle(theme).Render(fmt.Sprintf("  ↓ %d more", len(list)-end)) + "\n")
	}
	if len(list) == 0 {
		sb.WriteString(mutedStyle(theme).Render("  (no model matches)") + "\n")
	}
	sb.WriteString("\n" + mutedStyle(theme).Render("type to search · ↑/↓ move · Space check · Enter next · Esc back"))
	return sb.String()
}
