package tui

import (
	"os"
	"testing"
)

// TestEditorCommandPrefersEditor checks $EDITOR wins, then $VISUAL, then the
// platform default.
func TestEditorCommandPrefersEditor(t *testing.T) {
	t.Setenv("EDITOR", "magical-editor")
	t.Setenv("VISUAL", "visual-editor")
	if got := editorCommand(); got != "magical-editor" {
		t.Fatalf("expected $EDITOR to win, got %q", got)
	}
	os.Unsetenv("EDITOR")
	t.Setenv("VISUAL", "visual-editor")
	if got := editorCommand(); got != "visual-editor" {
		t.Fatalf("expected $VISUAL as fallback, got %q", got)
	}
	os.Unsetenv("VISUAL")
	if got := editorCommand(); got == "" {
		t.Fatal("expected a platform default editor when no env is set")
	}
}

// TestParseEditorCommand covers a bare name and a name-with-flags, ensuring the
// draft path is always the final arg.
func TestParseEditorCommand(t *testing.T) {
	t.Setenv("EDITOR", "vim")
	name, args := parseEditorCommand("/tmp/draft.md")
	if name != "vim" || len(args) != 1 || args[0] != "/tmp/draft.md" {
		t.Fatalf("bare editor parse wrong: name=%q args=%v", name, args)
	}
	t.Setenv("EDITOR", "code -w")
	name, args = parseEditorCommand("/tmp/draft.md")
	if name != "code" || len(args) != 2 || args[0] != "-w" || args[1] != "/tmp/draft.md" {
		t.Fatalf("flagged editor parse wrong: name=%q args=%v", name, args)
	}
}

// TestApplyEditorResultLoadsContent checks the edited content lands in the
// composer and a single trailing newline (which editors append) is stripped.
func TestApplyEditorResultLoadsContent(t *testing.T) {
	model := NewModel(Config{})
	next, _ := model.applyEditorResult(editorDoneMsg{content: "edited prompt\n"})
	if got := next.inputValue(); got != "edited prompt" {
		t.Fatalf("expected trailing newline stripped and content loaded, got %q", got)
	}
}

// TestApplyEditorResultErrorWithoutContentWarns checks an editor error with no
// recovered content surfaces a warning rather than emptying the composer.
func TestApplyEditorResultErrorWithoutContentWarns(t *testing.T) {
	model := NewModel(Config{})
	model = model.SetInput("keep me")
	next, _ := model.applyEditorResult(editorDoneMsg{err: errStub("killed"), content: ""})
	if next.inputValue() != "keep me" {
		t.Fatalf("expected composer untouched on editor error, got %q", next.inputValue())
	}
	if len(next.toasts) != 1 || next.toasts[0].variant != toastWarn {
		t.Fatalf("expected a warn toast, got %+v", next.toasts)
	}
}

type errStub string

func (e errStub) Error() string { return string(e) }
