package tui

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/junnhwan/bond-code/internal/agent"
	"github.com/junnhwan/bond-code/internal/command"
	"github.com/junnhwan/bond-code/internal/skill"
)

func writeTestSkill(t *testing.T, root, name, front, body string) {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	content := "---\nname: " + name + "\n" + front + "---\n" + body + "\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestSkillSlashSuggestionsIncludeUserOnlySkills(t *testing.T) {
	root := t.TempDir()
	writeTestSkill(t, root, "handoff", "description: hand off work\ndisable-model-invocation: true\n", "Hand off body.")
	writeTestSkill(t, root, "grilling", "description: grill plans\n", "Grill body.")
	writeTestSkill(t, root, "hidden-model", "description: model only\nuser-invocable: false\n", "Model body.")

	loader := skill.NewLoaderFromRoot(root)
	sl := NewSuggestionListWithSkills(nil, loader)
	names := map[string]string{}
	for _, item := range sl.commandItems {
		if item.Source == "skill" {
			names[item.Name] = item.Description
		}
	}
	if _, ok := names["handoff"]; !ok {
		t.Fatalf("user-only skill missing from slash suggestions: %#v", names)
	}
	if !strings.Contains(names["handoff"], "[user-only]") {
		t.Fatalf("user-only badge missing: %q", names["handoff"])
	}
	if _, ok := names["grilling"]; !ok {
		t.Fatalf("model+user skill missing from slash suggestions: %#v", names)
	}
	if _, ok := names["hidden-model"]; ok {
		t.Fatal("model-only skill must stay out of slash suggestions")
	}
}

func TestTrySkillSlashExpandsUserOnlySkill(t *testing.T) {
	root := t.TempDir()
	writeTestSkill(t, root, "handoff", "description: hand off\ndisable-model-invocation: true\n", "Do the handoff now.")
	loader := skill.NewLoaderFromRoot(root)

	model := NewModel(Config{
		CommandEnv: command.Env{SkillLoader: loader},
		Chat:       stubChat{},
	})
	next, cmd, handled := model.trySkillSlash("handoff", nil)
	if !handled {
		t.Fatal("expected skill slash to be handled")
	}
	if cmd == nil {
		t.Fatal("expected agent run cmd")
	}
	// User turn body should carry skill content + command markers.
	if len(next.timeline.Turns) == 0 {
		t.Fatal("expected a user turn")
	}
	body := next.timeline.Turns[len(next.timeline.Turns)-1].User.Body
	for _, want := range []string{"<command-name>/handoff</command-name>", "Do the handoff now.", "Base directory for this skill:"} {
		if !strings.Contains(body, want) {
			t.Fatalf("expanded skill missing %q:\n%s", want, body)
		}
	}
}

func TestCommandBlockWrapsLongSkillLines(t *testing.T) {
	long := strings.Repeat("word ", 40) + "ENDMARK"
	block := Block{Kind: BlockCommand, Title: "/skills", Body: "- wayfinder: " + long}
	m := NewModel(Config{})
	lines := m.renderTimelineBlockLines(block, 60)
	if len(lines) < 2 {
		t.Fatalf("expected wrapped multi-line command output, got %d lines: %v", len(lines), lines)
	}
	joined := ansi.Strip(strings.Join(lines, "\n"))
	if !strings.Contains(joined, "ENDMARK") {
		t.Fatalf("wrap must keep tail content, got:\n%s", joined)
	}
	if !strings.Contains(joined, "wayfinder") {
		t.Fatalf("wrap must keep skill name, got:\n%s", joined)
	}
	for _, line := range lines {
		if ansi.StringWidth(ansi.Strip(line)) > 62 {
			t.Fatalf("line wider than timeline: %q (w=%d)", line, ansi.StringWidth(ansi.Strip(line)))
		}
	}
}

// stubChat satisfies ChatRunner for slash-expand tests that only start a turn.
type stubChat struct{}

func (stubChat) RunWithEvents(context.Context, string, agent.EventSink) (*agent.RunResult, error) {
	return &agent.RunResult{}, nil
}

func (stubChat) Compact(context.Context, agent.EventSink) error { return nil }
