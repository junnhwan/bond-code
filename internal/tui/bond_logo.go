package tui

// Bond brand mark: two linked rings rendered in braille (Grok-style solid
// terminal icon — not letterforms of "BOND").
//
// Metaphor: a bond is a connection between two nodes (human ↔ agent, tool ↔
// tool). The mark is a pair of interlocked rings / molecular link, readable
// at a glance as "linked" without spelling the name.
//
// Generated from a 2×4-dot braille grid; sizes scale for terminal width.
// Characters are U+28xx braille patterns (including U+2800 blank).

// bondLogoXS — compact 2-row mark for narrow terminals.
const bondLogoXS = "" +
	"⢠⠴⢶⣶⠶⢤\n" +
	"⠘⠲⠞⠛⠶⠚"

// bondLogoSM — small welcome mark.
const bondLogoSM = "" +
	"⠀⢀⣀⣀⡀⠀⣀⣀⣀⠀⠀\n" +
	"⣰⡿⠋⠙⢿⣾⠟⠉⠻⣷⡀\n" +
	"⠹⣷⣄⣠⣾⢿⣦⣀⣴⡿⠁\n" +
	"⠀⠈⠉⠉⠁⠀⠉⠉⠉⠀⠀"

// bondLogoMD — default welcome mark.
const bondLogoMD = "" +
	"⠀⣠⣶⣾⣿⣷⣦⣤⣶⣿⣿⣶⣦⡀⠀\n" +
	"⣼⣿⠏⠁⠀⠈⢿⣿⠏⠀⠀⠉⢿⣿⡄\n" +
	"⢻⣿⣆⡀⠀⢀⣾⣿⣆⠀⠀⣀⣾⣿⠃\n" +
	"⠀⠙⠿⢿⣿⡿⠟⠛⠿⣿⣿⠿⠟⠁⠀"

// bondLogoLG — wide terminal hero mark.
const bondLogoLG = "" +
	"⠀⠀⠀⣀⣤⣤⣤⣤⣄⡀⠀⣀⣤⣤⣤⣤⣄⡀⠀⠀\n" +
	"⠀⣠⣾⣿⣿⠿⠿⠿⣿⣿⣿⣿⡿⠿⠿⢿⣿⣿⣦⡀\n" +
	"⢠⣿⣿⡟⠀⠀⠀⠀⠘⣿⣿⡟⠀⠀⠀⠀⠘⣿⣿⣧\n" +
	"⠘⣿⣿⣧⠀⠀⠀⠀⢠⣿⣿⣧⠀⠀⠀⠀⢠⣿⣿⡟\n" +
	"⠀⠙⢿⣿⣿⣶⣶⣶⣿⣿⣿⣿⣷⣶⣶⣾⣿⣿⠟⠁\n" +
	"⠀⠀⠀⠉⠛⠛⠛⠛⠋⠁⠀⠉⠛⠛⠛⠛⠋⠁⠀⠀"

// bondLogoForWidth picks a mark density that fits the terminal.
func bondLogoForWidth(width int) string {
	switch {
	case width >= 52:
		return bondLogoLG
	case width >= 36:
		return bondLogoMD
	case width >= 22:
		return bondLogoSM
	default:
		return bondLogoXS
	}
}
