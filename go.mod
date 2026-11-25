module github.com/tunsuy/glaude

go 1.26.0

// External dependencies will be added by each responsible layer agent:
//   Agent-CLI    : github.com/spf13/cobra
//   Agent-TUI    : github.com/charmbracelet/bubbletea, lipgloss, bubbles
//   Agent-Services: github.com/anthropics/anthropic-sdk-go, github.com/mark3labs/mcp-go, golang.org/x/oauth2, github.com/golang-jwt/jwt/v5
// The infra layer (pkg/types, pkg/utils, internal/config, internal/state,
// internal/session, internal/hooks, internal/plugin) uses only the Go standard library.

require (
	charm.land/bubbles/v2 v2.2.1
	charm.land/bubbletea/v2 v2.1.1
	charm.land/lipgloss/v2 v2.1.1
	github.com/BurntSushi/toml v1.6.0
	github.com/MoeForge/moejs v0.1.1
	github.com/atotto/clipboard v0.1.4
	github.com/charmbracelet/glamour v1.0.0
	github.com/charmbracelet/x/ansi v0.11.9
	github.com/coder/acp-go-sdk v0.20.0
	github.com/gorilla/websocket v1.5.3
	github.com/spf13/cobra v1.8.1
	github.com/yuin/goldmark v1.7.13
	golang.org/x/image v0.44.0
	golang.org/x/sys v0.47.0
)

require (
	github.com/MoeForge/moego v0.1.0-alpha.3 // indirect
	github.com/alecthomas/chroma/v2 v2.20.0 // indirect
	github.com/aymanbagabas/go-osc52/v2 v2.0.1 // indirect
	github.com/aymerick/douceur v0.2.0 // indirect
	github.com/charmbracelet/colorprofile v0.4.3 // indirect
	github.com/charmbracelet/lipgloss v1.1.1-0.20250404203927-76690c660834 // indirect
	github.com/charmbracelet/ultraviolet v0.0.0-20261008173134-6b8d4baf91b4 // indirect
	github.com/charmbracelet/x/cellbuf v0.0.15 // indirect
	github.com/charmbracelet/x/exp/slice v0.0.0-20250327172914-2fdc97757edf // indirect
	github.com/charmbracelet/x/term v0.2.2 // indirect
	github.com/charmbracelet/x/termios v0.1.1 // indirect
	github.com/charmbracelet/x/windows v0.2.2 // indirect
	github.com/clipperhouse/displaywidth v0.11.0 // indirect
	github.com/clipperhouse/uax29/v2 v2.7.0 // indirect
	github.com/dlclark/regexp2 v1.11.5 // indirect
	github.com/gorilla/css v1.0.1 // indirect
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
	github.com/lucasb-eyer/go-colorful v1.4.1 // indirect
	github.com/mattn/go-isatty v0.0.20 // indirect
	github.com/mattn/go-runewidth v0.0.30 // indirect
	github.com/microcosm-cc/bluemonday v1.0.27 // indirect
	github.com/muesli/cancelreader v0.2.2 // indirect
	github.com/muesli/reflow v0.3.0 // indirect
	github.com/muesli/termenv v0.16.0 // indirect
	github.com/rivo/uniseg v0.4.7 // indirect
	github.com/spf13/pflag v1.0.5 // indirect
	github.com/xo/terminfo v1.0.0 // indirect
	github.com/yuin/goldmark-emoji v1.0.6 // indirect
	golang.org/x/net v0.38.0 // indirect
	golang.org/x/sync v0.22.0 // indirect
	golang.org/x/term v0.36.0 // indirect
	golang.org/x/text v0.40.0 // indirect
)
