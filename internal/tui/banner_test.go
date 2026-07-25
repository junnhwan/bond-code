package tui

import (
	"strings"
	"testing"
	"unicode"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

func TestWelcomeMessageMentionsHelpCommand(t *testing.T) {
	welcome := RenderWelcomeMessage(80)
	if !strings.Contains(welcome, "/help") {
		t.Fatalf("expected welcome message to mention /help, got:\n%s", welcome)
	}
}

func TestWelcomeMessageUsesCanonicalCompactGuidance(t *testing.T) {
	welcome := ansi.Strip(RenderWelcomeMessage(80))
	// Help under the menu is incremental only — full slash list via /help, plus @path.
	// /resume and /status live in the welcome menu, not re-listed here.
	for _, want := range []string{"/help", "@path", "Press enter to chat"} {
		if !strings.Contains(welcome, want) {
			t.Fatalf("expected welcome message to mention %q, got:\n%s", want, welcome)
		}
	}
	for _, forbidden := range []string{
		"/resume", "/status", "/clear",
		"build anything", // tip removed; placeholder owns that line
		"? help", "/session", "Ctrl+X", "<leader>", "Ctrl+E", "Keys:", "Ask about this repo",
	} {
		if strings.Contains(welcome, forbidden) {
			t.Fatalf("welcome message leaked redundant or legacy guidance %q:\n%s", forbidden, welcome)
		}
	}
}

func TestBannerShowsBondIconMark(t *testing.T) {
	wide := ansi.Strip(RenderBanner(80, "v1.0.0"))
	if !containsBraille(wide) {
		t.Fatalf("wide banner should show braille icon mark, got:\n%s", wide)
	}
	if !strings.Contains(wide, "Bond Code") {
		t.Fatalf("welcome must name the product Bond Code under the mark, got:\n%s", wide)
	}
	if !strings.Contains(wide, "terminal coding agent") {
		t.Fatalf("expected slogan under the mark, got:\n%s", wide)
	}
	// Must be the linked-ring mark + caption, not old box art / dual wordmark.
	for _, forbidden := range []string{
		"BOND CODE", // all-caps dual banner of main branch
		"Local coding agent runtime",
		"\u256d\u2500\u2500", // old linked-box ASCII
		"B O N D",
	} {
		if strings.Contains(wide, forbidden) {
			t.Fatalf("banner leaked old identity %q:\n%s", forbidden, wide)
		}
	}

	narrow := ansi.Strip(RenderBanner(18, "v1.0.0"))
	if !containsBraille(narrow) {
		t.Fatalf("narrow terminal should still show icon mark, got:\n%s", narrow)
	}
	for _, line := range strings.Split(narrow, "\n") {
		if w := lipgloss.Width(line); w > 18 {
			t.Fatalf("narrow banner line exceeds width: %d > 18\n%s", w, line)
		}
	}
}

func TestBondLogoScalesByWidth(t *testing.T) {
	xs := bondLogoForWidth(10)
	lg := bondLogoForWidth(80)
	if xs == lg {
		t.Fatal("expected different logo densities for narrow vs wide")
	}
	if !containsBraille(xs) || !containsBraille(lg) {
		t.Fatal("all logo sizes must be braille marks")
	}
	// Larger logo has more rows.
	if strings.Count(lg, "\n") < strings.Count(xs, "\n") {
		t.Fatalf("lg should be taller than xs: xs=%q lg=%q", xs, lg)
	}
}

func TestWelcomeMenuListsStatus(t *testing.T) {
	menu := RenderWelcomeMenu(80)
	for _, want := range []string{"New session", "Resume last", "Status", "/clear", "/resume", "/status"} {
		if !strings.Contains(menu, want) {
			t.Fatalf("menu missing %q:\n%s", want, menu)
		}
	}
	if strings.Contains(menu, "Inspect status") {
		t.Fatalf("menu should use short Status label, got:\n%s", menu)
	}
}

func containsBraille(s string) bool {
	for _, r := range s {
		if unicode.In(r, unicode.Braille) {
			return true
		}
	}
	// Fallback: braille patterns block U+2800–U+28FF
	for _, r := range s {
		if r >= 0x2800 && r <= 0x28FF {
			return true
		}
	}
	return false
}
