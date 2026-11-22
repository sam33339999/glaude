package clipboard

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

const toolHint = "Install wl-clipboard or xclip to paste from the clipboard"

func TestReadImage_DarwinWritesTempFile(t *testing.T) {
	png := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}
	dir := t.TempDir()
	var path string
	r := &Reader{
		GOOS:    "darwin",
		TempDir: dir,
		Run: func(_ context.Context, name string, args ...string) ([]byte, error) {
			path = darwinScriptPath(t, args)
			want := []string{
				"-e", "set d to (the clipboard as «class PNGf»)",
				"-e", `set f to open for access POSIX file "` + path + `" with write permission`,
				"-e", "set eof f to 0",
				"-e", "write d to f",
				"-e", "close access f",
			}
			if name != "osascript" || !slices.Equal(args, want) {
				t.Fatalf("command = %s %q", name, args)
			}
			if err := os.WriteFile(path, png, 0o600); err != nil {
				t.Fatal(err)
			}
			return nil, nil
		},
	}

	got, err := r.ReadImage(context.Background())
	if err != nil {
		t.Fatalf("ReadImage: %v", err)
	}
	if !bytes.Equal(got, png) {
		t.Fatalf("bytes = %q, want PNG magic", got)
	}
	if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
		t.Fatalf("temp file = %v, want removed", statErr)
	}
}

func TestReadImage_DarwinEscapesQuotedTempDir(t *testing.T) {
	parent := t.TempDir()
	dir := filepath.Join(parent, `qu"ote`)
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	var args []string
	var actual string
	r := &Reader{
		GOOS:    "darwin",
		TempDir: dir,
		Run: func(_ context.Context, name string, got ...string) ([]byte, error) {
			if name != "osascript" {
				t.Fatalf("command = %s", name)
			}
			args = slices.Clone(got)
			entries, err := os.ReadDir(dir)
			if err != nil {
				t.Fatal(err)
			}
			if len(entries) != 1 {
				t.Fatalf("entries = %v", entries)
			}
			actual = filepath.Join(dir, entries[0].Name())
			return nil, errors.New("stop")
		},
	}
	_, err := r.ReadImage(context.Background())
	if !errors.Is(err, ErrNoImage) {
		t.Fatalf("err = %v, want ErrNoImage", err)
	}

	script, ok := singlePOSIXScript(args)
	if !ok {
		t.Fatalf("args = %q, want exactly one POSIX file statement", args)
	}
	const prefix = `set f to open for access POSIX file "`
	const suffix = `" with write permission`
	if !strings.HasPrefix(script, prefix) || !strings.HasSuffix(script, suffix) {
		t.Fatalf("script = %q, want a single POSIX open statement", script)
	}
	inner := strings.TrimSuffix(strings.TrimPrefix(script, prefix), suffix)
	gotPath, ok := unescapeAppleScriptString(inner)
	if !ok || gotPath != actual {
		t.Fatalf("escaped path = %q, want %q", inner, actual)
	}
	if !strings.Contains(inner, `qu\"ote`) {
		t.Fatalf("script = %q, want escaped quote", script)
	}
	if _, statErr := os.Stat(actual); !os.IsNotExist(statErr) {
		t.Fatalf("temp file = %v, want removed", statErr)
	}
}

func TestEscapeAppleScriptString(t *testing.T) {
	in := "a\\b\"c\rd\ne"
	got := escapeAppleScriptString(in)
	want := `a\\b\"c\rd\ne`
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	back, ok := unescapeAppleScriptString(got)
	if !ok || back != in {
		t.Fatalf("round trip = %q ok=%v", back, ok)
	}
}

func singlePOSIXScript(args []string) (string, bool) {
	n := 0
	script := ""
	for i := 0; i < len(args); i += 2 {
		if i+1 >= len(args) || args[i] != "-e" {
			return "", false
		}
		n++
		if strings.Contains(args[i+1], "POSIX file") {
			if script != "" {
				return "", false
			}
			script = args[i+1]
		}
	}
	if n != 5 || script == "" {
		return "", false
	}
	return script, true
}

func unescapeAppleScriptString(s string) (string, bool) {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] != '\\' {
			if s[i] == '"' || s[i] == '\n' || s[i] == '\r' {
				return "", false
			}
			b.WriteByte(s[i])
			continue
		}
		if i+1 >= len(s) {
			return "", false
		}
		i++
		switch s[i] {
		case '\\':
			b.WriteByte('\\')
		case '"':
			b.WriteByte('"')
		case 'r':
			b.WriteByte('\r')
		case 'n':
			b.WriteByte('\n')
		default:
			return "", false
		}
	}
	return b.String(), true
}

func TestReadImage_DarwinNoImage(t *testing.T) {
	t.Run("osascript error", func(t *testing.T) {
		dir := t.TempDir()
		var path string
		r := &Reader{
			GOOS:    "darwin",
			TempDir: dir,
			Run: func(_ context.Context, _ string, args ...string) ([]byte, error) {
				path = darwinScriptPath(t, args)
				return nil, errors.New("osascript failed")
			},
		}
		_, err := r.ReadImage(context.Background())
		if !errors.Is(err, ErrNoImage) {
			t.Fatalf("err = %v, want ErrNoImage", err)
		}
		if path == "" {
			t.Fatal("osascript was not invoked")
		}
		if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
			t.Fatalf("temp file = %v, want removed", statErr)
		}
	})

	t.Run("empty file", func(t *testing.T) {
		dir := t.TempDir()
		var path string
		r := &Reader{
			GOOS:    "darwin",
			TempDir: dir,
			Run: func(_ context.Context, _ string, args ...string) ([]byte, error) {
				path = darwinScriptPath(t, args)
				return nil, nil
			},
		}
		_, err := r.ReadImage(context.Background())
		if !errors.Is(err, ErrNoImage) {
			t.Fatalf("err = %v, want ErrNoImage", err)
		}
		if path == "" {
			t.Fatal("osascript was not invoked")
		}
		if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
			t.Fatalf("temp file = %v, want removed", statErr)
		}
	})
}

func TestReadImage_WaylandPicksPNG(t *testing.T) {
	png := []byte{0x89, 'P', 'N', 'G'}
	var calls []runCall
	r := &Reader{
		GOOS:     "linux",
		Getenv:   func(string) string { return "wayland-0" },
		LookPath: lookPathOK("wl-paste"),
		Run: func(ctx context.Context, name string, args ...string) ([]byte, error) {
			calls = append(calls, runCall{ctx, name, slices.Clone(args)})
			if len(calls) == 1 {
				return []byte("text/plain\nimage/jpeg\nimage/png\n"), nil
			}
			return png, nil
		},
	}

	got, err := r.ReadImage(context.Background())
	if err != nil {
		t.Fatalf("ReadImage: %v", err)
	}
	if !bytes.Equal(got, png) {
		t.Fatalf("bytes = %q, want PNG", got)
	}
	if len(calls) < 2 {
		t.Fatalf("calls = %d, want at least 2", len(calls))
	}
	if calls[0].name != "wl-paste" || !slices.Equal(calls[0].args, []string{"--list-types"}) {
		t.Fatalf("first call = %s %q", calls[0].name, calls[0].args)
	}
	if calls[1].name != "wl-paste" || !slices.Equal(calls[1].args, []string{"--type", "image/png"}) {
		t.Fatalf("second call = %s %q, want wl-paste --type image/png", calls[1].name, calls[1].args)
	}
}

func TestReadImage_X11Fallback(t *testing.T) {
	var calls []runCall
	r := &Reader{
		GOOS:     "linux",
		Getenv:   func(string) string { return "" },
		LookPath: lookPathOK("xclip"),
		Run: func(_ context.Context, name string, args ...string) ([]byte, error) {
			calls = append(calls, runCall{name: name, args: slices.Clone(args)})
			if len(calls) == 1 {
				return []byte("TARGETS\nimage/png\n"), nil
			}
			return []byte{0x89, 'P', 'N', 'G'}, nil
		},
	}

	if _, err := r.ReadImage(context.Background()); err != nil {
		t.Fatalf("ReadImage: %v", err)
	}
	if len(calls) == 0 {
		t.Fatal("no commands")
	}
	want := []string{"-selection", "clipboard", "-t", "TARGETS", "-o"}
	if calls[0].name != "xclip" || !slices.Equal(calls[0].args, want) {
		t.Fatalf("first call = %s %q, want xclip %q", calls[0].name, calls[0].args, want)
	}
	wantImage := []string{"-selection", "clipboard", "-t", "image/png", "-o"}
	if len(calls) < 2 || calls[1].name != "xclip" || !slices.Equal(calls[1].args, wantImage) {
		t.Fatalf("image call = %+v, want xclip %q", calls, wantImage)
	}
}

func TestReadImage_LinuxMissingTools(t *testing.T) {
	r := &Reader{
		GOOS:   "linux",
		Getenv: func(string) string { return "" },
		LookPath: func(string) (string, error) {
			return "", errors.New("not found")
		},
	}
	_, err := r.ReadImage(context.Background())
	me, ok := err.(*MissingToolError)
	if !ok {
		t.Fatalf("err = %T %v, want *MissingToolError", err, err)
	}
	if me.Hint != toolHint || me.Error() != toolHint {
		t.Fatalf("Hint = %q, want %q", me.Hint, toolHint)
	}
}

func TestReadImage_OtherPlatform(t *testing.T) {
	r := &Reader{GOOS: "windows"}
	_, err := r.ReadImage(context.Background())
	if !errors.Is(err, ErrUnsupported) {
		t.Fatalf("err = %v, want ErrUnsupported", err)
	}
}

func TestRun_Timeout(t *testing.T) {
	r := &Reader{
		GOOS: "darwin",
		Run: func(ctx context.Context, _ string, _ ...string) ([]byte, error) {
			deadline, ok := ctx.Deadline()
			if !ok {
				t.Fatal("Run context has no deadline")
			}
			remain := time.Until(deadline)
			if remain <= 0 || remain > 5*time.Second {
				t.Fatalf("deadline remaining = %s, want > 0 and ≤ 5s", remain)
			}
			return []byte("hi"), nil
		},
	}
	got, err := r.ReadText(context.Background())
	if err != nil {
		t.Fatalf("ReadText: %v", err)
	}
	if got != "hi" {
		t.Fatalf("text = %q, want hi", got)
	}
}

type runCall struct {
	ctx  context.Context
	name string
	args []string
}

// exitWithStderr builds an *exec.ExitError carrying stderr, the shape
// Output() returns when wl-paste or xclip exits non-zero.
func exitWithStderr(t *testing.T, stderr string) *exec.ExitError {
	t.Helper()
	err := exec.Command("false").Run()
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("false: %v", err)
	}
	exitErr.Stderr = []byte(stderr)
	return exitErr
}

func TestLinuxEmptyClipboard(t *testing.T) {
	tests := []struct {
		name    string
		wayland bool
		tool    string
		stderr  string
		empty   bool
	}{
		{name: "wl nothing copied", wayland: true, tool: "wl-paste", stderr: "Nothing is copied\n", empty: true},
		{name: "wl no selection", wayland: true, tool: "wl-paste", stderr: "No selection\n", empty: true},
		{name: "xclip no owner", wayland: false, tool: "xclip", stderr: "There is no owner for the CLIPBOARD selection\n", empty: true},
		{name: "connection refused", wayland: true, tool: "wl-paste", stderr: "Connection refused\n", empty: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := linuxReader(tc.wayland, tc.tool, func(_ context.Context, _ string, _ ...string) ([]byte, error) {
				return nil, exitWithStderr(t, tc.stderr)
			})
			_, imgErr := r.ReadImage(context.Background())
			if tc.empty {
				if !errors.Is(imgErr, ErrNoImage) {
					t.Fatalf("ReadImage err = %v, want ErrNoImage", imgErr)
				}
			} else if imgErr == nil || errors.Is(imgErr, ErrNoImage) {
				t.Fatalf("ReadImage err = %v, want a wrapped failure", imgErr)
			}

			text, textErr := r.ReadText(context.Background())
			if tc.empty {
				if text != "" || textErr != nil {
					t.Fatalf("ReadText = %q, %v, want empty nil", text, textErr)
				}
				return
			}
			if text == "" && textErr == nil {
				t.Fatal("ReadText = empty nil, want a failure")
			}
			if errors.Is(textErr, ErrNoImage) {
				t.Fatalf("ReadText err = %v, want a wrapped failure", textErr)
			}
		})
	}
}

func linuxReader(wayland bool, tool string, run Runner) *Reader {
	display := ""
	if wayland {
		display = "wayland-0"
	}
	return &Reader{
		GOOS:     "linux",
		Getenv:   func(string) string { return display },
		LookPath: lookPathOK(tool),
		Run:      run,
	}
}

func lookPathOK(want string) func(string) (string, error) {
	return func(name string) (string, error) {
		if name == want {
			return "/usr/bin/" + name, nil
		}
		return "", errors.New("not found")
	}
}

func darwinScriptPath(t *testing.T, args []string) string {
	t.Helper()
	const marker = `POSIX file "`
	for _, arg := range args {
		i := strings.Index(arg, marker)
		if i < 0 {
			continue
		}
		rest := arg[i+len(marker):]
		end := strings.IndexByte(rest, '"')
		if end < 0 {
			t.Fatalf("unclosed path in %q", arg)
		}
		return rest[:end]
	}
	t.Fatalf("osascript args missing POSIX path: %q", args)
	return ""
}

type inputCall struct {
	stdin string
	name  string
	args  []string
}

func recordInput(calls *[]inputCall) func(context.Context, string, string, ...string) error {
	return func(_ context.Context, stdin, name string, args ...string) error {
		*calls = append(*calls, inputCall{stdin: stdin, name: name, args: args})
		return nil
	}
}

func TestWriteText_Darwin(t *testing.T) {
	var calls []inputCall
	r := &Reader{GOOS: "darwin", RunInput: recordInput(&calls)}
	if err := r.WriteText(context.Background(), "hi\n世界"); err != nil {
		t.Fatalf("WriteText: %v", err)
	}
	if len(calls) != 1 || calls[0].name != "pbcopy" || len(calls[0].args) != 0 || calls[0].stdin != "hi\n世界" {
		t.Fatalf("calls = %+v", calls)
	}
}

func TestWriteText_Wayland(t *testing.T) {
	var calls []inputCall
	r := linuxReader(true, "wl-copy", nil)
	r.RunInput = recordInput(&calls)
	if err := r.WriteText(context.Background(), "x"); err != nil {
		t.Fatalf("WriteText: %v", err)
	}
	if len(calls) != 1 || calls[0].name != "wl-copy" {
		t.Fatalf("calls = %+v", calls)
	}
}

func TestWriteText_X11(t *testing.T) {
	var calls []inputCall
	r := linuxReader(false, "xclip", nil)
	r.RunInput = recordInput(&calls)
	if err := r.WriteText(context.Background(), "x"); err != nil {
		t.Fatalf("WriteText: %v", err)
	}
	if len(calls) != 1 || calls[0].name != "xclip" || strings.Join(calls[0].args, " ") != "-selection clipboard" {
		t.Fatalf("calls = %+v", calls)
	}
}

func TestWriteText_MissingTool(t *testing.T) {
	var calls []inputCall
	r := linuxReader(true, "none", nil)
	r.RunInput = recordInput(&calls)
	err := r.WriteText(context.Background(), "x")
	var missing *MissingToolError
	if !errors.As(err, &missing) {
		t.Fatalf("err = %v, want MissingToolError", err)
	}
	if len(calls) != 0 {
		t.Fatalf("unexpected calls %+v", calls)
	}
}

func TestWriteText_OtherPlatform(t *testing.T) {
	r := &Reader{GOOS: "windows"}
	if err := r.WriteText(context.Background(), "x"); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("err = %v, want ErrUnsupported", err)
	}
}
