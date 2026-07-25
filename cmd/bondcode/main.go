package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"runtime/debug"
	"syscall"

	"github.com/junnhwan/bond-code/internal/app"
	"github.com/junnhwan/bond-code/internal/cli"
	"github.com/junnhwan/bond-code/internal/observe"
)

func main() {
	// Install the process-wide runtime sink first, so any panic or startup
	// error — including bootstrap failures that occur before a session exists —
	// still leaves a body on disk at <projects-dir>/runtime.log. The projects
	// dir is computable from cwd + BONDCODE_HOME alone (no App/sessionID
	// needed); a failure to open the log is non-fatal (Nop default keeps
	// startup alive).
	runtimeLogPath := filepath.Join(app.DefaultProjectDataDir(), "runtime.log")
	if rl, err := observe.NewRuntimeLogger(runtimeLogPath); err == nil {
		observe.SetRuntimeLogger(rl)
	}

	// Outermost recover: catches panics on the main goroutine (bootstrap, and
	// TUI Update/View after tui.Run re-raises). Goroutine panics are caught at
	// their own boundaries (observe.SafeGo / per-site recover) via the same
	// sink; this net ensures the process exits with a visible message and the
	// runtime log path rather than a bare stack trace.
	defer func() {
		if r := recover(); r != nil {
			observe.LogPanic("main", r, debug.Stack())
			fmt.Fprintf(os.Stderr, "\nbondcode crashed: %v\n", r)
			fmt.Fprintf(os.Stderr, "runtime log: %s\n", runtimeLogPath)
			os.Exit(1)
		}
	}()

	// Cancel the root context on Ctrl+C / SIGTERM so the non-TTY paths (--once,
	// slash-command one-shots, future non-interactive modes) shut down cleanly:
	// the agent loop observes ctx.Done() and unwinds, then each command's
	// `defer application.Close()` flushes the per-session debug trace. This is
	// dormant during the interactive TUI, where ^C is captured in raw mode as a
	// KeyMsg and handled by stopAgent() — it never reaches the OS as SIGINT, so
	// this handler does not race Bubble Tea's own signal handling.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	root := cli.NewRootCommand()
	if err := root.ExecuteContext(ctx); err != nil {
		// Persist startup/CLI errors (bad config, MCP connect failure, ...) so
		// they're diagnosable without --debug, then surface the log path.
		observe.LogError("cli", err)
		fmt.Fprintf(os.Stderr, "runtime log: %s\n", runtimeLogPath)
		os.Exit(1)
	}
}
