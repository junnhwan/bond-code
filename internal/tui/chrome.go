package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// chrome.go owns pure Grok-stack chrome builders: welcome, shortcuts formatting,
// turn-status row shape, and permission option rows. View composition calls these;
// unit tests assert structure without a live PTY.

// HintItem is one shortcuts-bar entry (key display + short label).
type HintItem struct {
	Key   string
	Label string
	// Pinned keeps the hint when the bar is width-truncated.
	Pinned bool
}

// FormatShortcutsBar renders key:label pairs separated by two spaces, matching
// Grok Build's bottom hint rhythm (bold keys, dim labels).
func FormatShortcutsBar(hints []HintItem, width int) string {
	if len(hints) == 0 {
		return ""
	}
	if width < 1 {
		width = 1
	}
	keyStyle := lipgloss.NewStyle().Foreground(DefaultTheme.TextMuted).Bold(true)
	actionStyle := lipgloss.NewStyle().Foreground(DefaultTheme.Dim)

	parts := make([]string, 0, len(hints))
	plainParts := make([]string, 0, len(hints))
	for _, h := range hints {
		key := strings.TrimSpace(h.Key)
		label := strings.TrimSpace(h.Label)
		if key == "" {
			continue
		}
		plain := key
		styled := keyStyle.Render(key)
		if label != "" {
			plain += ":" + label
			styled += actionStyle.Render(":" + label)
		}
		parts = append(parts, styled)
		plainParts = append(plainParts, plain)
	}
	if len(parts) == 0 {
		return ""
	}

	// Fit as many hints as possible; always try to keep pinned ones.
	line := ""
	plainLine := ""
	for i, part := range parts {
		sep := ""
		if plainLine != "" {
			sep = "  "
		}
		candidate := plainLine + sep + plainParts[i]
		if lipgloss.Width(candidate) > width {
			continue
		}
		if line != "" {
			line += "  "
			plainLine += "  "
		}
		line += part
		plainLine += plainParts[i]
	}
	if line == "" {
		// Fall back to first hint truncated.
		return truncateStyled(parts[0], width)
	}
	return truncateStyled(line, width)
}

// WelcomeChromeInput drives pure welcome rendering.
type WelcomeChromeInput struct {
	Width      int
	Height     int
	Project    string
	Branch     string
	Version    string
	Model      string
	ShowPrompt bool
	// ActiveMenu is the highlighted menu row (hover/keyboard). -1 keeps the
	// default first-item accent used on cold open.
	ActiveMenu int
	// AnimFrame drives logo shimmer (0 = static).
	AnimFrame int
}

// welcomeMenuItem is one clickable cold-open action.
type welcomeMenuItem struct {
	Label   string
	Command string
}

func welcomeMenuItems() []welcomeMenuItem {
	return []welcomeMenuItem{
		{Label: "New session", Command: "/clear"},
		{Label: "Resume last", Command: "/resume"},
		{Label: "Status", Command: "/status"},
	}
}

// RenderWelcomeChrome builds the empty-session shell:
// top location bar → centered brand + menu → optional ❯ cue.
func RenderWelcomeChrome(in WelcomeChromeInput) string {
	lines, _ := buildWelcomeChromeLines(in)
	return strings.Join(lines, "\n")
}

// welcomeMenuRowYs returns absolute row indices (within the welcome body) of
// each menu item after vertical centering — used for mouse hit testing.
func welcomeMenuRowYs(in WelcomeChromeInput) []int {
	_, rows := buildWelcomeChromeLines(in)
	return rows
}

// buildWelcomeChromeLines returns rendered lines and the Y index of each menu
// row after padding/centering (same geometry View and mouse share).
func buildWelcomeChromeLines(in WelcomeChromeInput) (lines []string, menuRows []int) {
	w := in.Width
	if w < 1 {
		w = 1
	}
	h := in.Height
	if h < 1 {
		h = 1
	}
	version := strings.TrimSpace(in.Version)
	if version == "" {
		version = "v1.0.0"
	}
	active := in.ActiveMenu
	if active < 0 {
		active = 0
	}
	top := renderWelcomeLocationBar(w, in.Project, in.Branch, version)
	rule := dimStyle.Render(strings.Repeat("\u2500", max(1, w)))
	brand := renderWelcomeBrandFrame(w, in.AnimFrame)
	menu := renderWelcomeMenuColumn(w, active)
	// Menu owns cold-open actions; help under it is only incremental (start +
	// /help + @path) — no second listing of /resume /status.
	bodyParts := []string{brand, "", menu}
	if in.ShowPrompt {
		// Welcome embeds a static ❯ cue; the live textarea still owns real input
		// in the dock so Bubble Tea focus stays correct.
		bodyParts = append(bodyParts, "", accentStyle.Render("\u276f")+" "+dimStyle.Render("Build anything"))
		if model := strings.TrimSpace(in.Model); model != "" {
			bodyParts = append(bodyParts, dimStyle.Render(model+" \u00b7 normal"))
		}
	}
	bodyParts = append(bodyParts, "", RenderWelcomeMessage(max(1, w)))

	// Chrome header stays pinned: location bar + rule. Only the brand/menu
	// body is vertically centered below — otherwise the rule floats mid-screen
	// above the logo (looks like a stray hairline on the brand).
	header := []string{top, rule}
	body := strings.Split(strings.Join(bodyParts, "\n"), "\n")
	// One blank under the rule so the mark doesn't sit on the hairline.
	if len(body) == 0 || strings.TrimSpace(body[0]) != "" {
		body = append([]string{""}, body...)
	}

	// Menu row indices are measured after padding (absolute in final view).
	menuStartInBody := -1
	for i, line := range body {
		if strings.Contains(line, "New session") {
			menuStartInBody = i
			break
		}
	}

	lines = append([]string{}, header...)
	if len(header)+len(body) < h {
		innerPad := (h - len(header) - len(body)) / 2
		if innerPad < 0 {
			innerPad = 0
		}
		// On short terminals don't force a huge top gap.
		if h < 18 {
			innerPad = 0
		}
		for i := 0; i < innerPad; i++ {
			lines = append(lines, "")
		}
		lines = append(lines, body...)
	} else {
		lines = append(lines, body...)
	}
	for len(lines) < h {
		lines = append(lines, "")
	}
	if len(lines) > h {
		lines = lines[:h]
	}
	// Width-clamp every line.
	for i, line := range lines {
		lines[i] = truncateStyled(line, w)
	}

	nMenu := len(welcomeMenuItems())
	if menuStartInBody >= 0 {
		// body starts after header + optional innerPad blanks.
		bodyOrigin := len(header)
		// Recompute pad that was applied.
		if len(header)+len(body) < h && h >= 18 {
			innerPad := (h - len(header) - len(body)) / 2
			if innerPad < 0 {
				innerPad = 0
			}
			bodyOrigin = len(header) + innerPad
		}
		menuRows = make([]int, 0, nMenu)
		for i := 0; i < nMenu; i++ {
			row := bodyOrigin + menuStartInBody + i
			if row >= 0 && row < len(lines) {
				menuRows = append(menuRows, row)
			}
		}
	}
	return lines, menuRows
}

func renderWelcomeLocationBar(width int, project, branch, version string) string {
	leftParts := make([]string, 0, 3)
	if b := strings.TrimSpace(branch); b != "" {
		// branch glyph \u2387
		leftParts = append(leftParts, accentStyle.Render("\u2387 "+b))
	}
	proj := projectName(project)
	if proj == "" || proj == "." {
		proj = "bondcode"
	}
	leftParts = append(leftParts, dimStyle.Render(proj))
	left := strings.Join(leftParts, " ")
	right := strings.TrimSpace(version)
	if right == "" {
		right = "v1.0.0"
	}
	rightStyled := dimStyle.Render(right)
	gap := width - lipgloss.Width(left) - lipgloss.Width(rightStyled)
	if gap < 1 {
		return truncateStyled(left, max(1, width))
	}
	return left + strings.Repeat(" ", gap) + rightStyled
}

func renderWelcomeBrand(width int) string {
	return strings.Join(renderBondWordmark(width), "\n")
}

func renderWelcomeBrandFrame(width, frame int) string {
	return strings.Join(renderBondWordmarkFrame(width, frame), "\n")
}

func renderWelcomeMenuColumn(width int, activeIdx int) string {
	items := welcomeMenuItems()
	// Grok menu: centered column, label left, key right, full-row bg on select.
	colW := 51
	if width < colW+4 {
		colW = max(28, width-4)
	}
	leftPad := (width - colW) / 2
	if leftPad < 0 {
		leftPad = 0
	}
	pad := strings.Repeat(" ", leftPad)
	if activeIdx < 0 || activeIdx >= len(items) {
		activeIdx = 0
	}

	var lines []string
	for i, item := range items {
		active := i == activeIdx
		labelSt := welcomeMenuStyle
		keySt := welcomeMenuKeyStyle
		if active {
			labelSt = welcomeMenuActiveStyle
			keySt = welcomeMenuKeyActiveStyle
		}
		plainLabel := item.Label
		plainKey := item.Command
		gap := colW - lipgloss.Width(plainLabel) - lipgloss.Width(plainKey)
		if gap < 2 {
			gap = 2
		}
		// Full-row selection: pad the gap with the same background.
		gapStr := strings.Repeat(" ", gap)
		if active {
			gapStr = lipgloss.NewStyle().Background(DefaultTheme.Selection).Render(gapStr)
		}
		line := pad + labelSt.Render(plainLabel) + gapStr + keySt.Render(plainKey)
		// Extend selection bg to full column when short keys leave trailing space.
		if active {
			used := lipgloss.Width(plainLabel) + gap + lipgloss.Width(plainKey)
			if used < colW {
				line += lipgloss.NewStyle().Background(DefaultTheme.Selection).Render(strings.Repeat(" ", colW-used))
			}
		}
		lines = append(lines, truncateStyled(line, width))
	}
	return strings.Join(lines, "\n")
}

// FormatTurnStatusRow builds a single busy/waiting status line.
// Layout: `spinner activity  elapsed`
func FormatTurnStatusRow(spinner, activity, elapsed string, width int) string {
	if width < 1 {
		width = 1
	}
	activity = strings.TrimSpace(activity)
	if activity == "" {
		activity = "working"
	}
	left := strings.TrimSpace(spinner + " " + busyStyle.Render(activity))
	right := dimStyle.Render(strings.TrimSpace(elapsed))
	gap := width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 2 {
		return truncateStyled(left, width)
	}
	return left + strings.Repeat(" ", gap) + right
}

// FormatTurnStatusRowWithStop is the Grok busy row with a clickable [stop] cue.
func FormatTurnStatusRowWithStop(frame int, activity, elapsed string, width int, stopHovered bool) string {
	if width < 1 {
		width = 1
	}
	base := FormatTurnStatusRowAnimated(frame, activity, elapsed, max(1, width-8))
	stop := dimStyle.Render("[stop]")
	if stopHovered {
		stop = errorStyle.Render("[stop]")
	}
	// Recompose so [stop] sits on the far right.
	activity = strings.TrimSpace(activity)
	if activity == "" {
		activity = "working"
	}
	if !strings.Contains(activity, ".") {
		activity = activity + animActivityDots(frame)
	}
	bar := animAccentStyle(frame).Render(animAccentBar(frame))
	spin := animAccentStyle(frame).Render(animSpinnerFrame(frame))
	left := strings.TrimSpace(bar + " " + spin + " " + busyStyle.Render(activity))
	rightCore := strings.TrimSpace(elapsed)
	right := ""
	if rightCore != "" {
		right = dimStyle.Render(rightCore) + " "
	}
	right += stop
	gap := width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		return truncateStyled(left+" "+right, width)
	}
	_ = base
	return left + strings.Repeat(" ", gap) + right
}

// FormatPermissionOptionRow renders one vertical option: `❯ label` when active.
func FormatPermissionOptionRow(label string, active bool) string {
	if active {
		return confirmStyle.Render("\u276f " + label)
	}
	return "  " + label
}

// dockSeparator is the thin rule between scrollback and the prompt stack.
// Visible cue that this is not main's borderless transcript→boxed-composer jump.
func dockSeparator(width int) string {
	if width < 1 {
		width = 1
	}
	return dimStyle.Render(strings.Repeat("\u2500", width))
}

// FormatPermissionOptionList stacks option rows (Grok permission panel language).
func FormatPermissionOptionList(options []string, activeIdx int) string {
	if len(options) == 0 {
		return ""
	}
	if activeIdx < 0 {
		activeIdx = 0
	}
	if activeIdx >= len(options) {
		activeIdx = len(options) - 1
	}
	lines := make([]string, 0, len(options))
	for i, opt := range options {
		lines = append(lines, FormatPermissionOptionRow(opt, i == activeIdx))
	}
	return strings.Join(lines, "\n")
}

// UserPromptPrefix is the Grok-like user turn marker in scrollback.
const UserPromptPrefix = "❯ "

// formatUserEcho styles a submitted user prompt the way Grok shows user blocks.
func formatUserEcho(text string, width int) string {
	text = strings.TrimRight(text, "\n")
	if text == "" {
		return ""
	}
	prefix := accentStyle.Render("\u276f") + " "
	lines := strings.Split(text, "\n")
	out := make([]string, 0, len(lines))
	for i, line := range lines {
		if i == 0 {
			out = append(out, truncateStyled(prefix+userStyle.Render(line), max(1, width)))
			continue
		}
		out = append(out, truncateStyled("  "+userStyle.Render(line), max(1, width)))
	}
	return strings.Join(out, "\n")
}

var _ = fmt.Sprintf
