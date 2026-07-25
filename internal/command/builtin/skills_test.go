package builtin

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/junnhwan/bond-code/internal/command"
	"github.com/junnhwan/bond-code/internal/skill"
)

func TestSkillsCommandListsAndShowsSkills(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "debugging")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	content := "---\nname: debugging\ndescription: debug systematically\n---\n# Debugging\n\nReproduce first.\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	loader := skill.NewLoaderFromRoot(root)
	cmd := SkillsCommand()
	listResult, err := cmd.Run(context.Background(), command.Env{SkillLoader: loader}, nil)
	if err != nil {
		t.Fatalf("skills list: %v", err)
	}
	for _, want := range []string{"debugging", "debug systematically"} {
		if !strings.Contains(listResult.Output, want) {
			t.Fatalf("skills list missing %q:\n%s", want, listResult.Output)
		}
	}

	showResult, err := cmd.Run(context.Background(), command.Env{SkillLoader: loader}, []string{"show", "debugging"})
	if err != nil {
		t.Fatalf("skills show: %v", err)
	}
	for _, want := range []string{"# Debugging", "Reproduce first.", "Base directory for this skill:"} {
		if !strings.Contains(showResult.Output, want) {
			t.Fatalf("skills show missing %q:\n%s", want, showResult.Output)
		}
	}
}

func TestRegisterAllIncludesSkillsCommand(t *testing.T) {
	registry := command.NewRegistry()
	if err := RegisterAll(registry); err != nil {
		t.Fatal(err)
	}
	if _, ok := registry.Get("skills"); !ok {
		t.Fatal("expected /skills command to be registered")
	}
}
