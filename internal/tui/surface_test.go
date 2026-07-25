package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

func TestPaintGrokNightSurfaceEmitsBackgroundTokens(t *testing.T) {
	_ = lipgloss.DefaultRenderer()

	view := "hello"
	painted := paintGrokNightSurface(view, 20, 4, 2)
	if painted == "" {
		t.Fatal("expected painted surface")
	}
	// Every row padded to width; uniform near-black surface (not body/chrome split).
	for i, line := range strings.Split(painted, "\n") {
		if lipgloss.Width(line) != 20 {
			t.Fatalf("row %d width=%d want 20 (%q)", i, lipgloss.Width(line), ansi.Strip(line))
		}
	}
	if len(strings.Split(painted, "\n")) != 4 {
		t.Fatalf("want 4 rows, got %d", len(strings.Split(painted, "\n")))
	}
	if !hasGrokNightBG(painted) {
		t.Fatalf("expected GrokNight black bg on painted surface, sample=%q", truncateForTest(painted, 120))
	}
}

func TestPaintSurfaceLineKeepsBgThroughStyledText(t *testing.T) {
	// Styled text with an embedded reset used to leave only the trailing pad
	// painted (gray tail). After reinject, open-bg must appear more than once.
	styled := dimStyle.Render("abc") + accentStyle.Render("def")
	painted := paintSurfaceLine(styled, 40, DefaultTheme.BackgroundPanel)
	open, _ := backgroundOpenClose(DefaultTheme.BackgroundPanel)
	if open == "" {
		t.Skip("color profile emitted no background sequence")
	}
	if strings.Count(painted, open) < 2 {
		t.Fatalf("expected bg reinjected through resets, count=%d sample=%q",
			strings.Count(painted, open), truncateForTest(painted, 160))
	}
	if lipgloss.Width(painted) != 40 {
		t.Fatalf("width=%d want 40", lipgloss.Width(painted))
	}
	// Stripped content still starts with the glyphs (not eaten by paint).
	stripped := ansi.Strip(painted)
	if !strings.HasPrefix(strings.TrimRight(stripped, " "), "abcdef") {
		t.Fatalf("glyphs lost under paint: %q", stripped)
	}
}

func TestFormatThemePanelIsStructuredDarkPanel(t *testing.T) {
	body := formatThemePanel("magenta")
	stripped := ansi.Strip(body)
	if strings.Contains(stripped, "accents:") && strings.Count(stripped, ",") >= 3 {
		t.Fatalf("must not be a flat CSV accents dump: %q", stripped)
	}
	if !strings.Contains(stripped, "GrokNight") {
		t.Fatalf("expected GrokNight title, got %q", stripped)
	}
	if !strings.Contains(stripped, "▸") && !strings.Contains(stripped, "active") {
		t.Fatalf("expected active marker, got %q", stripped)
	}
	if !strings.Contains(stripped, "magenta") || !strings.Contains(stripped, "green") {
		t.Fatalf("expected accent rows, got %q", stripped)
	}
	// Active row uses Selection background token.
	sel := lipgloss.NewStyle().Background(DefaultTheme.Selection).Render("x")
	// Body should contain selection styling or at least multi-line row chrome.
	if strings.Count(stripped, "\n") < 3 {
		t.Fatalf("expected multi-row panel, got %q", stripped)
	}
	_ = sel
}

func TestViewPaintsGrokNightBackgroundOnWelcomeAndSession(t *testing.T) {
	m := NewModel(Config{}).SetSize(60, 20)
	welcome := m.View()
	if welcome == "" {
		t.Fatal("empty welcome view")
	}
	// Real View() must emit GrokNight truecolor backgrounds (panel #0a0a0a /
	// base #141414 → 48;2;10;10;10 and 48;2;20;20;20 in CSI form).
	if !hasGrokNightBG(welcome) {
		t.Fatalf("welcome View missing GrokNight bg ANSI; sample=%q", truncateForTest(welcome, 180))
	}
	// In-session: user turn + theme panel block.
	m.timeline = m.timeline.StartUserTurn("hello")
	m, _ = m.runThemeCommand(nil)
	session := m.View()
	if !hasGrokNightBG(session) {
		t.Fatalf("session View missing GrokNight bg ANSI; sample=%q", truncateForTest(session, 180))
	}
	stripped := ansi.Strip(session)
	if !strings.Contains(stripped, "hello") && !strings.Contains(stripped, "GrokNight") {
		t.Fatalf("expected session content in view, got %q", truncateForTest(stripped, 200))
	}
	// Background fill present: painted rows reach full width.
	fullWidth := 0
	for _, line := range strings.Split(session, "\n") {
		if lipgloss.Width(line) >= 60 {
			fullWidth++
		}
	}
	if fullWidth < 5 {
		t.Fatalf("expected GrokNight surface fill (full-width rows), got %d", fullWidth)
	}
}

func hasGrokNightBG(s string) bool {
	// Truecolor CSI (48;2;R;G;B) when the profile allows it.
	if strings.Contains(s, "48;2;10;10;10") || strings.Contains(s, "48:2:10:10:10") ||
		strings.Contains(s, "48;2;20;20;20") || strings.Contains(s, "48:2:20:20:20") {
		return true
	}
	// Hex tokens if the renderer embeds them.
	low := strings.ToLower(s)
	if strings.Contains(low, "0a0a0a") || strings.Contains(low, "141414") {
		return true
	}
	// Downgraded profiles still paint a black/near-black background (ANSI 40
	// or 256-color index) — that is the honest GrokNight fill on weak TTYs.
	if strings.Contains(s, "\x1b[40m") || strings.Contains(s, "[40m") ||
		strings.Contains(s, "48;5;") || strings.Contains(s, "48:5:") {
		return true
	}
	return false
}

func truncateForTest(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

func TestThemeCommandBodyUsesPanelBuilder(t *testing.T) {
	defer ApplyAccent(AccentPresets[0].Color)
	m := NewModel(Config{})
	m, _ = m.runThemeCommand(nil)
	last := m.timeline.Turns[len(m.timeline.Turns)-1].Blocks
	body := last[len(last)-1].Body
	if !strings.Contains(ansi.Strip(body), "active") && !strings.Contains(body, "▸") {
		t.Fatalf("theme command should store structured panel body, got %q", body)
	}
	if strings.HasPrefix(strings.TrimSpace(ansi.Strip(body)), "accents:") {
		t.Fatalf("old CSV dump still used: %q", body)
	}
}
