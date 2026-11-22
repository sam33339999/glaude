// Package clipboard reads image and text bytes from the system clipboard
// through an injectable command runner.
package clipboard

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"time"

	"github.com/atotto/clipboard"
)

const runTimeout = 5 * time.Second

// ErrNoImage is returned when the clipboard has no image.
var ErrNoImage = errors.New("clipboard: no image")

// ErrUnsupported is returned when this platform cannot read clipboard images.
var ErrUnsupported = errors.New("clipboard: unsupported platform")

// MissingToolError means a clipboard command is not installed.
// Error returns Hint.
type MissingToolError struct {
	// Hint is the installation message returned by Error.
	Hint string
}

// Error returns Hint.
func (e *MissingToolError) Error() string {
	return e.Hint
}

// Runner executes name with args and returns its stdout.
// The context it receives is cancelled after five seconds.
type Runner func(ctx context.Context, name string, args ...string) ([]byte, error)

// Reader reads clipboard images and text.
// Its fields are injectable so callers can fake the host environment.
type Reader struct {
	// Run executes a command. New uses exec.CommandContext(...).Output.
	Run Runner
	// RunInput executes a command with stdin. New uses exec.CommandContext(...).Run.
	RunInput InputRunner
	// LookPath resolves a command name. New uses exec.LookPath.
	LookPath func(string) (string, error)
	// Getenv reads an environment variable. New uses os.Getenv.
	Getenv func(string) string
	// GOOS selects the platform implementation.
	GOOS string
	// TempDir is the directory used for the macOS clipboard temp file.
	TempDir string
}

// New returns a Reader bound to the current process.
func New() *Reader {
	return &Reader{
		Run:      defaultRun,
		RunInput: defaultRunInput,
		LookPath: exec.LookPath,
		Getenv:   os.Getenv,
		GOOS:     runtime.GOOS,
		TempDir:  os.TempDir(),
	}
}

func defaultRun(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).Output()
}

// ReadImage returns raw clipboard image bytes.
// It does not detect a media type. Platforms other than darwin and linux
// return ErrUnsupported.
func (r *Reader) ReadImage(ctx context.Context) ([]byte, error) {
	switch r.GOOS {
	case "darwin":
		return r.readImageDarwin(ctx)
	case "linux":
		return r.readImageLinux(ctx)
	default:
		return nil, ErrUnsupported
	}
}

// ReadText returns clipboard text.
// darwin uses pbpaste, linux uses wl-paste or xclip, and every other
// platform uses github.com/atotto/clipboard.
func (r *Reader) ReadText(ctx context.Context) (string, error) {
	switch r.GOOS {
	case "darwin":
		return r.readTextDarwin(ctx)
	case "linux":
		return r.readTextLinux(ctx)
	default:
		return readTextOther(ctx)
	}
}

func readTextOther(ctx context.Context) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", fmt.Errorf("clipboard: %w", err)
	}
	text, err := clipboard.ReadAll()
	if err != nil {
		return "", fmt.Errorf("clipboard: %w", err)
	}
	return text, nil
}

func (r *Reader) run(ctx context.Context, name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, runTimeout)
	defer cancel()
	out, err := r.Run(ctx, name, args...)
	if err != nil {
		return nil, fmt.Errorf("clipboard: %s: %w", name, err)
	}
	return out, nil
}
