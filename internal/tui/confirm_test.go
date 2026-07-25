package tui

import (
	"strings"
	"testing"

	"github.com/junnhwan/bond-code/internal/agent"
)

func TestPermissionPanelHidesComposer(t *testing.T) {
	model := NewModel(Config{})
	model = model.SetSize(100, 24)
	model = model.ApplyAgentEvent(agent.Event{
		Type:     agent.EventToolConfirmationRequested,
		ToolName: "write_file",
		Risk:     "medium",
		Message:  "write file",
		Input:    `{"path":"internal/agent/loop.go"}`,
	})

	view := model.View()
	for _, want := range []string{
		"Permission required",
		"write_file",
		"internal/agent/loop.go",
		"Risk: medium",
		"y allow once",
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("permission panel missing %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, "d details") {
		t.Fatalf("permission panel should not advertise unimplemented details toggle:\n%s", view)
	}
	if strings.Contains(view, "> type a message") {
		t.Fatalf("permission panel should hide composer while it owns input:\n%s", view)
	}
	if strings.Contains(view, "/ commands") || strings.Contains(view, "@ files") {
		t.Fatalf("permission footer should not advertise composer actions:\n%s", view)
	}
}

func TestHighRiskPermissionPanelShowsYesNoSelector(t *testing.T) {
	model := NewModel(Config{})
	model = model.SetSize(100, 24)
	model = model.ApplyAgentEvent(agent.Event{
		Type:     agent.EventToolConfirmationRequested,
		ToolName: "run_command",
		Risk:     "high",
		Message:  "run command",
		Input:    `{"command":"rm -rf tmp"}`,
	})

	view := model.View()
	for _, want := range []string{
		"Permission required",
		"Risk: high",
		"Yes",
		"No",
		"enter confirm",
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("high-risk permission panel missing %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, "type yes") {
		t.Fatalf("high-risk permission panel should not require typing yes:\n%s", view)
	}
}

func TestClippedHighRiskPermissionKeepsSelectorVisible(t *testing.T) {
	model := NewModel(Config{})
	model = model.SetSize(121, 16)
	model = model.ApplyAgentEvent(agent.Event{
		Type:     agent.EventToolConfirmationRequested,
		ToolName: "edit_file",
		Risk:     "high",
		Message:  "update file",
		Input:    `{"path":"internal/tui/view.go","old_string":"old01\nold02\nold03\nold04\nold05\nold06\nold07\nold08\nold09\nold10\nold11\nold12\nold13\nold14","new_string":"new01\nnew02\nnew03\nnew04\nnew05\nnew06\nnew07\nnew08\nnew09\nnew10\nnew11\nnew12\nnew13\nnew14"}`,
	})

	view := model.View()
	assertViewFits(t, view, model.width, model.height)
	if !strings.Contains(view, "❯ No") {
		t.Fatalf("clipped high-risk permission should keep selected option visible:\n%s", view)
	}
	if !strings.Contains(view, "enter confirm") {
		t.Fatalf("clipped high-risk permission should keep confirmation hint visible:\n%s", view)
	}
}
