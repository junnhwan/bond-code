package tui

import (
	"context"
	"os"
	"runtime/debug"

	"github.com/junnhwan/bond-code/internal/observe"

	tea "github.com/charmbracelet/bubbletea"
)

func Run(ctx context.Context, cfg Config) error {
	if cfg.Context == nil {
		cfg.Context = ctx
	}
	opts := []tea.ProgramOption{tea.WithAltScreen(), tea.WithContext(ctx)}
	// Cell motion (not all-motion): click / wheel / drag. Prefer this over
	// AllMotion — better terminal support; free-hover is secondary to select.
	// Default MouseCapture is false so native drag-select/copy works.
	if cfg.MouseCapture {
		opts = append(opts, tea.WithMouseCellMotion())
	}
	program := tea.NewProgram(NewModel(cfg), opts...)
	// Recover panics raised on the main goroutine inside program.Run (Update/
	// View). Kill the program first so bubbletea restores the terminal (exits
	// alt-screen, drops raw mode), log the panic to the runtime sink, then
	// re-raise so main's outer recover exits cleanly with a visible message.
	defer func() {
		if r := recover(); r != nil {
			program.Kill()
			observe.LogPanic("tui", r, debug.Stack())
			panic(r)
		}
	}()
	finalModel, err := program.Run()
	if err != nil {
		return err
	}
	// After alt-screen restore: print a Grok/Claude-style resume hint so the
	// user can reattach without hunting session ids.
	if m, ok := finalModel.(Model); ok {
		PrintExitResumeHint(os.Stderr, m.ExitInfo())
	}
	return nil
}
