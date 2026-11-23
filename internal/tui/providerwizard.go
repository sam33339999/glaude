package tui

import (
	"errors"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/tunsuy/glaude/internal/commands"
	"github.com/tunsuy/glaude/internal/modeltier"
)

// providerStep is one screen of the /provider wizard.
type providerStep int

const (
	pvList providerStep = iota
	pvConfirmDelete
	pvID
	pvFormat
	pvEndpoint
	pvValidating
	pvModels
	pvManualModels
	pvTiers
	pvConfirm
	pvWriting
	pvDone
)

// providerAction tells the TUI what side effect a key press requested.
type providerAction int

const (
	pvActNone providerAction = iota
	pvActClose
	pvActValidate
	pvActApply
	pvActDelete
)

// providerTiers are the rows of the tier screen.
var providerTiers = []string{"lite", "pro", "max"}

// providerWizard is the pure state machine behind /provider (spec §5.1–5.4).
// It performs no I/O: handleKey returns an action and the TUI runs the
// matching tea.Cmd, feeding results back through onModels / onApplied /
// onDeleted. The typed API key lives only in apiKey.
type providerWizard struct {
	step         providerStep
	list         []commands.ProviderInfo
	warnings     []string
	mainProvider string
	writable     error
	// gen increases whenever a request starts or is abandoned.
	gen int

	listCursor int // 0..len(list); len(list) is "+ Add provider…"
	editing    *commands.ProviderInfo
	deleting   string

	id         []rune
	formatIdx  int
	baseURL    []rune
	apiKey     []rune
	noKey      bool
	focus      int // 0 base URL, 1 API key, 2 no-key toggle
	manual     []rune
	fromManual bool
	errMsg     string

	modelChooser

	tierCursor int
	// tierChoice[i] indexes checkOrder for providerTiers[i]; -1 = keep.
	tierChoice []int

	report  commands.ProviderReport
	deleted bool
}

func newProviderWizard(list []commands.ProviderInfo, warnings []string, mainProvider string, writable error) *providerWizard {
	return &providerWizard{list: list, warnings: warnings, mainProvider: mainProvider, writable: writable, modelChooser: newModelChooser(nil, nil)}
}

func (w *providerWizard) format() string { return setupFormats[w.formatIdx].id }

func (w *providerWizard) isNew() bool { return w.editing == nil }

func (w *providerWizard) providerID() string {
	if w.editing != nil {
		return w.editing.ID
	}
	return strings.TrimSpace(string(w.id))
}

// editsMain reports that the provider being edited is the main loop's.
func (w *providerWizard) editsMain() bool {
	return w.editing != nil && w.editing.ID == w.mainProvider
}

// keyRequired reports that the API key field may not be left empty.
func (w *providerWizard) keyRequired() bool {
	return !w.noKey && (w.isNew() || w.editing.KeySource == modeltier.KeySourceNone)
}

// input returns the endpoint as entered; text fields are trimmed.
func (w *providerWizard) input() commands.ProviderInput {
	in := commands.ProviderInput{
		ID:      w.providerID(),
		Format:  w.format(),
		BaseURL: strings.TrimSpace(string(w.baseURL)),
		NoKey:   w.noKey,
	}
	if !w.noKey {
		in.APIKey = strings.TrimSpace(string(w.apiKey))
	}
	return in
}

// plan returns what Apply should write. Models is never nil.
func (w *providerWizard) plan() commands.ProviderPlan {
	p := commands.ProviderPlan{Input: w.input(), IsNew: w.isNew(), Models: []commands.SetupModel{}, Tiers: map[string]string{}}
	for _, id := range w.checkOrder {
		m := commands.SetupModel{ID: id}
		for _, listed := range w.models {
			if listed.ID == id {
				m = listed
				break
			}
		}
		p.Models = append(p.Models, m)
	}
	for i, tier := range providerTiers {
		if i < len(w.tierChoice) && w.tierChoice[i] >= 0 {
			p.Tiers[tier] = w.checkOrder[w.tierChoice[i]]
		}
	}
	return p
}

func (w *providerWizard) deleteID() string { return w.deleting }

// handleKey advances the wizard. busy reports a query in progress.
func (w *providerWizard) handleKey(key tea.KeyPressMsg, busy bool) providerAction {
	switch w.step {
	case pvList:
		return w.keyList(key)
	case pvConfirmDelete:
		switch key.Code {
		case tea.KeyEnter:
			w.errMsg = ""
			w.gen++
			w.step = pvWriting
			return pvActDelete
		case tea.KeyEsc:
			w.step = pvList
		}
	case pvID:
		w.keyID(key)
	case pvFormat:
		w.keyFormat(key)
	case pvEndpoint:
		return w.keyEndpoint(key)
	case pvValidating:
		if key.Code == tea.KeyEsc {
			w.gen++
			w.step = pvEndpoint
		}
	case pvModels:
		switch w.modelChooser.handleKey(key) {
		case chooserBack:
			w.step = pvEndpoint
		case chooserDone:
			w.fromManual = false
			w.enterTiers()
		}
	case pvManualModels:
		w.keyManual(key)
	case pvTiers:
		w.keyTiers(key)
	case pvConfirm:
		return w.keyConfirm(key, busy)
	case pvWriting:
	case pvDone:
		return pvActClose
	}
	return pvActNone
}

func (w *providerWizard) keyList(key tea.KeyPressMsg) providerAction {
	w.errMsg = ""
	switch {
	case key.Code == tea.KeyUp:
		if w.listCursor > 0 {
			w.listCursor--
		}
	case key.Code == tea.KeyDown:
		if w.listCursor < len(w.list) {
			w.listCursor++
		}
	case key.Code == tea.KeyEsc:
		return pvActClose
	case key.Code == tea.KeyEnter:
		if w.writable != nil {
			w.errMsg = w.writable.Error()
			return pvActNone
		}
		if w.listCursor == len(w.list) {
			w.editing, w.id = nil, nil
			w.step = pvID
			return pvActNone
		}
		info := w.list[w.listCursor]
		w.editing = &info
		w.formatIdx = 0
		for i, f := range setupFormats {
			if f.id == info.Format {
				w.formatIdx = i
			}
		}
		w.step = pvFormat
	case key.Text == "d" && w.listCursor < len(w.list):
		if w.writable != nil {
			w.errMsg = w.writable.Error()
			return pvActNone
		}
		id := w.list[w.listCursor].ID
		if id == w.mainProvider {
			w.errMsg = "The main loop is using " + id + "; switch away with /model first."
			return pvActNone
		}
		w.deleting = id
		w.step = pvConfirmDelete
	}
	return pvActNone
}

func (w *providerWizard) keyID(key tea.KeyPressMsg) {
	switch key.Code {
	case tea.KeyBackspace:
		w.id = dropLast(w.id)
	case tea.KeyEsc:
		w.errMsg = ""
		w.step = pvList
	case tea.KeyEnter:
		id := strings.TrimSpace(string(w.id))
		if err := modeltier.ValidProviderID(id); err != nil {
			w.errMsg = strings.TrimPrefix(err.Error(), "modeltier: ")
			return
		}
		if slices.ContainsFunc(w.list, func(p commands.ProviderInfo) bool { return p.ID == id }) {
			w.errMsg = "Provider " + id + " already exists."
			return
		}
		w.errMsg = ""
		w.step = pvFormat
	default:
		w.id = append(w.id, []rune(key.Text)...)
	}
}

func (w *providerWizard) keyFormat(key tea.KeyPressMsg) {
	switch key.Code {
	case tea.KeyUp, tea.KeyDown, tea.KeyTab:
		w.formatIdx = (w.formatIdx + 1) % len(setupFormats)
	case tea.KeyEsc:
		w.errMsg = ""
		if w.isNew() {
			w.step = pvID
		} else {
			w.step = pvList
		}
	case tea.KeyEnter:
		w.baseURL = []rune(setupFormats[w.formatIdx].baseURL)
		if w.editing != nil && w.editing.Format == w.format() {
			w.baseURL = []rune(w.editing.BaseURL)
		}
		w.apiKey, w.focus, w.errMsg = nil, 0, ""
		w.noKey = w.editing != nil && w.editing.KeySource == modeltier.KeySourceNone
		w.step = pvEndpoint
	}
}

func (w *providerWizard) keyEndpoint(key tea.KeyPressMsg) providerAction {
	switch key.Code {
	case tea.KeyTab, tea.KeyDown:
		w.focus = (w.focus + 1) % 3
	case tea.KeyUp:
		w.focus = (w.focus + 2) % 3
	case tea.KeyEsc:
		w.errMsg = ""
		w.step = pvFormat
	case tea.KeyEnter:
		in := w.input()
		switch {
		case in.BaseURL == "":
			w.errMsg = "Base URL is required."
		case in.APIKey == "" && w.keyRequired():
			w.errMsg = "API key is required (or mark it as not needed)."
		default:
			w.errMsg = ""
			w.gen++
			w.step = pvValidating
			return pvActValidate
		}
	default:
		switch w.focus {
		case 0:
			w.editField(&w.baseURL, key)
		case 1:
			w.editField(&w.apiKey, key)
		case 2:
			if key.Code == tea.KeySpace {
				w.noKey = !w.noKey
			}
		}
	}
	return pvActNone
}

func (w *providerWizard) editField(f *[]rune, key tea.KeyPressMsg) {
	if key.Code == tea.KeyBackspace {
		*f = dropLast(*f)
		return
	}
	*f = append(*f, []rune(key.Text)...)
}

// onModels receives the ListModels result for the current request.
func (w *providerWizard) onModels(models []commands.SetupModel, err error) {
	if w.step != pvValidating {
		return
	}
	var pre []string
	if w.editing != nil {
		pre = w.editing.Models
	}
	switch {
	case errors.Is(err, commands.ErrModelListUnsupported):
		w.errMsg = ""
		w.manual = []rune(strings.Join(pre, ", "))
		w.step = pvManualModels
	case err != nil:
		w.errMsg = err.Error()
		w.step = pvEndpoint
	default:
		w.modelChooser = newModelChooser(models, pre)
		w.errMsg = ""
		w.step = pvModels
	}
}

func (w *providerWizard) keyManual(key tea.KeyPressMsg) {
	switch key.Code {
	case tea.KeyBackspace:
		w.manual = dropLast(w.manual)
	case tea.KeyEsc:
		w.step = pvEndpoint
	case tea.KeyEnter:
		var ids []string
		for _, id := range strings.Split(string(w.manual), ",") {
			if id = strings.TrimSpace(id); id != "" && !slices.Contains(ids, id) {
				ids = append(ids, id)
			}
		}
		if len(ids) == 0 {
			w.errMsg = "Enter at least one model ID."
			return
		}
		models := make([]commands.SetupModel, len(ids))
		for i, id := range ids {
			models[i] = commands.SetupModel{ID: id}
		}
		w.modelChooser = newModelChooser(models, ids)
		w.errMsg = ""
		w.fromManual = true
		w.enterTiers()
	default:
		w.manual = append(w.manual, []rune(key.Text)...)
	}
}

func (w *providerWizard) enterTiers() {
	w.tierChoice = []int{-1, -1, -1}
	w.tierCursor = 0
	w.step = pvTiers
}

func (w *providerWizard) keyTiers(key tea.KeyPressMsg) {
	switch key.Code {
	case tea.KeyUp:
		w.tierCursor = (w.tierCursor + len(providerTiers) - 1) % len(providerTiers)
	case tea.KeyDown, tea.KeyTab:
		w.tierCursor = (w.tierCursor + 1) % len(providerTiers)
	case tea.KeyLeft, tea.KeyRight:
		n := len(w.checkOrder) + 1 // "keep" + each checked model
		pos := w.tierChoice[w.tierCursor] + 1
		if key.Code == tea.KeyRight {
			pos = (pos + 1) % n
		} else {
			pos = (pos + n - 1) % n
		}
		w.tierChoice[w.tierCursor] = pos - 1
	case tea.KeyEsc:
		if w.fromManual {
			w.step = pvManualModels
		} else {
			w.step = pvModels
		}
	case tea.KeyEnter:
		w.errMsg = ""
		w.step = pvConfirm
	}
}

func (w *providerWizard) keyConfirm(key tea.KeyPressMsg, busy bool) providerAction {
	switch key.Code {
	case tea.KeyEsc:
		w.errMsg = ""
		w.step = pvTiers
	case tea.KeyEnter:
		if busy && w.editsMain() {
			w.errMsg = "Cannot change the provider the main loop is using while a query is running."
			return pvActNone
		}
		w.errMsg = ""
		w.gen++
		w.step = pvWriting
		return pvActApply
	}
	return pvActNone
}

// handlePaste inserts pasted text (newlines dropped) into the focused field.
func (w *providerWizard) handlePaste(text string) {
	text = strings.Map(func(r rune) rune {
		if r == '\n' || r == '\r' {
			return -1
		}
		return r
	}, text)
	switch w.step {
	case pvID:
		w.id = append(w.id, []rune(text)...)
	case pvEndpoint:
		switch w.focus {
		case 0:
			w.baseURL = append(w.baseURL, []rune(text)...)
		case 1:
			w.apiKey = append(w.apiKey, []rune(text)...)
		}
	case pvManualModels:
		w.manual = append(w.manual, []rune(text)...)
	case pvModels:
		w.paste(text)
	}
}

// onApplied receives the Apply result.
func (w *providerWizard) onApplied(r commands.ProviderReport, err error) {
	if w.step != pvWriting {
		return
	}
	if err != nil {
		w.errMsg = err.Error()
		w.step = pvConfirm
		return
	}
	w.report, w.errMsg = r, ""
	w.step = pvDone
}

// onDeleted receives the Delete result.
func (w *providerWizard) onDeleted(r commands.ProviderReport, err error) {
	if w.step != pvWriting {
		return
	}
	if err != nil {
		w.errMsg = err.Error()
		w.step = pvList
		return
	}
	w.report, w.errMsg, w.deleted = r, "", true
	w.step = pvDone
}
