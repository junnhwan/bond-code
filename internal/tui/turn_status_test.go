package tui

import (
	"strings"
	"testing"
	"time"
)

func TestRenderTurnRunStatusShowsInterruptedWhenAgentStopsMidTurn(t *testing.T) {
	started := time.Now().Add(-30 * time.Second)
	m := Model{
		timeline: TimelineState{Turns: []Turn{{
			ID:        "turn-1",
			StartedAt: started,
			Run:       TurnRunStatus{State: "working", Detail: "tool: read_file", StartedAt: started},
		}}},
		agent: AgentRunState{Busy: false},
	}
	got := m.renderTurnRunStatus(m.timeline.Turns[0])
	if !strings.Contains(got, "interrupted") {
		t.Fatalf("expected interrupted marker when agent stopped mid-turn, got %q", got)
	}
	if !strings.Contains(got, "read_file") {
		t.Fatalf("expected last activity surfaced in interrupted line, got %q", got)
	}
}

func TestRenderTurnRunStatusNotInterruptedWhenDone(t *testing.T) {
	started := time.Now().Add(-30 * time.Second)
	m := Model{
		timeline: TimelineState{Turns: []Turn{{
			ID:  "turn-1",
			Run: TurnRunStatus{State: "done", StartedAt: started, EndedAt: time.Now()},
		}}},
		agent: AgentRunState{Busy: false},
	}
	got := m.renderTurnRunStatus(m.timeline.Turns[0])
	if strings.Contains(got, "interrupted") {
		t.Fatalf("done turn must not show interrupted, got %q", got)
	}
}

func TestIsTerminalRunState(t *testing.T) {
	for _, s := range []string{"done", "failed", "cancelled"} {
		if !isTerminalRunState(s) {
			t.Fatalf("expected %q to be terminal", s)
		}
	}
	for _, s := range []string{"", "working", "waiting"} {
		if isTerminalRunState(s) {
			t.Fatalf("expected %q to be non-terminal", s)
		}
	}
}
