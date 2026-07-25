package tui

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode"
)

// ExitInfo is printed after the alt-screen TUI exits so the user can resume
// the same session from a plain terminal (mirrors Grok Build / Claude Code).
type ExitInfo struct {
	SessionID    string
	Title        string
	LastPrompt   string
	LastResponse string
}

// ExitInfo builds the post-quit resume payload from the live model state.
func (m Model) ExitInfo() ExitInfo {
	info := ExitInfo{
		SessionID: strings.TrimSpace(m.cfg.Status.SessionID),
	}
	var firstPrompt string
	for _, turn := range m.timeline.Turns {
		prompt := firstDisplayLine(turn.User.Body)
		if prompt == "" {
			continue
		}
		if firstPrompt == "" {
			firstPrompt = prompt
		}
		info.LastPrompt = prompt
		if resp := lastAssistantLine(turn); resp != "" {
			info.LastResponse = resp
		}
	}
	info.Title = firstPrompt
	if info.Title == "" && info.SessionID != "" {
		info.Title = info.SessionID
	}
	return info
}

func lastAssistantLine(turn Turn) string {
	for i := len(turn.Blocks) - 1; i >= 0; i-- {
		b := turn.Blocks[i]
		if b.Kind != BlockAssistant {
			continue
		}
		if line := firstDisplayLine(b.Body); line != "" {
			return line
		}
		if line := firstDisplayLine(b.Title); line != "" {
			return line
		}
	}
	return ""
}

func firstDisplayLine(text string) string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// Collapse internal whitespace for a single terminal row.
		fields := strings.FieldsFunc(line, unicode.IsSpace)
		if len(fields) == 0 {
			continue
		}
		return strings.Join(fields, " ")
	}
	return ""
}

// FormatExitResumeHint renders the plain-terminal quit tail:
//
//	<title>
//	> last prompt
//	  last response
//
//	Resume this session with:
//	  bondcode --resume <id>
func FormatExitResumeHint(info ExitInfo, maxWidth int, binary string) string {
	if !shouldPrintExitResume(info) {
		return ""
	}
	if maxWidth < 20 {
		maxWidth = 20
	}
	binary = strings.TrimSpace(binary)
	if binary == "" {
		binary = "bondcode"
	}
	var b strings.Builder
	b.WriteByte('\n')
	if summary := exitSummaryLines(info, maxWidth); summary != "" {
		b.WriteString(summary)
		b.WriteByte('\n')
	}
	b.WriteString("Resume this session with:\n")
	fmt.Fprintf(&b, "  %s --resume %s\n", binary, info.SessionID)
	return b.String()
}

func shouldPrintExitResume(info ExitInfo) bool {
	id := strings.TrimSpace(info.SessionID)
	if id == "" || id == "local" {
		return false
	}
	return true
}

func exitSummaryLines(info ExitInfo, maxWidth int) string {
	// Only print a glanceable tail when there was real conversation.
	if strings.TrimSpace(info.LastPrompt) == "" && strings.TrimSpace(info.LastResponse) == "" {
		return ""
	}
	var lines []string
	if title := strings.TrimSpace(info.Title); title != "" {
		lines = append(lines, truncateRunes(title, maxWidth))
	}
	if prompt := strings.TrimSpace(info.LastPrompt); prompt != "" {
		lines = append(lines, "> "+truncateRunes(prompt, max(1, maxWidth-2)))
	}
	if resp := strings.TrimSpace(info.LastResponse); resp != "" {
		lines = append(lines, "  "+truncateRunes(resp, max(1, maxWidth-2)))
	}
	if len(lines) == 0 {
		return ""
	}
	return strings.Join(lines, "\n") + "\n"
}

// PrintExitResumeHint writes the quit tail to w (typically stderr after the
// alt screen has been restored). Best-effort: write errors are ignored so a
// closed pipe cannot crash shutdown.
func PrintExitResumeHint(w io.Writer, info ExitInfo) {
	if w == nil {
		return
	}
	width := 80
	// Best-effort terminal width; fall back quietly.
	if cols := os.Getenv("COLUMNS"); cols != "" {
		var n int
		if _, err := fmt.Sscanf(cols, "%d", &n); err == nil && n > 0 {
			width = n
		}
	}
	bin := filepath.Base(os.Args[0])
	bin = strings.TrimSuffix(bin, ".exe")
	if bin == "" || bin == "." {
		bin = "bondcode"
	}
	_, _ = io.WriteString(w, FormatExitResumeHint(info, width, bin))
}
