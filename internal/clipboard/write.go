package clipboard

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

const linuxWriteHint = "Install wl-clipboard or xclip to copy to the clipboard"

// InputRunner executes name with args, feeding stdin to the command.
// The context it receives is cancelled after five seconds.
type InputRunner func(ctx context.Context, stdin string, name string, args ...string) error

// defaultRunInput deliberately leaves Stdout/Stderr unset (/dev/null):
// xclip and wl-copy fork a background owner of the selection, and a pipe
// held by that child would make Wait block until the timeout.
func defaultRunInput(ctx context.Context, stdin string, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdin = strings.NewReader(stdin)
	return cmd.Run()
}

// WriteText puts s on the system clipboard.
// darwin uses pbcopy; linux uses wl-copy on Wayland, otherwise xclip.
// A missing linux tool returns *MissingToolError; other platforms return
// ErrUnsupported.
func (r *Reader) WriteText(ctx context.Context, s string) error {
	var name string
	var args []string
	switch r.GOOS {
	case "darwin":
		name = "pbcopy"
	case "linux":
		switch {
		case r.Getenv("WAYLAND_DISPLAY") != "" && r.hasTool("wl-copy"):
			name = "wl-copy"
		case r.hasTool("xclip"):
			name, args = "xclip", []string{"-selection", "clipboard"}
		default:
			return &MissingToolError{Hint: linuxWriteHint}
		}
	default:
		return ErrUnsupported
	}
	ctx, cancel := context.WithTimeout(ctx, runTimeout)
	defer cancel()
	if err := r.RunInput(ctx, s, name, args...); err != nil {
		return fmt.Errorf("clipboard: %s: %w", name, err)
	}
	return nil
}
