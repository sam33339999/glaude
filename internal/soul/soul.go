// Package soul stores the user's "soul": standing identity and rules for the
// agent (persona, tone, hard constraints). It lives in files, not in the
// conversation, and is put into the system prompt of every request — the main
// loop and every sub-agent — so auto-compaction, which only rewrites
// conversation messages, can never drop or dilute it.
//
// Two scopes, both applied when present (global first, project last so the
// more specific one has the final word):
//
//	~/.glaude/SOUL.md        global
//	<project>/.glaude/SOUL.md project
//
// Stdlib only.
package soul

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/tunsuy/glaude/internal/textutil"
)

// FileName is the soul file name inside a .glaude directory.
const FileName = "SOUL.md"

// MaxBytes caps one soul file: it is sent with every request.
const MaxBytes = 16 * 1024

// GlobalPath is the user-wide soul file.
func GlobalPath(home string) string { return filepath.Join(home, ".glaude", FileName) }

// ProjectPath is the project soul file.
func ProjectPath(projectDir string) string { return filepath.Join(projectDir, ".glaude", FileName) }

// Source is one soul file that exists.
type Source struct {
	Scope string // "global" | "project"
	Path  string
	Text  string
}

// Sources returns the soul files that exist and are non-empty, global first.
// Unreadable files are skipped.
func Sources(home, projectDir string) []Source {
	var out []Source
	add := func(scope, path string) {
		if path == "" {
			return
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return
		}
		if t := strings.TrimSpace(string(b)); t != "" {
			if len(t) > MaxBytes {
				t = textutil.TruncateBytes(t, MaxBytes)
			}
			out = append(out, Source{Scope: scope, Path: path, Text: t})
		}
	}
	if home != "" {
		add("global", GlobalPath(home))
	}
	if projectDir != "" && (home == "" || filepath.Clean(ProjectPath(projectDir)) != filepath.Clean(GlobalPath(home))) {
		add("project", ProjectPath(projectDir))
	}
	return out
}

// Prompt renders the system-prompt section for the soul ("" when none).
func Prompt(home, projectDir string) string {
	srcs := Sources(home, projectDir)
	if len(srcs) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("# Soul\n\nThe user defined the following standing identity and rules for you. They apply to every turn and every task, and take priority over default style guidance. If the project soul conflicts with the global soul, follow the project soul.\n")
	for _, s := range srcs {
		fmt.Fprintf(&sb, "\n<soul scope=%q>\n%s\n</soul>\n", s.Scope, s.Text)
	}
	return sb.String()
}

// PromptForCwd is Prompt with the current user's home directory.
func PromptForCwd(projectDir string) string {
	home, _ := os.UserHomeDir()
	return Prompt(home, projectDir)
}

// Save writes text to path atomically (temp file + rename).
func Save(path, text string) error {
	text = strings.TrimSpace(text)
	if text == "" {
		return errors.New("soul is empty")
	}
	if len(text) > MaxBytes {
		return fmt.Errorf("soul is %d bytes; the limit is %d (it is sent with every request)", len(text), MaxBytes)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".soul-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.WriteString(text + "\n"); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// Clear removes the soul file at path. ok is false when there was none.
func Clear(path string) (ok bool, err error) {
	err = os.Remove(path)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	return err == nil, err
}
