package app

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveConfigPathExplicitWins(t *testing.T) {
	explicit := filepath.Join(t.TempDir(), "explicit.yaml")
	if got := resolveConfigPathFrom([]string{"bondcode.yaml"}, explicit); got != explicit {
		t.Fatalf("explicit --config should win, got %q", got)
	}
}

func TestResolveConfigPathFallsBackToFirstExisting(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "nope.yaml")
	present := filepath.Join(dir, "bondcode.yaml")
	if err := os.WriteFile(present, []byte("model:\n  model: x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	second := filepath.Join(dir, "home.yaml")
	got := resolveConfigPathFrom([]string{missing, present, second}, "")
	if got != present {
		t.Fatalf("expected first existing path %q, got %q", present, got)
	}
}

func TestResolveConfigPathEmptyWhenNoneExist(t *testing.T) {
	if got := resolveConfigPathFrom([]string{filepath.Join(t.TempDir(), "missing.yaml")}, ""); got != "" {
		t.Fatalf("expected empty when no candidate exists, got %q", got)
	}
}

func TestDefaultConfigSearchPathsStartsProjectLocal(t *testing.T) {
	paths := defaultConfigSearchPaths()
	if len(paths) == 0 || paths[0] != "bondcode.yaml" {
		t.Fatalf("expected project-local bondcode.yaml first, got %v", paths)
	}
	// A resolved $HOME should contribute a user-level config.yaml.
	for _, p := range paths[1:] {
		if filepath.Base(p) == "config.yaml" {
			return
		}
	}
	t.Fatalf("expected a user-level config.yaml after the project path, got %v", paths)
}

func TestEncodeProjectDirMatchesClaudeCode(t *testing.T) {
	cases := map[string]string{
		`D:\zjh\dev\chat\my-proj\bond-code`: "D--zjh-dev-chat-my-proj-bond-code",
		`C:\Users\33734`:                    "C--Users-33734",
		"/home/user/proj":                   "-home-user-proj",
		"":                                  "default",
	}
	for in, want := range cases {
		if got := encodeProjectDir(in); got != want {
			t.Errorf("encodeProjectDir(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestDefaultProjectDataDirUnderBondcodeHome(t *testing.T) {
	t.Setenv("BONDCODE_HOME", filepath.Join(t.TempDir(), "bc"))
	got := defaultProjectDataDirFor(`D:\proj`)
	want := filepath.Join(bondcodeHome(), "projects", "D--proj")
	if got != want {
		t.Fatalf("defaultProjectDataDirFor = %q, want %q", got, want)
	}
}

func TestBondcodeHomeRespectsEnv(t *testing.T) {
	t.Setenv("BONDCODE_HOME", "/custom/bc")
	if got := bondcodeHome(); got != "/custom/bc" {
		t.Fatalf("bondcodeHome() = %q, want /custom/bc", got)
	}
}
