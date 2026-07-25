package subagent

import (
	"context"
	"testing"

	"github.com/junnhwan/bond-code/internal/agent"

	"github.com/junnhwan/bond-code/internal/llm"
	"github.com/junnhwan/bond-code/internal/tool"
)

func TestRunTaskEmitsLifecycleEvents(t *testing.T) {
	events := []Event{}
	manager := newTestManagerWithOptions(
		llm.NewFakeClient([]llm.Chunk{{Content: "child answer", Done: true}}),
		tool.NewRegistry(),
		ManagerOptions{
			MaxChildrenPerTurn:    1,
			DefaultTimeoutSeconds: 5,
			EventSink: func(event Event) {
				events = append(events, event)
			},
		},
	)

	result, err := manager.RunTask(context.Background(), TaskRequest{
		Description:  "inspect",
		Prompt:       "inspect docs",
		SubagentType: AgentTypeResearch,
	})
	if err != nil {
		t.Fatalf("run task: %v", err)
	}
	if result.Status != "completed" {
		t.Fatalf("expected completed, got %#v", result)
	}
	if len(events) < 2 {
		t.Fatalf("expected lifecycle events, got %#v", events)
	}
	if events[0].Type != EventStarted || events[len(events)-1].Type != EventFinished {
		t.Fatalf("expected start and finish events, got %#v", events)
	}
}

func TestChildEventSinkForwardsModelAndReasoningChunks(t *testing.T) {
	var events []Event
	manager := &SubagentManager{options: ManagerOptions{EventSink: func(event Event) { events = append(events, event) }}}
	sink := manager.childEventSink(TaskRequest{TaskID: "task-1", Generation: 3}, AgentProfile{Type: AgentTypeResearch})
	sink(agent.Event{Type: agent.EventReasoningChunk, Message: "considering"})
	sink(agent.Event{Type: agent.EventModelChunk, Message: "answer"})
	if len(events) != 2 {
		t.Fatalf("expected two transcript chunks, got %#v", events)
	}
	if events[0].Type != EventTranscriptChunk || events[0].TranscriptKind != "reasoning" || events[0].Generation != 3 {
		t.Fatalf("unexpected reasoning chunk: %#v", events[0])
	}
	if events[1].Type != EventTranscriptChunk || events[1].TranscriptKind != "assistant" || events[1].Message != "answer" {
		t.Fatalf("unexpected model chunk: %#v", events[1])
	}
}
