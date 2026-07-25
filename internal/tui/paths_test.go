package tui

import (
	"path/filepath"
	"testing"
)

func TestDisplayPathVerboseAndEmpty(t *testing.T) {
	prev := displayProjectRoot
	defer func() { displayProjectRoot = prev }()
	setDisplayProjectRoot("")

	if got := displayPath("", false); got != "" {
		t.Errorf("displayPath('') = %q, want empty", got)
	}
	if got := displayPath("src/foo.go", true); got != "src/foo.go" {
		t.Errorf("verbose should keep the path as-is: got %q", got)
	}
	// A relative path with no project root stays relative (no home match either
	// in the test environment).
	if got := displayPath("src/foo.go", false); got != "src/foo.go" {
		t.Errorf("relative path with no root should stay: got %q", got)
	}
}

func TestDisplayPathProjectRelative(t *testing.T) {
	prev := displayProjectRoot
	defer func() { displayProjectRoot = prev }()

	root := filepath.Join("tmp", "proj")
	setDisplayProjectRoot(root)

	// An absolute path inside the project root shortens to its project-relative
	// form, regardless of OS path separator.
	abs := absPath(filepath.Join(root, "src", "foo.go"))
	got := displayPath(abs, false)
	want := filepath.ToSlash(filepath.Join("src", "foo.go"))
	if got != want {
		t.Errorf("project-relative: got %q, want %q", got, want)
	}
}
