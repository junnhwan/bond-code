package agent

import (
	"context"
	"testing"

	"github.com/junnhwan/bond-code/internal/llm"
	"github.com/junnhwan/bond-code/internal/safety"
	"github.com/junnhwan/bond-code/internal/testutil/llmfake"
	"github.com/junnhwan/bond-code/internal/tool"
)

func TestLoopStreamsAgentEventsToSink(t *testing.T) {
	registry := tool.NewRegistry()
	if err := registry.Register(&fakeReadTool{}); err != nil {
		t.Fatal(err)
	}
	client := llmfake.New([][]llm.Chunk{
		{{Content: "checking "}, {ToolCall: &llm.ToolCall{ID: "call-read", Name: "read_file", Arguments: `{}`}, Done: true}},
		{{Content: "done", Done: true}},
	})
	loop := NewLoop(LoopConfig{MaxSteps: 3}, client, registry, safety.Policy{}, safety.StaticConfirmer(true))

	var events []Event
	_, err := loop.RunWithEvents(context.Background(), "read", func(event Event) {
		events = append(events, event)
	})
	if err != nil {
		t.Fatal(err)
	}

	want := []EventType{
		EventAgentStarted,
		EventModelChunk,
		EventToolRequested,
		EventToolApproved,
		EventToolResult,
		EventModelChunk,
		EventAgentFinished,
	}
	if len(events) != len(want) {
		t.Fatalf("expected %d events, got %#v", len(want), events)
	}
	for i, wantType := range want {
		if events[i].Type != wantType {
			t.Fatalf("event %d: expected %s, got %#v", i, wantType, events[i])
		}
	}
	if events[2].ToolName != "read_file" || events[2].Input != `{}` {
		t.Fatalf("expected tool request details, got %#v", events[2])
	}
	if events[4].ToolName != "read_file" || events[4].Output != "file content" {
		t.Fatalf("expected tool result details, got %#v", events[4])
	}
}

func TestLoopEmitsConfirmationRequestBeforeConfirmer(t *testing.T) {
	toolUnderTest := &riskTool{risk: tool.RiskHigh}
	registry := tool.NewRegistry()
	if err := registry.Register(toolUnderTest); err != nil {
		t.Fatal(err)
	}
	client := llmfake.New([][]llm.Chunk{
		{{ToolCall: &llm.ToolCall{ID: "call-risk", Name: toolUnderTest.Name(), Arguments: `{"path":"README.md"}`}, Done: true}},
		{{Content: "done", Done: true}},
	})
	confirmer := &recordingConfirmer{approve: true}
	loop := NewLoop(LoopConfig{MaxSteps: 3}, client, registry, safety.Policy{RequireConfirmation: true}, confirmer)

	var events []Event
	_, err := loop.RunWithEvents(context.Background(), "write", func(event Event) {
		events = append(events, event)
	})
	if err != nil {
		t.Fatal(err)
	}

	confirmIndex := indexEvent(events, EventToolConfirmationRequested)
	approvedIndex := indexEvent(events, EventToolApproved)
	if confirmIndex < 0 {
		t.Fatalf("expected confirmation request event, got %#v", events)
	}
	if approvedIndex < 0 || confirmIndex > approvedIndex {
		t.Fatalf("expected confirmation request before approval, got %#v", events)
	}
	if events[confirmIndex].Risk != string(tool.RiskHigh) || events[confirmIndex].Input != `{"path":"README.md"}` {
		t.Fatalf("expected confirmation details, got %#v", events[confirmIndex])
	}
}

func TestLoopEmitsFinalMeasuredUsageEventPerModelCall(t *testing.T) {
	registry := tool.NewRegistry()
	client := llmfake.New([][]llm.Chunk{
		{
			{Usage: &llm.Usage{InputTokens: 100}},
			{Usage: &llm.Usage{InputTokens: 100, OutputTokens: 8}, Content: "done", Done: true},
		},
	})
	loop := NewLoop(LoopConfig{MaxSteps: 1}, client, registry, safety.Policy{}, safety.StaticConfirmer(true))

	var finalUsage []Event
	_, err := loop.RunWithEvents(context.Background(), "hello", func(event Event) {
		if event.Type == EventContextMeasured && event.MeasuredUsageFinal {
			finalUsage = append(finalUsage, event)
		}
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(finalUsage) != 1 {
		t.Fatalf("expected one final measured usage event, got %#v", finalUsage)
	}
	if finalUsage[0].MeasuredInputTokens != 100 || finalUsage[0].MeasuredOutputTokens != 8 {
		t.Fatalf("expected final usage 100/8, got %#v", finalUsage[0])
	}
}

func indexEvent(events []Event, eventType EventType) int {
	for i, event := range events {
		if event.Type == eventType {
			return i
		}
	}
	return -1
}
