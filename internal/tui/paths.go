package tui

import (
	"os"
	"path/filepath"
	"strings"
)

// displayProjectRoot is the project root used to shorten file paths in tool
// rendering. It is set once from Config.Status.ProjectRoot when the TUI model
// is created. The TUI runs a single instance per process, so package-level
// state is acceptable here.
var displayProjectRoot string

func setDisplayProjectRoot(root string) {
	displayProjectRoot = strings.TrimSpace(root)
}

// absPath returns an absolute form of path, falling back to the original on
// error. Relative paths are resolved against the process working directory.
func absPath(path string) string {
	if filepath.IsAbs(path) {
		return path
	}
	if resolved, err := filepath.Abs(path); err == nil {
		return resolved
	}
	return path
}

// displayPath shortens a path for display: project-relative if it lives inside
// the project root, "~"-prefixed if under the user's home directory, otherwise
// as-is. It mirrors Claude Code's getDisplayPath. verbose=true returns the
// original path unchanged so power users can see full locations.
func displayPath(path string, verbose bool) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	if verbose {
		return filepath.ToSlash(path)
	}

	abs := absPath(path)

	if root := strings.TrimSpace(displayProjectRoot); root != "" {
		rootAbs := absPath(root)
		if rel, err := filepath.Rel(rootAbs, abs); err == nil && rel != "." && !strings.HasPrefix(rel, "..") {
			return filepath.ToSlash(rel)
		}
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		if abs == home || strings.HasPrefix(abs, home+string(os.PathSeparator)) {
			return "~" + filepath.ToSlash(strings.TrimPrefix(abs, home))
		}
	}
	return filepath.ToSlash(path)
}
