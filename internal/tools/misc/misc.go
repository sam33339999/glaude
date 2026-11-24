package misc

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/tunsuy/glaude/internal/tools"

	"github.com/tunsuy/glaude/internal/textutil"
)

// ── SkillTool ─────────────────────────────────────────────────────────────────

// SkillInput is the input schema for SkillTool.
type SkillInput struct {
	// Skill is the skill name or slash-command path (required).
	Skill string `json:"skill"`
	// Args is optional arguments to pass to the skill.
	Args string `json:"args,omitempty"`
}

// NewSkillTool constructs the Skill tool (BF-13). dirsFn supplies the skill
// search directories live (user ~/.glaude/skills + project .glaude/skills,
// project wins on name collision); wired by bootstrap to home + the session
// WorkingDir so worktree switches are respected.
//
// Divergence (documented in tools.md): the oracle runs skill prompts in an
// isolated sub-agent with its own token budget; this implementation loads the
// skill instructions into the current session context for the agent to follow.
func NewSkillTool(dirsFn func() []string) tools.Tool {
	return &skillTool{dirsFn: dirsFn}
}

type skillTool struct {
	tools.BaseTool
	dirsFn func() []string
}

func (t *skillTool) Name() string { return "Skill" }

func (t *skillTool) Description(_ tools.Input, _ tools.PermissionContext) string {
	return `Invokes a named skill (slash command) within the current session.`
}

func (t *skillTool) InputSchema() tools.InputSchema {
	return tools.NewInputSchema(
		map[string]json.RawMessage{
			"skill": tools.PropSchema(map[string]any{
				"type":        "string",
				"description": "The skill name or slash-command path to invoke",
			}),
			"args": tools.PropSchema(map[string]any{
				"type":        "string",
				"description": "Optional arguments to pass to the skill",
			}),
		},
		[]string{"skill"},
	)
}

func (t *skillTool) IsConcurrencySafe(_ tools.Input) bool { return false }
func (t *skillTool) IsReadOnly(_ tools.Input) bool        { return false }

func (t *skillTool) UserFacingName(input tools.Input) string {
	var in SkillInput
	if json.Unmarshal(input, &in) == nil && in.Skill != "" {
		return fmt.Sprintf("Skill(%s)", in.Skill)
	}
	return "Skill"
}

// skillMaxContentChars caps loaded skill content (window-small principle).
const skillMaxContentChars = 100_000

func (t *skillTool) Call(input tools.Input, _ *tools.UseContext, _ tools.OnProgressFn) (*tools.Result, error) {
	var in SkillInput
	if err := json.Unmarshal(input, &in); err != nil {
		return &tools.Result{IsError: true, Content: fmt.Sprintf("Skill: invalid input: %v", err)}, nil
	}
	name := strings.TrimSpace(in.Skill)
	if name == "" {
		return &tools.Result{IsError: true, Content: "Skill: skill name is required"}, nil
	}
	if strings.ContainsAny(name, "/\\") || strings.Contains(name, "..") {
		return &tools.Result{IsError: true, Content: fmt.Sprintf("Skill: invalid skill name %q (path traversal rejected)", name)}, nil
	}
	if t.dirsFn == nil {
		return &tools.Result{IsError: true, Content: "Skill: no skill directories configured"}, nil
	}

	content, found := loadSkillContent(t.dirsFn(), name)
	if !found {
		available := listSkillNames(t.dirsFn())
		msg := fmt.Sprintf("Skill %q not found.", name)
		if len(available) > 0 {
			msg += " Available skills: " + strings.Join(available, ", ")
		} else {
			msg += " No skills are installed (add markdown skills under ~/.glaude/skills or .glaude/skills)."
		}
		return &tools.Result{IsError: true, Content: msg}, nil
	}
	if len(content) > skillMaxContentChars {
		content = textutil.TruncateBytes(content, skillMaxContentChars) + "\n…(truncated)"
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "Skill %q loaded. Follow these instructions:\n\n%s", name, content)
	if in.Args != "" {
		fmt.Fprintf(&sb, "\n\nArguments: %s", in.Args)
	}
	return &tools.Result{Content: sb.String()}, nil
}

// loadSkillContent searches dirs for <name>/SKILL.md or <name>.md (first
// match wins; dirs are ordered project-first by the caller) and returns the
// body with any YAML frontmatter stripped.
func loadSkillContent(dirs []string, name string) (string, bool) {
	for _, dir := range dirs {
		for _, candidate := range []string{
			filepath.Join(dir, name, "SKILL.md"),
			filepath.Join(dir, name+".md"),
		} {
			raw, err := os.ReadFile(candidate)
			if err != nil {
				continue
			}
			return stripFrontmatter(string(raw)), true
		}
	}
	return "", false
}

// stripFrontmatter removes a leading YAML frontmatter block (--- ... ---).
func stripFrontmatter(s string) string {
	if !strings.HasPrefix(s, "---\n") && !strings.HasPrefix(s, "---\r\n") {
		return strings.TrimSpace(s)
	}
	rest := s[4:]
	if i := strings.Index(rest, "\n---"); i >= 0 {
		after := rest[i+4:]
		if j := strings.IndexByte(after, '\n'); j >= 0 {
			return strings.TrimSpace(after[j+1:])
		}
	}
	return strings.TrimSpace(s)
}

// listSkillNames enumerates installed skill names across dirs (deduplicated,
// sorted) for the not-found hint.
func listSkillNames(dirs []string) []string {
	seen := make(map[string]bool)
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() {
				if _, err := os.Stat(filepath.Join(dir, e.Name(), "SKILL.md")); err == nil {
					seen[e.Name()] = true
				}
			} else if strings.HasSuffix(e.Name(), ".md") {
				seen[strings.TrimSuffix(e.Name(), ".md")] = true
			}
		}
	}
	names := make([]string, 0, len(seen))
	for n := range seen {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// ── BriefTool ─────────────────────────────────────────────────────────────────

// BriefInput is the input schema for BriefTool.
type BriefInput struct {
	// Content is the text to be briefed/summarised.
	Content string `json:"content"`
}

// BriefTool is the exported singleton instance (BF-14: extractive summariser).
// Divergence (documented in tools.md): the oracle's legacy "Brief" tool is
// SendUserMessage (Kairos assistant-mode, feature-gated); this repo's design
// spec defines Brief as a content summariser. Implemented extractively —
// deterministic, no extra LLM call (cache-first gate Q3).
var BriefTool tools.Tool = &briefTool{}

type briefTool struct{ tools.BaseTool }

func (t *briefTool) Name() string { return "Brief" }

func (t *briefTool) Description(_ tools.Input, _ tools.PermissionContext) string {
	return `Generates a concise summary (brief) of the provided content.`
}

func (t *briefTool) InputSchema() tools.InputSchema {
	return tools.NewInputSchema(
		map[string]json.RawMessage{
			"content": tools.PropSchema(map[string]any{
				"type":        "string",
				"description": "The content to summarise",
			}),
		},
		[]string{"content"},
	)
}

func (t *briefTool) IsConcurrencySafe(_ tools.Input) bool { return true }
func (t *briefTool) IsReadOnly(_ tools.Input) bool        { return true }
func (t *briefTool) UserFacingName(_ tools.Input) string  { return "Brief" }

// briefMaxInputChars bounds processing so a huge paste cannot stall the loop.
const briefMaxInputChars = 200_000

func (t *briefTool) Call(input tools.Input, _ *tools.UseContext, _ tools.OnProgressFn) (*tools.Result, error) {
	var in BriefInput
	if err := json.Unmarshal(input, &in); err != nil {
		return &tools.Result{IsError: true, Content: fmt.Sprintf("Brief: invalid input: %v", err)}, nil
	}
	content := strings.TrimSpace(in.Content)
	if content == "" {
		return &tools.Result{IsError: true, Content: "Brief: content is required"}, nil
	}
	if len(content) > briefMaxInputChars {
		content = textutil.TruncateBytes(content, briefMaxInputChars)
	}
	brief := extractiveBrief(content)
	return &tools.Result{Content: brief}, nil
}

// extractiveBrief produces a deterministic extractive summary: sentences are
// scored by term frequency (stopwords excluded) plus position bonuses for the
// opening and closing sentences; the top budget sentences are emitted in
// original order with a compression header. No LLM call is made.
func extractiveBrief(content string) string {
	sentences := splitSentences(content)
	words := strings.Fields(content)
	if len(sentences) <= 3 {
		return fmt.Sprintf("Brief (%d words, kept in full):\n\n%s", len(words), content)
	}

	freq := make(map[string]int)
	for _, w := range words {
		w = strings.ToLower(strings.Trim(w, ".,;:!?\"'()[]{}"))
		if len(w) < 4 || briefStopwords[w] {
			continue
		}
		freq[w]++
	}

	type scoredSentence struct {
		text  string
		score float64
		idx   int
	}
	scored := make([]scoredSentence, 0, len(sentences))
	for i, s := range sentences {
		var score float64
		for _, w := range strings.Fields(s) {
			w = strings.ToLower(strings.Trim(w, ".,;:!?\"'()[]{}"))
			if f, ok := freq[w]; ok {
				score += float64(f)
			}
		}
		// Normalise by length so long sentences don't dominate by sheer size.
		if n := len(strings.Fields(s)); n > 0 {
			score /= float64(n)
		}
		if i == 0 {
			score += 2 // opening sentence carries the topic
		}
		if i == len(sentences)-1 {
			score++ // closing sentence carries the conclusion
		}
		scored = append(scored, scoredSentence{text: s, score: score, idx: i})
	}

	budget := len(sentences) / 4
	if budget < 3 {
		budget = 3
	}
	if budget > 8 {
		budget = 8
	}
	sort.Slice(scored, func(i, j int) bool {
		if scored[i].score != scored[j].score {
			return scored[i].score > scored[j].score
		}
		return scored[i].idx < scored[j].idx
	})
	kept := scored[:budget]
	sort.Slice(kept, func(i, j int) bool { return kept[i].idx < kept[j].idx })

	var sb strings.Builder
	var keptWords int
	fmt.Fprintf(&sb, "Brief (%d words → ", len(words))
	for i, k := range kept {
		if i > 0 {
			sb.WriteString(" ")
		}
		sb.WriteString(k.text)
		keptWords += len(strings.Fields(k.text))
	}
	header := fmt.Sprintf("Brief (%d words → %d words, %d/%d sentences):\n\n", len(words), keptWords, len(kept), len(sentences))
	return header + sb.String()
}

// splitSentences splits text on sentence-ending punctuation followed by
// whitespace (keeping the punctuation), falling back to paragraph breaks.
func splitSentences(content string) []string {
	var sentences []string
	var cur strings.Builder
	runes := []rune(content)
	for i, r := range runes {
		cur.WriteRune(r)
		if r == '.' || r == '!' || r == '?' {
			if i+1 >= len(runes) || runes[i+1] == ' ' || runes[i+1] == '\n' || runes[i+1] == '\t' {
				s := strings.TrimSpace(cur.String())
				if s != "" {
					sentences = append(sentences, s)
				}
				cur.Reset()
			}
		}
	}
	if s := strings.TrimSpace(cur.String()); s != "" {
		sentences = append(sentences, s)
	}
	if len(sentences) == 0 {
		sentences = []string{content}
	}
	return sentences
}

// briefStopwords are common words excluded from term-frequency scoring.
var briefStopwords = map[string]bool{
	"this": true, "that": true, "with": true, "from": true, "have": true,
	"been": true, "were": true, "will": true, "would": true, "could": true,
	"should": true, "their": true, "there": true, "which": true, "about": true,
	"into": true, "than": true, "then": true, "them": true, "these": true,
	"those": true, "such": true, "some": true, "more": true, "most": true,
	"also": true, "very": true, "when": true, "what": true, "where": true,
	"while": true, "because": true, "before": true, "after": true, "above": true,
	"below": true, "between": true, "through": true, "during": true, "each": true,
	"other": true, "only": true, "over": true, "same": true, "just": true,
}

// ── ToolSearch ────────────────────────────────────────────────────────────────

// ToolSearchInput is the input schema for ToolSearch.
type ToolSearchInput struct {
	// Query is the natural-language search query (required).
	Query string `json:"query"`
}

// NewToolSearchTool constructs the ToolSearch tool backed by the live tool
// registry (BF-12). Keyword scoring over tool names, SearchHint() and
// descriptions; no external index needed. Registered by RegisterBuiltinTools
// with the registry itself (self-reference is safe: search runs at call time).
func NewToolSearchTool(reg *tools.Registry) tools.Tool {
	return &toolSearchTool{reg: reg}
}

type toolSearchTool struct {
	tools.BaseTool
	reg *tools.Registry
}

func (t *toolSearchTool) Name() string { return "ToolSearch" }

func (t *toolSearchTool) Description(_ tools.Input, _ tools.PermissionContext) string {
	return `Searches the tool registry for tools matching a natural-language query.`
}

func (t *toolSearchTool) InputSchema() tools.InputSchema {
	return tools.NewInputSchema(
		map[string]json.RawMessage{
			"query": tools.PropSchema(map[string]any{
				"type":        "string",
				"description": "Natural-language description of the desired tool",
			}),
		},
		[]string{"query"},
	)
}

func (t *toolSearchTool) IsConcurrencySafe(_ tools.Input) bool { return true }
func (t *toolSearchTool) IsReadOnly(_ tools.Input) bool        { return true }

func (t *toolSearchTool) UserFacingName(input tools.Input) string {
	var in ToolSearchInput
	if json.Unmarshal(input, &in) == nil && in.Query != "" {
		return fmt.Sprintf("ToolSearch(%s)", in.Query)
	}
	return "ToolSearch"
}

// toolSearchMaxResults caps the returned matches (window-small principle).
const toolSearchMaxResults = 10

func (t *toolSearchTool) Call(input tools.Input, _ *tools.UseContext, _ tools.OnProgressFn) (*tools.Result, error) {
	if t.reg == nil {
		return &tools.Result{IsError: true, Content: "ToolSearch: no tool registry configured"}, nil
	}
	var in ToolSearchInput
	if err := json.Unmarshal(input, &in); err != nil {
		return &tools.Result{IsError: true, Content: fmt.Sprintf("ToolSearch: invalid input: %v", err)}, nil
	}
	tokens := searchTokens(in.Query)
	if len(tokens) == 0 {
		return &tools.Result{IsError: true, Content: "ToolSearch: query is required"}, nil
	}

	type scored struct {
		name  string
		desc  string
		score int
	}
	var matches []scored
	for _, tool := range t.reg.All() {
		name := tool.Name()
		nameTokens := searchTokens(name)
		hintTokens := searchTokens(tool.SearchHint())
		desc := strings.ToLower(tool.Description(tools.Input("{}"), nil))
		score := 0
		for _, q := range tokens {
			switch {
			case containsToken(nameTokens, q):
				score += 3
			case containsToken(hintTokens, q):
				score += 2
			case strings.Contains(desc, q):
				score++
			}
		}
		if score > 0 {
			matches = append(matches, scored{name: name, desc: firstLine(tool.Description(tools.Input("{}"), nil)), score: score})
		}
	}
	if len(matches) == 0 {
		return &tools.Result{Content: fmt.Sprintf("No tools match query %q. Use the tool list in your system prompt for available tools.", in.Query)}, nil
	}
	sort.Slice(matches, func(i, j int) bool {
		if matches[i].score != matches[j].score {
			return matches[i].score > matches[j].score
		}
		return matches[i].name < matches[j].name
	})
	if len(matches) > toolSearchMaxResults {
		matches = matches[:toolSearchMaxResults]
	}
	type entry struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	entries := make([]entry, 0, len(matches))
	for _, m := range matches {
		entries = append(entries, entry{Name: m.name, Description: m.desc})
	}
	raw, err := json.Marshal(entries)
	if err != nil {
		return nil, fmt.Errorf("ToolSearch: marshal results: %w", err)
	}
	return &tools.Result{Content: string(raw)}, nil
}

// searchTokens lowercases s and splits on non-alphanumeric runs. CamelCase
// names are split at case boundaries first so "FileRead" yields file+read.
func searchTokens(s string) []string {
	var spaced strings.Builder
	runes := []rune(s)
	for i, r := range runes {
		if i > 0 && unicode.IsUpper(r) && (unicode.IsLower(runes[i-1]) || (i+1 < len(runes) && unicode.IsLower(runes[i+1]))) {
			spaced.WriteRune(' ')
		}
		spaced.WriteRune(r)
	}
	var tokens []string
	for _, f := range strings.FieldsFunc(strings.ToLower(spaced.String()), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	}) {
		if f != "" {
			tokens = append(tokens, f)
		}
	}
	return tokens
}

// containsToken reports whether tok appears in tokens (exact or prefix match,
// so "read" matches "readfile" fragments like "read").
func containsToken(tokens []string, tok string) bool {
	for _, t := range tokens {
		if t == tok || strings.HasPrefix(t, tok) {
			return true
		}
	}
	return false
}

// firstLine returns the first line of s, truncated to 160 runes.
func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	r := []rune(s)
	if len(r) > 160 {
		return string(r[:160]) + "…"
	}
	return s
}

// ── SleepTool ─────────────────────────────────────────────────────────────────

// maxSleepMilliseconds caps a single Sleep call at 24 hours. Requests above
// the cap are rejected honestly. Besides being a sane product bound, the cap
// keeps the time.Duration conversion far from the int64 overflow boundary
// (~9.2e12 ms ≈ 292 years), where Milliseconds*time.Millisecond would wrap
// negative and make the timer fire immediately, falsely reporting success
// (CR-BF01-5). The cap is enforced at runtime only; the provider-facing schema
// is intentionally left byte-stable (no prefix move).
const maxSleepMilliseconds = 24 * 60 * 60 * 1000

// SleepInput is the input schema for SleepTool.
type SleepInput struct {
	// Milliseconds is the duration to sleep in milliseconds (required).
	// Must be in [0, maxSleepMilliseconds].
	Milliseconds int `json:"milliseconds"`
}

// SleepTool is the exported singleton instance.
var SleepTool tools.Tool = &sleepTool{}

type sleepTool struct{ tools.BaseTool }

func (t *sleepTool) Name() string { return "Sleep" }

func (t *sleepTool) Description(_ tools.Input, _ tools.PermissionContext) string {
	return `Pauses execution for the specified number of milliseconds.`
}

func (t *sleepTool) InputSchema() tools.InputSchema {
	return tools.NewInputSchema(
		map[string]json.RawMessage{
			"milliseconds": tools.PropSchema(map[string]any{
				"type":        "integer",
				"description": "Number of milliseconds to sleep",
				"minimum":     0,
			}),
		},
		[]string{"milliseconds"},
	)
}

func (t *sleepTool) IsConcurrencySafe(_ tools.Input) bool { return true }
func (t *sleepTool) IsReadOnly(_ tools.Input) bool        { return true }

func (t *sleepTool) UserFacingName(input tools.Input) string {
	var in SleepInput
	if json.Unmarshal(input, &in) == nil {
		return fmt.Sprintf("Sleep(%dms)", in.Milliseconds)
	}
	return "Sleep"
}

func (t *sleepTool) Call(input tools.Input, uctx *tools.UseContext, _ tools.OnProgressFn) (*tools.Result, error) {
	// Validate presence first: encoding/json leaves absent fields at zero,
	// which would otherwise be indistinguishable from an explicit 0ms sleep.
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(input, &raw); err != nil {
		return &tools.Result{IsError: true, Content: fmt.Sprintf("Sleep: invalid input: %v", err)}, nil
	}
	if _, ok := raw["milliseconds"]; !ok {
		return &tools.Result{IsError: true, Content: "Sleep: missing required field 'milliseconds'"}, nil
	}
	var in SleepInput
	if err := json.Unmarshal(input, &in); err != nil {
		return &tools.Result{IsError: true, Content: fmt.Sprintf("Sleep: invalid input: %v", err)}, nil
	}
	if in.Milliseconds < 0 {
		return &tools.Result{IsError: true, Content: fmt.Sprintf("Sleep: milliseconds must be >= 0, got %d", in.Milliseconds)}, nil
	}
	if in.Milliseconds > maxSleepMilliseconds {
		return &tools.Result{IsError: true, Content: fmt.Sprintf("Sleep: milliseconds must be <= %d (24h), got %d", maxSleepMilliseconds, in.Milliseconds)}, nil
	}

	ctx := context.Background()
	var abortCh <-chan struct{}
	if uctx != nil {
		if uctx.Ctx != nil {
			ctx = uctx.Ctx
		}
		abortCh = uctx.AbortCh
	}

	timer := time.NewTimer(time.Duration(in.Milliseconds) * time.Millisecond)
	defer timer.Stop()

	select {
	case <-timer.C:
		return &tools.Result{Content: fmt.Sprintf("Slept for %d milliseconds", in.Milliseconds)}, nil
	case <-ctx.Done():
		return &tools.Result{IsError: true, Content: fmt.Sprintf("Sleep: cancelled before completing %dms: %v", in.Milliseconds, ctx.Err())}, nil
	case <-abortCh:
		return &tools.Result{IsError: true, Content: fmt.Sprintf("Sleep: aborted before completing %dms", in.Milliseconds)}, nil
	}
}

// ── SyntheticOutput ───────────────────────────────────────────────────────────

// SyntheticOutputInput is the input schema for SyntheticOutput.
type SyntheticOutputInput struct {
	// Content is the synthetic content to inject as a tool result.
	Content string `json:"content"`
	// IsError marks the output as an error result.
	IsError bool `json:"is_error,omitempty"`
}

// SyntheticOutputTool is the exported singleton instance (BF-15).
// Oracle parity (SyntheticOutputTool.ts): the tool validates its input and
// returns it as the output — no real tool execution. Here the input content
// becomes the tool result itself (the synthetic injection), with is_error
// propagated. The oracle's SDK structured_output variant is a documented
// divergence (this repo's schema is {content, is_error}).
var SyntheticOutputTool tools.Tool = &syntheticOutputTool{}

type syntheticOutputTool struct{ tools.BaseTool }

func (t *syntheticOutputTool) Name() string { return "SyntheticOutput" }

func (t *syntheticOutputTool) Description(_ tools.Input, _ tools.PermissionContext) string {
	return `Injects a synthetic tool result into the conversation without executing a real tool call.`
}

func (t *syntheticOutputTool) InputSchema() tools.InputSchema {
	return tools.NewInputSchema(
		map[string]json.RawMessage{
			"content": tools.PropSchema(map[string]any{
				"type":        "string",
				"description": "The content to inject as a tool result",
			}),
			"is_error": tools.PropSchema(map[string]any{
				"type":        "boolean",
				"description": "If true, marks the synthetic result as an error",
			}),
		},
		[]string{"content"},
	)
}

func (t *syntheticOutputTool) IsConcurrencySafe(_ tools.Input) bool { return false }
func (t *syntheticOutputTool) IsReadOnly(_ tools.Input) bool        { return true }
func (t *syntheticOutputTool) UserFacingName(_ tools.Input) string  { return "SyntheticOutput" }

func (t *syntheticOutputTool) Call(input tools.Input, _ *tools.UseContext, _ tools.OnProgressFn) (*tools.Result, error) {
	var in SyntheticOutputInput
	if err := json.Unmarshal(input, &in); err != nil {
		return &tools.Result{IsError: true, Content: fmt.Sprintf("SyntheticOutput: invalid input: %v", err)}, nil
	}
	// The tool result IS the synthetic injection: echo content verbatim and
	// propagate the error flag. No side effects, no real tool execution.
	return &tools.Result{Content: in.Content, IsError: in.IsError}, nil
}
