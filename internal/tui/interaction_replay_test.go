package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/junnhwan/bond-code/internal/agent"
	"github.com/junnhwan/bond-code/internal/ask"
)

func TestInteractionReplayKeepsWorkspaceFramesStable(t *testing.T) {
	model := NewModel(Config{Status: Status{
		SessionID:      "session-replay",
		ProjectRoot:    `D:\dev\my_proj\go\bond-code`,
		Model:          "glm-5.1",
		PermissionMode: "confirm",
		ToolCount:      12,
		GitBranch:      "main",
	}})
	model = model.SetSize(160, 32)
	assertReplayFrame(t, "empty wide", model)

	model.timeline = model.timeline.StartUserTurn("summarize this project")
	model = model.ApplyAgentEvent(agent.Event{Type: agent.EventAgentStarted})
	model.agent.Busy = true
	assertReplayFrame(t, "user submitted", model)

	model = model.ApplyAgentEvent(agent.Event{Type: agent.EventModelChunk, Message: strings.Repeat("reading code ", 20)})
	assertReplayFrame(t, "assistant streaming", model)

	model = model.ApplyAgentEvent(agent.Event{
		Type:       agent.EventToolRequested,
		ToolName:   "run_command",
		ToolCallID: "tool-1",
		Input:      `{"command":"go test ./internal/tui"}`,
	})
	assertReplayFrame(t, "tool running", model)

	model = model.ApplyAgentEvent(agent.Event{
		Type:       agent.EventToolResult,
		ToolName:   "run_command",
		ToolCallID: "tool-1",
		Input:      `{"command":"go test ./internal/tui"}`,
		Output:     "ok github.com/junnhwan/bond-code/internal/tui",
	})
	assertReplayFrame(t, "tool done", model)

	model = model.ApplyAgentEvent(agent.Event{
		Type:     agent.EventToolConfirmationRequested,
		ToolName: "write_file",
		Risk:     "medium",
		Message:  "write generated notes",
		Input:    `{"path":"notes.md","content":"line one\nline two\nline three"}`,
	})
	assertReplayFrame(t, "permission dock", model)

	model = model.ApplyAgentEvent(agent.Event{
		Type:     agent.EventToolRejected,
		ToolName: "write_file",
		Risk:     "medium",
		Message:  "rejected",
	})
	model.question = &ask.Question{
		Prompt: "Pick next step",
		Options: []ask.Option{
			{Label: "Run tests", Description: strings.Repeat("validate current tui behavior ", 4)},
			{Label: "Inspect reference", Description: strings.Repeat("compare against opencode ", 4)},
		},
	}
	assertReplayFrame(t, "question dock", model)

	model.question = nil
	model = model.ApplyAgentEvent(agent.Event{
		Type:       agent.EventSubagentStarted,
		ToolCallID: "task-reviewer",
		ToolName:   "reviewer",
		Message:    "review the rendered workspace",
	})
	assertReplayFrame(t, "subagent bar", model)

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyCtrlUp})
	model = updated.(Model)
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(Model)
	assertReplayFrame(t, "subagent window", model)

	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	model = updated.(Model)
	assertReplayFrame(t, "back to subagent bar", model)

	model = model.SetSize(80, 24)
	assertReplayFrame(t, "resized narrow", model)
}

func TestInteractionReplayConstrainsTallPermissionDock(t *testing.T) {
	model := NewModel(Config{Status: Status{
		SessionID:      "session-replay",
		ProjectRoot:    `D:\dev\my_proj\go\bond-code`,
		Model:          "glm-5.1",
		PermissionMode: "confirm",
		ToolCount:      12,
		GitBranch:      "main",
	}})
	model = model.SetSize(121, 16)
	model.timeline = model.timeline.StartUserTurn("apply a larger edit")
	model.agent.Busy = true
	model = model.ApplyAgentEvent(agent.Event{
		Type:     agent.EventToolConfirmationRequested,
		ToolName: "edit_file",
		Risk:     "medium",
		Message:  "update file",
		Input:    `{"path":"internal/tui/view.go","old_string":"old01\nold02\nold03\nold04\nold05\nold06\nold07\nold08\nold09\nold10\nold11\nold12\nold13\nold14","new_string":"new01\nnew02\nnew03\nnew04\nnew05\nnew06\nnew07\nnew08\nnew09\nnew10\nnew11\nnew12\nnew13\nnew14"}`,
	})

	assertReplayFrame(t, "tall permission dock", model)
}

func assertReplayFrame(t *testing.T, name string, model Model) {
	t.Helper()
	view := model.View()
	assertViewFits(t, view, model.width, model.height)

	if model.agent.Pending != nil || model.question != nil {
		for _, notWant := range []string{"> type a message", "/ commands", "@ files", "interrupted"} {
			if strings.Contains(view, notWant) {
				t.Fatalf("%s pending dock leaked %q:\n%s", name, notWant, view)
			}
		}
	}

	layout := model.currentLayout()
	if model.focus == FocusAgentWindow {
		if false /*ShowSidebar removed*/ {
			t.Fatalf("%s agent window should not reserve live: %#v\n%s", name, layout, view)
		}
		if replaySidebarColumn(view) >= 0 {
			t.Fatalf("%s agent window should not render live:\n%s", name, view)
		}
		return
	}

	sidebarColumn := replaySidebarColumn(view)
	if false /*ShowSidebar removed*/ {
		if sidebarColumn != layout.TimelineW {
			t.Fatalf("%s live column = %d, want %d:\n%s", name, sidebarColumn, layout.TimelineW, view)
		}
	} else if sidebarColumn >= 0 {
		t.Fatalf("%s compact layout should not render live column %d:\n%s", name, sidebarColumn, view)
	}

	for _, line := range strings.Split(view, "\n") {
		content := strings.TrimRight(line, " ")
		if strings.Contains(content, "agents (") && lipgloss.Width(content) > layout.TimelineW {
			t.Fatalf("%s agent bar exceeds timeline width %d:\n%s", name, layout.TimelineW, view)
		}
		if strings.Contains(ansi.Strip(content), "> ") && lipgloss.Width(content) > layout.TimelineW {
			t.Fatalf("%s composer prompt exceeds timeline width %d:\n%s", name, layout.TimelineW, view)
		}
	}
}

func replaySidebarColumn(view string) int {
	for _, line := range strings.Split(view, "\n") {
		plain := ansi.Strip(line)
		if !strings.Contains(plain, "│") {
			continue
		}
		if !strings.Contains(plain, "SESSION") &&
			!strings.Contains(plain, "PROJECT") &&
			!strings.Contains(plain, "TOOLS") &&
			!strings.Contains(plain, "MODE") &&
			!strings.Contains(plain, " sess") &&
			!strings.Contains(plain, " perm") {
			continue
		}
		idx := strings.Index(plain, "│")
		if idx >= 0 {
			return ansi.StringWidth(plain[:idx])
		}
	}
	return -1
}
