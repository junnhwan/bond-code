package tui

import (
	"os"
	"os/exec"
	"runtime"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// External editor (Phase 3.2). <leader>e hands the current draft to $EDITOR (or
// $VISUAL, with a platform default fallback) via tea.ExecProcess, which pauses
// the Bubble Tea renderer and gives the editor the tty. When the editor exits,
// the draft file is read back into the composer so long prompts can be written
// in a real editor and submitted from the TUI.
//
// This is the one feature that needs a tty-attached subprocess; tea.ExecProcess
// is the canonical Bubble Tea primitive for it, so the renderer does not fight
// the editor for the screen.

// editorDoneMsg carries the edited draft back into Update once the editor exits.
type editorDoneMsg struct {
	content string
	err     error
}

// openExternalEditor writes the current draft to a temp file and returns a Cmd
// that runs the resolved editor on it. No-op (with a toast) when no editor is
// configured.
func (m Model) openExternalEditor() (Model, tea.Cmd) {
	if editorCommand() == "" {
		return m.pushToast("no $EDITOR/$VISUAL configured", toastWarn), nil
	}
	draft := m.inputValue()
	tmp, err := os.CreateTemp("", "bondcode-draft-*.md")
	if err != nil {
		return m.pushToast("could not open draft file: "+err.Error(), toastError), nil
	}
	if _, err := tmp.WriteString(draft); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
		return m.pushToast("could not write draft: "+err.Error(), toastError), nil
	}
	tmpPath := tmp.Name()
	_ = tmp.Close()

	name, args := parseEditorCommand(tmpPath)
	cmd := exec.Command(name, args...)
	return m, tea.ExecProcess(cmd, func(err error) tea.Msg {
		// Runs after the editor exits; the temp file holds the edited content.
		content, readErr := os.ReadFile(tmpPath)
		_ = os.Remove(tmpPath)
		if readErr != nil {
			return editorDoneMsg{err: readErr}
		}
		return editorDoneMsg{content: string(content), err: err}
	})
}

// applyEditorResult is the Update-side handler for editorDoneMsg: it drops the
// trailing newline most editors append and loads the content into the composer.
// An editor error (non-zero exit / killed) surfaces as a toast but still
// recovers whatever the editor wrote, if anything.
func (m Model) applyEditorResult(msg editorDoneMsg) (Model, tea.Cmd) {
	if msg.err != nil && msg.content == "" {
		return m.pushToast("editor exited with error: "+msg.err.Error(), toastWarn), nil
	}
	// Strip exactly one trailing newline: editors terminate files with \n, which
	// would otherwise become an unwanted blank line in the single-line composer.
	content := strings.TrimSuffix(msg.content, "\n")
	content = strings.TrimSuffix(content, "\r")
	m = m.SetInput(content)
	m.navTurnIdx = -1
	if msg.err != nil {
		m = m.pushToast("editor exited with error; recovered content", toastWarn)
	}
	return m, nil
}

// editorCommand resolves the editor binary + raw arg string from the
// environment, with a platform default. Empty only when explicitly cleared and
// no default resolves (rare).
func editorCommand() string {
	if v := strings.TrimSpace(os.Getenv("EDITOR")); v != "" {
		return v
	}
	if v := strings.TrimSpace(os.Getenv("VISUAL")); v != "" {
		return v
	}
	switch runtime.GOOS {
	case "windows":
		return "notepad"
	case "darwin":
		return "vim"
	default:
		if _, err := exec.LookPath("nano"); err == nil {
			return "nano"
		}
		return "vi"
	}
}

// parseEditorCommand splits the editor command into name + args, appending the
// draft path. Handles EDITOR values like "code -w" or "nvim".
func parseEditorCommand(filePath string) (string, []string) {
	parts := strings.Fields(editorCommand())
	if len(parts) == 0 {
		return "vi", []string{filePath}
	}
	return parts[0], append(parts[1:], filePath)
}
