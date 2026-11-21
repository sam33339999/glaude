package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/tunsuy/glaude/internal/config"
	"github.com/tunsuy/glaude/internal/mcp"
	"github.com/tunsuy/glaude/internal/tools"
	mcptools "github.com/tunsuy/glaude/internal/tools/mcp"
)

// Startup MCP wiring: connect the pool to every configured server and register
// the discovered per-server tools. Registration is one-shot and pinned for the
// session (provider prefix-cache stability — see mcp.RegisterServerTools).
// Connection failures are non-fatal — they are recorded on the pool's
// connection records (surfaced by `mcp list` and the /mcp command) and joined
// into the returned error for debug logging.

// mcpConnectTimeout bounds a single startup connection attempt. It is a var
// (not a const) so tests can inject a short timeout; production keeps 30s.
var mcpConnectTimeout = 30 * time.Second

// connectConfiguredMCPServers connects pool to the configured MCP servers and
// registers each connected server's tools into reg via mcptools.RegisterServerTools.
//
// Server selection mirrors `mcp list` semantics (config.MCPStore.ListMCPServers:
// deduplicated across scopes, rejected and non-listable entries omitted), with
// one addition: project-scoped (.mcp.json) servers are only connected when the
// user has approved them (enableAllProjectMcpServers or enabledMcpjsonServers
// in the merged settings). Pending-approval servers are left untouched.
//
// The returned error joins all per-server connect/list failures; callers treat
// it as non-fatal (startup must not fail because an MCP server is down).
func connectConfiguredMCPServers(
	ctx context.Context,
	reg *tools.Registry,
	pool *mcp.Pool,
	settings *config.LayeredSettings,
	homeDir, workingDir string,
	flagServers []config.MCPServerEntry,
) error {
	store := config.NewMCPStore(homeDir, workingDir)
	var errs []error
	// --mcp-config servers first: named explicitly, so no approval, and they
	// shadow configured servers of the same name.
	fromFlag := map[string]bool{}
	for _, e := range flagServers {
		fromFlag[e.Name] = true
		cfg, err := mcpServerConfigFromEntry(e)
		if err != nil {
			errs = append(errs, fmt.Errorf("mcp: --mcp-config server %q: %w", e.Name, err))
			continue
		}
		if err := connectWithWatchdog(ctx, pool, e.Name, cfg); err != nil {
			errs = append(errs, fmt.Errorf("mcp: connect %q: %w", e.Name, err))
		}
	}
	for _, e := range store.ListMCPServers() {
		if fromFlag[e.Name] {
			continue
		}
		if _, scope, ok := store.GetMCPServer(e.Name); ok && scope == config.MCPScopeProject &&
			!projectMCPApproved(settings, e.Name) {
			continue
		}
		cfg, err := mcpServerConfigFromEntry(e)
		if err != nil {
			errs = append(errs, fmt.Errorf("mcp: server %q config: %w", e.Name, err))
			continue
		}
		if err := connectWithWatchdog(ctx, pool, e.Name, cfg); err != nil {
			errs = append(errs, fmt.Errorf("mcp: connect %q: %w", e.Name, err))
		}
	}

	if _, err := mcptools.RegisterServerTools(ctx, reg, pool); err != nil {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

// connectWithWatchdog connects one server with a bounded, NON-ABANDONING
// startup wait (mcpConnectTimeout).
//
// Kernel constraint: the agtkeel jsonRPCClient binds its background reader
// goroutine to the context of the first call (the initialize handshake), so a
// healthy connection needs that context to outlive boot. We therefore pass a
// cancellable context WITHOUT a deadline and enforce the timeout with an
// external watchdog: on success the context is deliberately left uncancelled
// (its lifetime becomes the connection's lifetime); on failure the deferred
// cancel releases it.
//
// On timeout or caller cancellation we cancel that context and WAIT for the
// Connect goroutine to fully drain before returning. Cancellation propagates
// into Connect (HTTP request contexts / stdio Recv ctx.Done / the call()
// select), and dialServer's failure path closes the transport, so the drain is
// bounded. Every pool write therefore terminates before boot returns: no
// background goroutine may mutate pool connection state mid-session
// (CR-BF01-1, carrying the CR-BF03-2 P0 escalation; PM followup constraint).
//
// A server whose handshake exceeds the timeout is failed for this session.
// That cost is real but the alternative benefit was illusory: under session
// pinning a late-landing connection's tools would never be registered into
// this session's registry anyway (RegisterServerTools is a one-shot no-op on
// re-pass), and keeping a half-booted connection alive only created a data
// race against unsynchronized /mcp and resource-tool readers. The kernel-side
// root fix (reader not bound to the handshake ctx; real transport teardown) is
// tracked as cross-repo debt with BF-19 (CR-BF01-7).
func connectWithWatchdog(ctx context.Context, pool *mcp.Pool, name string, cfg mcp.ServerConfig) (err error) {
	cctx, cancel := context.WithCancel(ctx)
	defer func() {
		if err != nil {
			cancel()
		}
	}()

	done := make(chan error, 1)
	go func() {
		done <- pool.Connect(cctx, name, cfg)
	}()

	timer := time.NewTimer(mcpConnectTimeout)
	defer timer.Stop()

	select {
	case err = <-done:
		return err
	case <-timer.C:
		err = fmt.Errorf("handshake timed out after %s", mcpConnectTimeout)
	case <-ctx.Done():
		err = ctx.Err()
	}

	// The watchdog fired. Cancel and drain: do not return until the Connect
	// goroutine has finished, so no pool write can outlive startup.
	cancel()
	if connErr := <-done; connErr == nil {
		// The handshake raced the deadline and won: the reader goroutine is
		// bound to the now-cancelled context, so this connection cannot serve
		// the session. Tear it down synchronously (still within boot) rather
		// than leaving a dead "connected" record behind.
		if derr := pool.Disconnect(name); derr != nil {
			err = fmt.Errorf("%w (teardown: %v)", err, derr)
		}
	}
	return err
}

// projectMCPApproved reports whether a project-scoped (.mcp.json) server has
// been approved for use via the merged settings.
func projectMCPApproved(settings *config.LayeredSettings, name string) bool {
	if settings == nil || settings.Merged == nil {
		return false
	}
	if settings.Merged.EnableAllProjectMCP != nil && *settings.Merged.EnableAllProjectMCP {
		return true
	}
	for _, n := range settings.Merged.EnabledMCPServers {
		if n == name {
			return true
		}
	}
	return false
}
