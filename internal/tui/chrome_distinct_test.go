package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/junnhwan/bond-code/internal/agent"
)

// oldPrimaryChromeMarkers are identity strings from main's TUI that must not
// appear as the primary chrome on this branch (plan acceptance criterion 4).
var oldPrimaryChromeMarkers = []string{
	"> ", // main composer prompt (space after >)
	"Type a message",
	"running · Esc/Ctrl+C stop",
	"Esc/Ctrl+C stop run + queue",
	"Local coding agent runtime",
	"BOND CODE",
	"/ commands",
	"? help",
	"@ files",
}

func TestChromeDistinctEmptySession(t *testing.T) {
	m := NewModel(Config{
		Status: Status{Model: "glm-5.1", ProjectRoot: "bond-code", GitBranch: "main"},
	}).SetSize(80, 28)
	view := ansi.Strip(m.View())

	// New markers present.
	for _, want := range []string{"Bond Code", "terminal coding agent", "New session", "/help", "\u276f"} {
		if !strings.Contains(view, want) {
			if want == "\u276f" {
				if !strings.Contains(view, "❯") && !strings.Contains(view, string(rune(0x276f))) {
					t.Fatalf("empty session missing prompt glyph:\n%s", view)
				}
				continue
			}
			t.Fatalf("empty session missing new marker %q:\n%s", want, view)
		}
	}
	// Brand icon: braille linked-ring mark (Grok-style icon, not letterforms).
	if !containsBraille(view) {
		t.Fatalf("wide empty session must show braille bond mark:\n%s", view)
	}
	// Old box-drawing clip-art must be gone.
	if strings.Contains(view, "\u256d\u2500\u2500") {
		t.Fatalf("welcome still shows old box-drawing logo:\n%s", view)
	}
	// Old primary chrome absent.
	for _, old := range oldPrimaryChromeMarkers {
		if old == "> " {
			// Avoid false positive on "\u276f " containing unrelated; check composer prompt specifically.
			// main uses Prompt = "> "; we use ❯. Fail if a line starts with "> " as the prompt.
			for _, line := range strings.Split(view, "\n") {
				trim := strings.TrimLeft(line, " ")
				if strings.HasPrefix(trim, "> ") && !strings.HasPrefix(trim, "> =") {
					// allow code/examples but not the live prompt row with placeholder
					if strings.Contains(trim, "Build anything") || strings.Contains(trim, "Type a message") {
						t.Fatalf("empty session still uses old > prompt: %q", line)
					}
				}
			}
			continue
		}
		if strings.Contains(view, old) {
			t.Fatalf("empty session leaked old chrome %q:\n%s", old, view)
		}
	}
	// No permanent Agent switcher while idle single-agent.
	if strings.Contains(view, "⬡ Agent") || strings.Contains(view, "Agent coordinator") {
		t.Fatalf("idle view must not show permanent Agent bar:\n%s", view)
	}
}

func TestChromeDistinctBusySession(t *testing.T) {
	m := NewModel(Config{Status: Status{Model: "glm-5.1"}}).SetSize(80, 24)
	m.timeline = m.timeline.StartUserTurn("hello")
	m.agent.Busy = true
	m.agent.LiveDetail = "tool: read_file"
	view := ansi.Strip(m.View())

	if !strings.Contains(view, "read_file") && !strings.Contains(view, "tool:") {
		if !strings.Contains(view, "thinking") {
			t.Fatalf("busy view should surface activity:\n%s", view)
		}
	}
	// Shortcuts cancel language (new), not main busy footer.
	if !strings.Contains(strings.ToLower(view), "esc") {
		t.Fatalf("busy shortcuts should mention esc:\n%s", view)
	}
	if strings.Contains(view, "running · Esc/Ctrl+C stop") {
		t.Fatalf("busy view must not use main running footer:\n%s", view)
	}
	if strings.Contains(view, "Type a message") || strings.Contains(view, "Local coding agent runtime") {
		t.Fatalf("busy view leaked main identity:\n%s", view)
	}
}

func TestChromeDistinctPermissionSession(t *testing.T) {
	m := NewModel(Config{}).SetSize(80, 24)
	m.timeline = m.timeline.StartUserTurn("x")
	m.agent.Pending = &agent.Event{
		Type:     agent.EventToolConfirmationRequested,
		ToolName: "write_file",
		Risk:     "medium",
		Input:    `{"path":"a.go"}`,
	}
	view := ansi.Strip(m.View())
	for _, want := range []string{"Permission required", "Allow once", "Reject"} {
		if !strings.Contains(view, want) {
			t.Fatalf("permission view missing %q:\n%s", want, view)
		}
	}
	// Vertical selected option.
	if !strings.Contains(view, "\u276f Allow once") && !strings.Contains(view, "❯ Allow once") &&
		!strings.Contains(view, "\u276f Always") && !strings.Contains(view, "❯ Always") &&
		!strings.Contains(view, "\u276f Reject") && !strings.Contains(view, "❯ Reject") {
		t.Fatalf("permission options must be vertical with cursor glyph:\n%s", view)
	}
	for _, old := range []string{"running · Esc/Ctrl+C stop", "Type a message", "Local coding agent runtime"} {
		if strings.Contains(view, old) {
			t.Fatalf("permission view leaked old chrome %q:\n%s", old, view)
		}
	}
}

func TestComposerPromptIsNotMainGreaterThan(t *testing.T) {
	m := NewModel(Config{}).SetSize(80, 12)
	// Drive real composer view path.
	body := ansi.Strip(m.composerViewForWidth(60))
	if strings.Contains(body, "Type a message") {
		t.Fatalf("composer still uses main placeholder:\n%s", body)
	}
	if m.composer.Input.Prompt == "> " {
		t.Fatal("composer Prompt still main \"> \"")
	}
	if !strings.Contains(body, "Build anything") && !strings.Contains(body, string(rune(0x276f))) && !strings.Contains(body, "❯") {
		// empty focused textarea shows placeholder
		if !strings.Contains(body, "Build anything") {
			t.Fatalf("composer should show Build anything placeholder or ❯:\n%s", body)
		}
	}
}
