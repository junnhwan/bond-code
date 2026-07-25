package builtin

import (
	"context"
	"strings"
	"testing"

	"github.com/junnhwan/bond-code/internal/command"
)

func TestHelpCommandRendersTUICommandsAndClaudeCoreKeys(t *testing.T) {
	result, err := HelpCommand().Run(context.Background(), command.Env{}, nil)
	if err != nil {
		t.Fatalf("help command: %v", err)
	}
	for _, want := range []string{"/help", "/status", "/context", "/copy", "/clear", "/retry", "/diff", "/history", "/exit"} {
		if !strings.Contains(result.Output, want) {
			t.Fatalf("expected %q in help output:\n%s", want, result.Output)
		}
	}
	if result.Panel == nil || result.Panel.Title != "help" {
		t.Fatalf("expected structured help panel, got %#v", result.Panel)
	}

	for _, descriptor := range command.DirectKeyDescriptors() {
		if !strings.Contains(result.Output, descriptor.DisplayShortcut) {
			t.Errorf("canonical direct key %q missing from help output:\n%s", descriptor.DisplayShortcut, result.Output)
		}
	}
	for _, removed := range []string{"Ctrl+P", "Ctrl+X", "Adaptive Rail", "rail", "sidebar", "Ctrl+F", "Ctrl+H", "Alt+Ctrl"} {
		if strings.Contains(result.Output, removed) {
			t.Errorf("removed shortcut or surface %q appeared in help output:\n%s", removed, result.Output)
		}
	}
	for _, row := range result.Panel.Sections[1].Rows {
		if row.Key == "?" || row.Key == "v" {
			t.Errorf("removed shortcut %q appeared in KEYS", row.Key)
		}
	}
}

func TestRegisterAllIncludesHelpCommand(t *testing.T) {
	registry := command.NewRegistry()
	if err := RegisterAll(registry); err != nil {
		t.Fatal(err)
	}
	if _, ok := registry.Get("help"); !ok {
		t.Fatal("expected help command to be registered")
	}
}
