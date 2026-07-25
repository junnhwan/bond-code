package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestComposerHeightForWidth(t *testing.T) {
	tests := []struct {
		name  string
		value string
		width int
		want  int
	}{
		{name: "empty", value: "", width: 10, want: 1},
		{name: "soft wrap", value: "12345678901", width: 10, want: 2},
		{name: "cursor wraps at exact edge", value: "1234567890", width: 10, want: 2},
		{name: "word wrapping", value: "123456 123456 123456", width: 10, want: 3},
		{name: "explicit empty line", value: "one\n\nthree", width: 20, want: 3},
		{name: "wide unicode", value: "你好世界你好", width: 6, want: 2},
		{name: "clamped", value: strings.Repeat("x", 100), width: 10, want: composerMaxHeight},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := composerHeightForWidth(tt.value, tt.width); got != tt.want {
				t.Fatalf("composerHeightForWidth(%q, %d) = %d, want %d", tt.value, tt.width, got, tt.want)
			}
		})
	}
}

func TestComposerBasePresentationIsCompactAndHelpFree(t *testing.T) {
	model := NewModel(Config{Status: Status{Model: "claude-sonnet"}}).SetSize(100, 24)
	view := ansi.Strip(model.composerViewForWidth(model.currentLayout().TimelineW))

	// ❯ prompt line + model/mode info line (no heavy rounded border).
	if got := renderedHeight(view); got < 2 {
		t.Fatalf("base composer height = %d, want at least prompt+info:\n%s", got, view)
	}
	if !strings.Contains(view, "❯") {
		t.Fatalf("base composer should show ❯ prompt:\n%s", view)
	}
	// Info line carries model · mode; permanent help legends stay off the prompt body.
	for _, notWant := range []string{"/help", "/ commands", "? help", "@ files", "chars ", "tok ~"} {
		if strings.Contains(view, notWant) {
			t.Fatalf("base composer should not contain permanent help legend %q:\n%s", notWant, view)
		}
	}
	if !strings.Contains(view, "claude-sonnet") {
		t.Fatalf("prompt info line should include model:\n%s", view)
	}
}

func TestComposerMultilinePresentationGrowsWithInputOnly(t *testing.T) {
	model := NewModel(Config{}).SetSize(80, 24)
	model = model.SetInput("first line\nsecond line")
	view := ansi.Strip(model.composerViewForWidth(model.currentLayout().TimelineW))

	for _, want := range []string{"first line", "second line"} {
		if !strings.Contains(view, want) {
			t.Fatalf("multiline composer missing %q:\n%s", want, view)
		}
	}
	// Top rule + input height + optional info line.
	want := model.composer.Input.Height() + 1
	if model.promptInfoLine() != "" {
		want++
	}
	if got := renderedHeight(view); got != want {
		t.Fatalf("multiline composer height = %d, want %d:\n%s", got, want, view)
	}
}
